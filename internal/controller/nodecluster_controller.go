package controller

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/protocol"
	"github.com/hanzoai/nchain/internal/status"
)

// NodeClusterReconciler reconciles a NodeCluster object.
type NodeClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=nodeclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=nodeclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=nchain.hanzo.ai,resources=nodeclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services;configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete

func (r *NodeClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("nodecluster", req.NamespacedName)

	// Fetch the NodeCluster CR.
	var cluster v1alpha1.NodeCluster
	if err := r.Get(ctx, req.NamespacedName, &cluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Get protocol driver.
	driver, ok := protocol.Get(cluster.Spec.Protocol)
	if !ok {
		log.Error(fmt.Errorf("unknown protocol: %s", cluster.Spec.Protocol), "Failed to get driver")
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "UnknownProtocol",
			fmt.Sprintf("Protocol %q is not supported", cluster.Spec.Protocol))
		cluster.Status.Phase = v1alpha1.PhaseDegraded
		return ctrl.Result{}, r.Status().Update(ctx, &cluster)
	}

	// Set phase to Creating if not yet set.
	if cluster.Status.Phase == "" || cluster.Status.Phase == v1alpha1.PhasePending {
		cluster.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeProgressing,
			metav1.ConditionTrue, "Creating", "Building resources")
		if err := r.Status().Update(ctx, &cluster); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Build and reconcile StatefulSet.
	desiredSts := manifests.BuildNodeClusterStatefulSet(&cluster, driver)
	if err := ctrl.SetControllerReference(&cluster, desiredSts, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingSts := &appsv1.StatefulSet{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingSts, func() error {
		existingSts.Name = desiredSts.Name
		existingSts.Namespace = desiredSts.Namespace
		return manifests.MutateFuncFor(existingSts, desiredSts)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Build and reconcile headless Service.
	desiredHeadless := manifests.BuildNodeClusterHeadlessService(&cluster)
	if err := ctrl.SetControllerReference(&cluster, desiredHeadless, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingHeadless := &corev1.Service{}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingHeadless, func() error {
		existingHeadless.Name = desiredHeadless.Name
		existingHeadless.Namespace = desiredHeadless.Namespace
		return manifests.MutateFuncFor(existingHeadless, desiredHeadless)()
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Build and reconcile regular Service.
	desiredSvc := manifests.BuildNodeClusterService(&cluster)
	if err := ctrl.SetControllerReference(&cluster, desiredSvc, r.Scheme); err != nil {
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

	// Build and reconcile ConfigMap if driver needs it.
	if driver.NeedsConfigMap(&cluster.Spec) {
		desiredCM := manifests.BuildNodeClusterConfigMap(&cluster, driver)
		if err := ctrl.SetControllerReference(&cluster, desiredCM, r.Scheme); err != nil {
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
	}

	// Build PDB if replicas > 1.
	replicas := derefInt32(cluster.Spec.Replicas, 3)
	if replicas > 1 {
		desiredPDB := manifests.BuildPDB(cluster.Name, cluster.Namespace, manifests.SelectorLabels(cluster.Name))
		if err := ctrl.SetControllerReference(&cluster, desiredPDB, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingPDB := &policyv1.PodDisruptionBudget{}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingPDB, func() error {
			existingPDB.Name = desiredPDB.Name
			existingPDB.Namespace = desiredPDB.Namespace
			return manifests.MutateFuncFor(existingPDB, desiredPDB)()
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Update status from StatefulSet.
	var currentSts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredSts), &currentSts); err == nil {
		cluster.Status.ReadyReplicas = currentSts.Status.ReadyReplicas
		cluster.Status.CurrentReplicas = currentSts.Status.CurrentReplicas
	}

	// Determine phase.
	cluster.Status.ObservedGeneration = cluster.Generation
	if cluster.Status.ReadyReplicas == replicas {
		cluster.Status.Phase = v1alpha1.PhaseRunning
		cluster.Status.BootstrapComplete = true
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "AllReplicasReady", "All node replicas are ready")
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeProgressing,
			metav1.ConditionFalse, "Complete", "Reconciliation complete")
	} else if cluster.Status.ReadyReplicas > 0 {
		cluster.Status.Phase = v1alpha1.PhaseBootstrapping
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "PartiallyReady",
			fmt.Sprintf("%d/%d replicas ready", cluster.Status.ReadyReplicas, replicas))
	} else {
		cluster.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NoReplicasReady", "No replicas are ready yet")
	}

	if err := r.Status().Update(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled NodeCluster", "phase", cluster.Status.Phase,
		"ready", cluster.Status.ReadyReplicas, "total", replicas)

	return ctrl.Result{}, nil
}

func (r *NodeClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NodeCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
