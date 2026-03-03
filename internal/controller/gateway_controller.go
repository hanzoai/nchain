package controller

import (
	"context"
	"fmt"

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

// GatewayReconciler reconciles a Gateway object.
type GatewayReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=gateways,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=gateways/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *GatewayReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("gateway", req.NamespacedName)

	var gw v1alpha1.Gateway
	if err := r.Get(ctx, req.NamespacedName, &gw); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Build and reconcile ConfigMap (KrakenD config).
	desiredCM := manifests.BuildGatewayConfigMap(&gw)
	if err := ctrl.SetControllerReference(&gw, desiredCM, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingCM := &corev1.ConfigMap{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingCM, func() error {
		existingCM.Name = desiredCM.Name
		existingCM.Namespace = desiredCM.Namespace
		return manifests.MutateFuncFor(existingCM, desiredCM)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Build and reconcile Deployment.
	desiredDep := manifests.BuildGatewayDeployment(&gw)
	if err := ctrl.SetControllerReference(&gw, desiredDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingDep := &appsv1.Deployment{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingDep, func() error {
		existingDep.Name = desiredDep.Name
		existingDep.Namespace = desiredDep.Namespace
		return manifests.MutateFuncFor(existingDep, desiredDep)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Build and reconcile Service.
	labels := manifests.StandardLabels(gw.Name, "gateway", "", "")
	selectorLbls := manifests.SelectorLabels(gw.Name)
	desiredSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gw.Name,
			Namespace: gw.Namespace,
			Labels:    labels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: selectorLbls,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 8080, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	if err := ctrl.SetControllerReference(&gw, desiredSvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingSvc := &corev1.Service{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingSvc, func() error {
		existingSvc.Name = desiredSvc.Name
		existingSvc.Namespace = desiredSvc.Namespace
		return manifests.MutateFuncFor(existingSvc, desiredSvc)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Build Ingress if enabled.
	if gw.Spec.Ingress != nil && gw.Spec.Ingress.Enabled {
		desiredIng := manifests.BuildIngress(gw.Name, gw.Namespace, gw.Spec.Ingress,
			gw.Name, 8080, labels)
		if err := ctrl.SetControllerReference(&gw, desiredIng, r.Scheme); err != nil {
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
	var currentDep appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredDep), &currentDep); err == nil {
		gw.Status.ReadyReplicas = currentDep.Status.ReadyReplicas
	}
	gw.Status.ActiveRoutes = int32(len(gw.Spec.Routes))

	gw.Status.ObservedGeneration = gw.Generation
	replicas := derefInt32(gw.Spec.Replicas, 2)
	if gw.Status.ReadyReplicas == replicas {
		gw.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&gw.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Gateway is ready")
	} else {
		gw.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&gw.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady",
			fmt.Sprintf("%d/%d replicas ready", gw.Status.ReadyReplicas, replicas))
	}

	if err := r.Status().Update(ctx, &gw); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Gateway", "phase", gw.Status.Phase, "routes", gw.Status.ActiveRoutes)
	return ctrl.Result{}, nil
}

func (r *GatewayReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Gateway{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
