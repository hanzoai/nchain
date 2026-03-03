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

// IndexerReconciler reconciles an Indexer object.
type IndexerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=indexers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=indexers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

func (r *IndexerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("indexer", req.NamespacedName)

	var indexer v1alpha1.Indexer
	if err := r.Get(ctx, req.NamespacedName, &indexer); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Build and reconcile Deployment.
	desiredDep := manifests.BuildIndexerDeployment(&indexer)
	if err := ctrl.SetControllerReference(&indexer, desiredDep, r.Scheme); err != nil {
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
	labels := manifests.StandardLabels(indexer.Name, "indexer", "", "")
	selectorLbls := manifests.SelectorLabels(indexer.Name)
	desiredSvc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      indexer.Name,
			Namespace: indexer.Namespace,
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
	if err := ctrl.SetControllerReference(&indexer, desiredSvc, r.Scheme); err != nil {
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
	if indexer.Spec.Ingress != nil && indexer.Spec.Ingress.Enabled {
		desiredIng := manifests.BuildIngress(indexer.Name, indexer.Namespace, indexer.Spec.Ingress,
			indexer.Name, 8080, labels)
		if err := ctrl.SetControllerReference(&indexer, desiredIng, r.Scheme); err != nil {
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

	// Update status from Deployment.
	var currentDep appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredDep), &currentDep); err == nil {
		indexer.Status.ReadyReplicas = currentDep.Status.ReadyReplicas
	}

	indexer.Status.ObservedGeneration = indexer.Generation
	replicas := derefInt32(indexer.Spec.Replicas, 1)
	if indexer.Status.ReadyReplicas == replicas {
		indexer.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&indexer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Indexer is ready")
	} else {
		indexer.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&indexer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady",
			fmt.Sprintf("%d/%d replicas ready", indexer.Status.ReadyReplicas, replicas))
	}

	if err := r.Status().Update(ctx, &indexer); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Indexer", "phase", indexer.Status.Phase)
	return ctrl.Result{}, nil
}

func (r *IndexerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Indexer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
