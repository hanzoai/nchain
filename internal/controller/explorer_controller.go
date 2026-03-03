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

// ExplorerReconciler reconciles an Explorer object.
type ExplorerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=explorers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=explorers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *ExplorerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("explorer", req.NamespacedName)

	var explorer v1alpha1.Explorer
	if err := r.Get(ctx, req.NamespacedName, &explorer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Build backend and frontend deployments.
	backendDep, frontendDep := manifests.BuildExplorerDeployments(&explorer)

	// Reconcile backend Deployment.
	if err := ctrl.SetControllerReference(&explorer, backendDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingBackend := &appsv1.Deployment{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingBackend, func() error {
		existingBackend.Name = backendDep.Name
		existingBackend.Namespace = backendDep.Namespace
		return manifests.MutateFuncFor(existingBackend, backendDep)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Backend Service.
	backendLabels := manifests.StandardLabels(explorer.Name+"-backend", "explorer-backend", explorer.Name, "")
	backendSelector := manifests.SelectorLabels(explorer.Name + "-backend")
	backendSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      explorer.Name + "-backend",
			Namespace: explorer.Namespace,
			Labels:    backendLabels,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: backendSelector,
			Ports: []corev1.ServicePort{
				{Name: "http", Port: 4000, Protocol: corev1.ProtocolTCP},
			},
		},
	}
	if err := ctrl.SetControllerReference(&explorer, backendSvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingBackendSvc := &corev1.Service{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingBackendSvc, func() error {
		existingBackendSvc.Name = backendSvc.Name
		existingBackendSvc.Namespace = backendSvc.Namespace
		return manifests.MutateFuncFor(existingBackendSvc, backendSvc)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Reconcile frontend if present.
	if frontendDep != nil {
		if err := ctrl.SetControllerReference(&explorer, frontendDep, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingFrontend := &appsv1.Deployment{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingFrontend, func() error {
			existingFrontend.Name = frontendDep.Name
			existingFrontend.Namespace = frontendDep.Namespace
			return manifests.MutateFuncFor(existingFrontend, frontendDep)()
		}); err != nil {
			return ctrl.Result{}, err
		}

		// Frontend Service.
		frontendLabels := manifests.StandardLabels(explorer.Name+"-frontend", "explorer-frontend", explorer.Name, "")
		frontendSelector := manifests.SelectorLabels(explorer.Name + "-frontend")
		frontendSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      explorer.Name + "-frontend",
				Namespace: explorer.Namespace,
				Labels:    frontendLabels,
			},
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeClusterIP,
				Selector: frontendSelector,
				Ports: []corev1.ServicePort{
					{Name: "http", Port: 3000, Protocol: corev1.ProtocolTCP},
				},
			},
		}
		if err := ctrl.SetControllerReference(&explorer, frontendSvc, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingFrontendSvc := &corev1.Service{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingFrontendSvc, func() error {
			existingFrontendSvc.Name = frontendSvc.Name
			existingFrontendSvc.Namespace = frontendSvc.Namespace
			return manifests.MutateFuncFor(existingFrontendSvc, frontendSvc)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Build Ingress if enabled.
	if explorer.Spec.Ingress != nil && explorer.Spec.Ingress.Enabled {
		// Route ingress to frontend if available, else backend.
		ingressTarget := explorer.Name + "-backend"
		ingressPort := int32(4000)
		if frontendDep != nil {
			ingressTarget = explorer.Name + "-frontend"
			ingressPort = 3000
		}
		labels := manifests.StandardLabels(explorer.Name, "explorer", "", "")
		desiredIng := manifests.BuildIngress(explorer.Name, explorer.Namespace, explorer.Spec.Ingress,
			ingressTarget, ingressPort, labels)
		if err := ctrl.SetControllerReference(&explorer, desiredIng, r.Scheme); err != nil {
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
	var currentBackend appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(backendDep), &currentBackend); err == nil {
		explorer.Status.BackendReady = currentBackend.Status.ReadyReplicas > 0
	}
	if frontendDep != nil {
		var currentFrontend appsv1.Deployment
		if err := r.Get(ctx, client.ObjectKeyFromObject(frontendDep), &currentFrontend); err == nil {
			explorer.Status.FrontendReady = currentFrontend.Status.ReadyReplicas > 0
		}
	} else {
		explorer.Status.FrontendReady = true
	}

	explorer.Status.ObservedGeneration = explorer.Generation
	if explorer.Status.BackendReady && explorer.Status.FrontendReady {
		explorer.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&explorer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Explorer is ready")
	} else {
		explorer.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&explorer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady", "Explorer components are starting")
	}

	if err := r.Status().Update(ctx, &explorer); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Explorer", "phase", explorer.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *ExplorerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Explorer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
