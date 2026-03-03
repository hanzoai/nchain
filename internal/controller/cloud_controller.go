package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/status"
)

// CloudReconciler reconciles a Cloud object.
type CloudReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=clouds,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=clouds/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=autoscaling,resources=horizontalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

func (r *CloudReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("cloud", req.NamespacedName)

	var cloud v1alpha1.Cloud
	if err := r.Get(ctx, req.NamespacedName, &cloud); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Reconcile API Deployment.
	apiDep := manifests.BuildCloudAPIDeployment(&cloud)
	if err := r.reconcileOwned(ctx, &cloud, apiDep); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling API deployment: %w", err)
	}

	// Reconcile API Service.
	apiSvcName := cloud.Name + "-api"
	apiSvc := manifests.BuildCloudService(apiSvcName, cloud.Namespace, 8000,
		manifests.SelectorLabels(apiSvcName))
	if err := r.reconcileOwned(ctx, &cloud, apiSvc); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling API service: %w", err)
	}

	// Reconcile API PDB.
	apiPDB := manifests.BuildPDB(apiSvcName, cloud.Namespace, manifests.SelectorLabels(apiSvcName))
	if err := r.reconcileOwned(ctx, &cloud, apiPDB); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling API PDB: %w", err)
	}

	// Reconcile API HPA.
	if cloud.Spec.API.Autoscaling != nil && cloud.Spec.API.Autoscaling.Enabled {
		apiHPA := manifests.BuildCloudHPA(apiSvcName, cloud.Namespace, apiSvcName, cloud.Spec.API.Autoscaling)
		if err := r.reconcileOwned(ctx, &cloud, apiHPA); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling API HPA: %w", err)
		}
	}

	// Reconcile single API Ingress (multi-host for all brands' API/WS domains).
	if cloud.Spec.Ingress != nil && cloud.Spec.Ingress.Enabled {
		apiIng := manifests.BuildCloudAPIIngress(&cloud)
		if err := r.reconcileOwned(ctx, &cloud, apiIng); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling API ingress: %w", err)
		}
	}

	// Reconcile per-brand web resources.
	var totalWebReady int32
	for _, brand := range cloud.Spec.Brands {
		// Web Deployment.
		webDep := manifests.BuildCloudWebDeployment(&cloud, brand)
		if err := r.reconcileOwned(ctx, &cloud, webDep); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling web deployment for %s: %w", brand.Name, err)
		}

		// Web Service.
		webSvcName := cloud.Name + "-web-" + brand.Name
		webSvc := manifests.BuildCloudService(webSvcName, cloud.Namespace, 3001,
			manifests.SelectorLabels(webSvcName))
		if err := r.reconcileOwned(ctx, &cloud, webSvc); err != nil {
			return ctrl.Result{}, fmt.Errorf("reconciling web service for %s: %w", brand.Name, err)
		}

		// Web HPA.
		if cloud.Spec.Web.Autoscaling != nil && cloud.Spec.Web.Autoscaling.Enabled {
			webHPA := manifests.BuildCloudHPA(webSvcName, cloud.Namespace, webSvcName, cloud.Spec.Web.Autoscaling)
			if err := r.reconcileOwned(ctx, &cloud, webHPA); err != nil {
				return ctrl.Result{}, fmt.Errorf("reconciling web HPA for %s: %w", brand.Name, err)
			}
		}

		// Web Ingress per brand (web domain only).
		if cloud.Spec.Ingress != nil && cloud.Spec.Ingress.Enabled {
			webIng := manifests.BuildCloudWebIngress(&cloud, brand)
			if err := r.reconcileOwned(ctx, &cloud, webIng); err != nil {
				return ctrl.Result{}, fmt.Errorf("reconciling web ingress for %s: %w", brand.Name, err)
			}
		}

		// Read web deployment status.
		var currentWeb appsv1.Deployment
		if err := r.Get(ctx, client.ObjectKeyFromObject(webDep), &currentWeb); err == nil {
			totalWebReady += currentWeb.Status.ReadyReplicas
		}
	}

	// Update status.
	var currentAPI appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(apiDep), &currentAPI); err == nil {
		cloud.Status.APIReadyReplicas = currentAPI.Status.ReadyReplicas
	}
	cloud.Status.WebReadyReplicas = totalWebReady
	cloud.Status.BrandCount = int32(len(cloud.Spec.Brands))
	cloud.Status.ObservedGeneration = cloud.Generation

	apiTarget := derefInt32(cloud.Spec.API.Replicas, 3)
	webTarget := derefInt32(cloud.Spec.Web.Replicas, 2) * cloud.Status.BrandCount
	if cloud.Status.APIReadyReplicas >= apiTarget && cloud.Status.WebReadyReplicas >= webTarget {
		cloud.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&cloud.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "All Cloud components are ready")
	} else if cloud.Status.APIReadyReplicas > 0 || cloud.Status.WebReadyReplicas > 0 {
		cloud.Status.Phase = v1alpha1.PhaseBootstrapping
		status.SetCondition(&cloud.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "Bootstrapping",
			fmt.Sprintf("API %d/%d, Web %d/%d", cloud.Status.APIReadyReplicas, apiTarget,
				cloud.Status.WebReadyReplicas, webTarget))
	} else {
		cloud.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&cloud.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "Creating", "Cloud resources are being created")
	}

	if err := r.Status().Update(ctx, &cloud); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Cloud", "phase", cloud.Status.Phase,
		"brands", cloud.Status.BrandCount,
		"apiReady", cloud.Status.APIReadyReplicas,
		"webReady", cloud.Status.WebReadyReplicas)

	return ctrl.Result{}, nil
}

// reconcileOwned creates or updates an owned resource with controller reference.
func (r *CloudReconciler) reconcileOwned(ctx context.Context, cloud *v1alpha1.Cloud, desired client.Object) error {
	if err := ctrl.SetControllerReference(cloud, desired, r.Scheme); err != nil {
		return err
	}
	existing := desired.DeepCopyObject().(client.Object)
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.SetName(desired.GetName())
		existing.SetNamespace(desired.GetNamespace())
		return manifests.MutateFuncFor(existing, desired)()
	})
	return err
}

func (r *CloudReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Cloud{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Owns(&autoscalingv2.HorizontalPodAutoscaler{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
