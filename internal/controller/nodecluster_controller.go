package controller

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
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
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete

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

	// Resolve replicas, ports, image, and health endpoint once for reuse below.
	replicas := derefInt32(cluster.Spec.Replicas, 3)
	defaults := driver.DefaultPorts()
	rpcPort := cluster.Spec.Ports.RPC
	if rpcPort == 0 {
		rpcPort = defaults.RPC
	}
	p2pPort := cluster.Spec.Ports.P2P
	if p2pPort == 0 {
		p2pPort = defaults.P2P
	}
	healthPath, healthPort := driver.HealthEndpoint(&cluster.Spec)
	nodeImage := imageRef(cluster.Spec.Image)

	// Build and reconcile StatefulSet.
	desiredSts := manifests.BuildNodeClusterStatefulSet(&cluster, driver)

	// Augment the node container with native streaming-replication env, and add
	// the RLP-import and startup-gate init containers (ported validator behavior).
	// Init order: driver containers (plugins, seed-restore) -> RLP import -> gate.
	replEnv, replEnvFrom := replicationEnv(cluster.Spec.Replication)
	if len(replEnv) > 0 || len(replEnvFrom) > 0 {
		for i := range desiredSts.Spec.Template.Spec.Containers {
			c := &desiredSts.Spec.Template.Spec.Containers[i]
			if c.Name == "node" {
				c.Env = append(c.Env, replEnv...)
				c.EnvFrom = append(c.EnvFrom, replEnvFrom...)
			}
		}
	}
	if c := buildRLPImportInitContainer(&cluster, nodeImage, rpcPort, p2pPort); c != nil {
		desiredSts.Spec.Template.Spec.InitContainers = append(desiredSts.Spec.Template.Spec.InitContainers, *c)
	}
	if c := buildStartupGateInitContainer(&cluster, nodeImage, healthPath, healthPort, replicas); c != nil {
		desiredSts.Spec.Template.Spec.InitContainers = append(desiredSts.Spec.Template.Spec.InitContainers, *c)
	}

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

	// Per-pod LoadBalancer Services for stable external P2P IPs (validator identity).
	if cluster.Spec.PerPodService != nil && cluster.Spec.PerPodService.Enabled {
		for i := int32(0); i < replicas; i++ {
			svc := buildPerPodService(&cluster, i, rpcPort, p2pPort)
			if err := r.reconcileOwned(ctx, &cluster, svc); err != nil {
				return ctrl.Result{}, fmt.Errorf("reconciling per-pod service %d: %w", i, err)
			}
		}
	}

	// Operator-driven periodic snapshot CronJob (backup/DR + seed-restore source).
	if err := r.reconcileSnapshotCronJob(ctx, &cluster, nodeImage, rpcPort); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling snapshot cronjob: %w", err)
	}

	// Update status from StatefulSet.
	var currentSts appsv1.StatefulSet
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredSts), &currentSts); err == nil {
		cluster.Status.ReadyReplicas = currentSts.Status.ReadyReplicas
		cluster.Status.CurrentReplicas = currentSts.Status.CurrentReplicas
	}

	// Poll per-node health for lux validators (populates per-node and aggregate
	// status: nodeID, peers, P/C heights, bootstrapped, healthy, height skew).
	polled := false
	if cluster.Spec.Protocol == "lux" {
		polled = r.pollHealth(ctx, &cluster, rpcPort, replicas)
	}

	// Determine phase: stage by pod readiness, then refine with health for lux.
	cluster.Status.ObservedGeneration = cluster.Generation
	switch {
	case cluster.Status.ReadyReplicas == 0:
		cluster.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NoReplicasReady", "No replicas are ready yet")
	case cluster.Status.ReadyReplicas < replicas:
		cluster.Status.Phase = v1alpha1.PhaseBootstrapping
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "PartiallyReady",
			fmt.Sprintf("%d/%d replicas ready", cluster.Status.ReadyReplicas, replicas))
	case polled && cluster.Status.HealthyReplicas == 0:
		// Pods are ready but no node reports healthy yet — chains still bootstrapping.
		cluster.Status.Phase = v1alpha1.PhaseBootstrapping
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "Bootstrapping", "Pods ready, nodes bootstrapping")
	case polled && (cluster.Status.HealthyReplicas < replicas || len(cluster.Status.DegradedReasons) > 0):
		cluster.Status.Phase = v1alpha1.PhaseDegraded
		msg := fmt.Sprintf("%d/%d nodes healthy", cluster.Status.HealthyReplicas, replicas)
		if len(cluster.Status.DegradedReasons) > 0 {
			msg = strings.Join(cluster.Status.DegradedReasons, "; ")
		}
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "Degraded", msg)
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeDegraded,
			metav1.ConditionTrue, "Degraded", msg)
	default:
		cluster.Status.Phase = v1alpha1.PhaseRunning
		cluster.Status.BootstrapComplete = true
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "AllReplicasReady", "All node replicas are ready")
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeProgressing,
			metav1.ConditionFalse, "Complete", "Reconciliation complete")
		status.SetCondition(&cluster.Status.Conditions, v1alpha1.ConditionTypeDegraded,
			metav1.ConditionFalse, "Healthy", "All nodes healthy")
	}

	if err := r.Status().Update(ctx, &cluster); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled NodeCluster", "phase", cluster.Status.Phase,
		"ready", cluster.Status.ReadyReplicas, "healthy", cluster.Status.HealthyReplicas,
		"total", replicas, "heightSkew", cluster.Status.HeightSkew)

	// Re-poll health periodically for lux validators so status stays fresh
	// (spec-change predicate ignores StatefulSet status-only updates).
	if cluster.Spec.Protocol == "lux" {
		interval := cluster.Spec.HealthPolicy.CheckIntervalSeconds
		if interval <= 0 {
			interval = 30
		}
		return ctrl.Result{RequeueAfter: time.Duration(interval) * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

func (r *NodeClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.NodeCluster{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&policyv1.PodDisruptionBudget{}).
		Owns(&batchv1.CronJob{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
