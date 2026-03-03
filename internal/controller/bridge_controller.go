package controller

import (
	"context"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/status"
)

// BridgeReconciler reconciles a Bridge object.
type BridgeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=bridges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=bridges/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *BridgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("bridge", req.NamespacedName)

	var bridge v1alpha1.Bridge
	if err := r.Get(ctx, req.NamespacedName, &bridge); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Build server and UI deployments.
	serverDep, uiDep := manifests.BuildBridgeDeployments(&bridge)

	// Reconcile server Deployment.
	if err := ctrl.SetControllerReference(&bridge, serverDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingServer := &appsv1.Deployment{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingServer, func() error {
		existingServer.Name = serverDep.Name
		existingServer.Namespace = serverDep.Namespace
		return manifests.MutateFuncFor(existingServer, serverDep)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Server Service.
	serverLabels := manifests.StandardLabels(bridge.Name+"-server", "bridge-server", bridge.Name, "")
	serverSelector := manifests.SelectorLabels(bridge.Name + "-server")
	serverSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bridge.Name + "-server",
			Namespace: bridge.Namespace,
			Labels:    serverLabels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: serverSelector,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	if err := ctrl.SetControllerReference(&bridge, serverSvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingServerSvc := &corev1.Service{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingServerSvc, func() error {
		existingServerSvc.Name = serverSvc.Name
		existingServerSvc.Namespace = serverSvc.Namespace
		return manifests.MutateFuncFor(existingServerSvc, serverSvc)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile UI if present.
	if uiDep != nil {
		if err := ctrl.SetControllerReference(&bridge, uiDep, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingUI := &appsv1.Deployment{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingUI, func() error {
			existingUI.Name = uiDep.Name
			existingUI.Namespace = uiDep.Namespace
			return manifests.MutateFuncFor(existingUI, uiDep)()
		}); err != nil {
			return ctrl.Result{}, err
		}

		uiLabels := manifests.StandardLabels(bridge.Name+"-ui", "bridge-ui", bridge.Name, "")
		uiSelector := manifests.SelectorLabels(bridge.Name + "-ui")
		uiSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      bridge.Name + "-ui",
				Namespace: bridge.Namespace,
				Labels:    uiLabels,
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: uiSelector,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 3000, Protocol: corev1.ProtocolTCP},
				},
			},
		}
		if err := ctrl.SetControllerReference(&bridge, uiSvc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingUISvc := &corev1.Service{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingUISvc, func() error {
			existingUISvc.Name = uiSvc.Name
			existingUISvc.Namespace = uiSvc.Namespace
			return manifests.MutateFuncFor(existingUISvc, uiSvc)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Build Ingress if enabled.
	if bridge.Spec.Ingress != nil && bridge.Spec.Ingress.Enabled {
		ingressTarget := bridge.Name + "-server"
		ingressPort := int32(8080)
		if uiDep != nil {
			ingressTarget = bridge.Name + "-ui"
			ingressPort = 3000
		}
		labels := manifests.StandardLabels(bridge.Name, "bridge", "", "")
		desiredIng := manifests.BuildIngress(bridge.Name, bridge.Namespace, bridge.Spec.Ingress,
			ingressTarget, ingressPort, labels)
		if err := ctrl.SetControllerReference(&bridge, desiredIng, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingIng := &networkingv1.Ingress{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingIng, func() error {
			existingIng.Name = desiredIng.Name
			existingIng.Namespace = desiredIng.Namespace
			return manifests.MutateFuncFor(existingIng, desiredIng)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status.
	var currentServer appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(serverDep), &currentServer); err == nil {
		bridge.Status.ServerReady = currentServer.Status.ReadyReplicas > 0
	}
	if uiDep != nil {
		var currentUI appsv1.Deployment
		if err := r.Get(ctx, client.ObjectKeyFromObject(uiDep), &currentUI); err == nil {
			bridge.Status.UIReady = currentUI.Status.ReadyReplicas > 0
		}
	} else {
		bridge.Status.UIReady = true
	}

	bridge.Status.ObservedGeneration = bridge.Generation
	if bridge.Status.ServerReady && bridge.Status.UIReady {
		bridge.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&bridge.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Bridge is ready")
	} else {
		bridge.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&bridge.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady", "Bridge components are starting")
	}

	if err := r.Status().Update(ctx, &bridge); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Bridge", "phase", bridge.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *BridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Bridge{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
