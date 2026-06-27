package controller

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
	"github.com/hanzoai/nchain/internal/status"
)

const (
	// indexerPortDefault matches the ghcr.io/luxfi/indexer HTTP listener.
	indexerPortDefault = int32(8091)
	// indexerPollDefault is the default block poll interval in seconds.
	indexerPollDefault = int32(2)
	// indexerDataMount is where the indexer writes its embedded SQLite store.
	indexerDataMount = "/data"
	// indexerDataVolume is the pod volume name backing indexerDataMount.
	indexerDataVolume = "data"
	// indexerDataSizeDefault sizes the SQLite PVC when no storage is requested.
	indexerDataSizeDefault = "10Gi"
	// indexerRunAsUser is the non-root UID/GID/FSGroup of the luxfi/indexer image.
	indexerRunAsUser = int64(65532)
	// nodeClusterRPCPortDefault is the fallback chain RPC port used for endpoint
	// discovery when the referenced NodeCluster cannot be read.
	nodeClusterRPCPortDefault = int32(9650)
	indexerRequeueInterval    = 60 * time.Second
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
// +kubebuilder:rbac:groups="",resources=services;persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
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

	port := indexer.Spec.Port
	if port == 0 {
		port = indexerPortDefault
	}

	labels := manifests.StandardLabels(indexer.Name, "indexer", "", "")
	selectorLbls := manifests.SelectorLabels(indexer.Name)

	// Persistent volume for the embedded SQLite store. The indexer is a single
	// writer over a ReadWriteOnce PVC; high availability comes from streaming
	// replication + restore-on-boot, not from multiple live replicas.
	pvcName := indexer.Name + "-data"
	if err := r.reconcileDataPVC(ctx, &indexer, pvcName); err != nil {
		return ctrl.Result{}, err
	}

	// Discover RPC/WS endpoints from the parent NodeCluster headless service.
	rpcEndpoint, wsEndpoint := r.resolveEndpoints(ctx, &indexer)

	// Build the indexer Deployment from the shared builder, then layer on the
	// SQLite volume, behavioural env vars, feature flags, replication and the
	// seed-restore init container.
	desiredDep := manifests.BuildIndexerDeployment(&indexer)
	augmentIndexerDeployment(desiredDep, &indexer, indexerDeployConfig{
		rpcEndpoint: rpcEndpoint,
		wsEndpoint:  wsEndpoint,
		port:        port,
		pvcName:     pvcName,
		image:       indexerImage(&indexer),
	})
	if err := ctrl.SetControllerReference(&indexer, desiredDep, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingDep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: desiredDep.Name, Namespace: desiredDep.Namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingDep, manifests.MutateFuncFor(existingDep, desiredDep)); err != nil {
		return ctrl.Result{}, err
	}

	// Service for the indexer API.
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
				{Name: "http", Port: port, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
	if err := ctrl.SetControllerReference(&indexer, desiredSvc, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}
	existingSvc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: desiredSvc.Name, Namespace: desiredSvc.Namespace}}
	if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingSvc, manifests.MutateFuncFor(existingSvc, desiredSvc)); err != nil {
		return ctrl.Result{}, err
	}

	// Ingress (optional).
	if indexer.Spec.Ingress != nil && indexer.Spec.Ingress.Enabled {
		desiredIng := manifests.BuildIngress(indexer.Name, indexer.Namespace, indexer.Spec.Ingress,
			indexer.Name, port, labels)
		if err := ctrl.SetControllerReference(&indexer, desiredIng, r.Scheme); err != nil {
			return ctrl.Result{}, err
		}
		existingIng := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: desiredIng.Name, Namespace: desiredIng.Namespace}}
		if _, err := ctrl.CreateOrUpdate(ctx, r.Client, existingIng, manifests.MutateFuncFor(existingIng, desiredIng)); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Status: read the live Deployment for readiness.
	var currentDep appsv1.Deployment
	if err := r.Get(ctx, client.ObjectKeyFromObject(desiredDep), &currentDep); err == nil {
		indexer.Status.ReadyReplicas = currentDep.Status.ReadyReplicas
	}

	indexer.Status.ObservedGeneration = indexer.Generation
	indexer.Status.APIEndpoint = fmt.Sprintf("http://%s.%s.svc:%d", indexer.Name, indexer.Namespace, port)
	// The store is embedded in the pod, so it is ready once the pod is ready.
	indexer.Status.DatabaseReady = indexer.Status.ReadyReplicas >= 1

	// Single-writer SQLite: the Deployment runs exactly one pod.
	if indexer.Status.ReadyReplicas >= 1 {
		indexer.Status.Phase = v1alpha1.PhaseRunning
		status.SetCondition(&indexer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionTrue, "Ready", "Indexer is ready")
	} else {
		indexer.Status.Phase = v1alpha1.PhaseCreating
		status.SetCondition(&indexer.Status.Conditions, v1alpha1.ConditionTypeReady,
			metav1.ConditionFalse, "NotReady", "Indexer pod is not ready yet")
	}

	if err := r.Status().Update(ctx, &indexer); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Reconciled Indexer", "phase", indexer.Status.Phase, "databaseReady", indexer.Status.DatabaseReady)
	return ctrl.Result{RequeueAfter: indexerRequeueInterval}, nil
}

// reconcileDataPVC ensures the SQLite data PVC exists. PVCs are largely
// immutable, so it is created once and then left untouched. By default the PVC
// is retained across Indexer deletion (no owner reference); set Storage
// RetentionPolicy=Delete to have it garbage-collected with the Indexer.
func (r *IndexerReconciler) reconcileDataPVC(ctx context.Context, indexer *v1alpha1.Indexer, pvcName string) error {
	pvc := buildIndexerDataPVC(indexer, pvcName)
	if s := indexer.Spec.Storage; s != nil && s.RetentionPolicy == v1alpha1.RetentionPolicyDelete {
		if err := ctrl.SetControllerReference(indexer, pvc, r.Scheme); err != nil {
			return err
		}
	}

	existing := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, client.ObjectKeyFromObject(pvc), existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, pvc)
	}
	return err
}

// buildIndexerDataPVC builds the ReadWriteOnce PVC backing the embedded store.
func buildIndexerDataPVC(indexer *v1alpha1.Indexer, name string) *corev1.PersistentVolumeClaim {
	size := resource.MustParse(indexerDataSizeDefault)
	var storageClass *string
	if s := indexer.Spec.Storage; s != nil {
		if !s.Size.IsZero() {
			size = s.Size
		}
		if s.StorageClassName != "" {
			sc := s.StorageClassName
			storageClass = &sc
		}
	}

	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: indexer.Namespace,
			Labels:    manifests.SelectorLabels(indexer.Name),
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: size},
			},
			StorageClassName: storageClass,
		},
	}
}

// indexerDeployConfig carries resolved values into the Deployment augmentation.
type indexerDeployConfig struct {
	rpcEndpoint string
	wsEndpoint  string
	port        int32
	pvcName     string
	image       string
}

// augmentIndexerDeployment layers the indexer's runtime configuration onto the
// base Deployment built by the shared manifest builder: the SQLite data volume,
// chain/endpoint env, feature-flag env, streaming-replication env, a marker-gated
// seed-restore init container, health probes, the non-root security context and
// the single-writer Recreate strategy.
func augmentIndexerDeployment(dep *appsv1.Deployment, indexer *v1alpha1.Indexer, cfg indexerDeployConfig) {
	if len(dep.Spec.Template.Spec.Containers) == 0 {
		return
	}
	pod := &dep.Spec.Template.Spec
	c := &pod.Containers[0]

	// Single-writer over a ReadWriteOnce PVC: exactly one pod, replaced (not
	// rolled) so the old pod releases the volume before the new one mounts it.
	one := int32(1)
	dep.Spec.Replicas = &one
	dep.Spec.Strategy = appsv1.DeploymentStrategy{Type: appsv1.RecreateDeploymentStrategyType}

	// Embedded SQLite store on the data PVC.
	dataMount := corev1.VolumeMount{Name: indexerDataVolume, MountPath: indexerDataMount}
	ensureVolumeMount(c, dataMount)
	ensureVolume(pod, corev1.Volume{
		Name: indexerDataVolume,
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: cfg.pvcName},
		},
	})

	// CLI flags the luxfi/indexer binary reads for store path + listen address.
	c.Args = []string{"-data=" + indexerDataMount, fmt.Sprintf("-http=:%d", cfg.port)}

	pollInterval := indexer.Spec.PollInterval
	if pollInterval == 0 {
		pollInterval = indexerPollDefault
	}

	c.Env = append(c.Env,
		corev1.EnvVar{Name: "DATA_DIR", Value: indexerDataMount},
		corev1.EnvVar{Name: "HTTP_ADDR", Value: fmt.Sprintf(":%d", cfg.port)},
		corev1.EnvVar{Name: "RPC_ENDPOINT", Value: cfg.rpcEndpoint},
		corev1.EnvVar{Name: "WS_ENDPOINT", Value: cfg.wsEndpoint},
		corev1.EnvVar{Name: "POLL_INTERVAL", Value: strconv.Itoa(int(pollInterval))},
	)

	// Feature flags: only emitted when enabled (matches the upstream operator).
	if indexer.Spec.DefiIndexing {
		c.Env = append(c.Env, corev1.EnvVar{Name: "DEFI_INDEXING", Value: "true"})
	}
	if indexer.Spec.NFTIndexing {
		c.Env = append(c.Env, corev1.EnvVar{Name: "NFT_INDEXING", Value: "true"})
	}
	if indexer.Spec.ContractVerification {
		c.Env = append(c.Env, corev1.EnvVar{Name: "CONTRACT_VERIFICATION", Value: "true"})
	}
	if indexer.Spec.TraceEnabled {
		c.Env = append(c.Env, corev1.EnvVar{Name: "TRACE_ENABLED", Value: "true"})
	}

	// Streaming replication of the embedded store to object storage.
	if env, from := indexerReplicationEnv(indexer.Spec.Replication); env != nil {
		c.Env = append(c.Env, env...)
		c.EnvFrom = append(c.EnvFrom, from...)
	}

	c.Ports = []corev1.ContainerPort{{Name: "http", ContainerPort: cfg.port, Protocol: corev1.ProtocolTCP}}
	c.ReadinessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")},
		},
		InitialDelaySeconds: 15,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
	c.LivenessProbe = &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{Path: "/health", Port: intstr.FromString("http")},
		},
		InitialDelaySeconds: 60,
		PeriodSeconds:       30,
		TimeoutSeconds:      10,
		FailureThreshold:    5,
	}

	// Restore the store from an object-store snapshot before the indexer starts.
	if init := indexerSeedRestoreInit(indexer.Spec.SeedRestore, cfg.image, dataMount); init != nil {
		pod.InitContainers = append([]corev1.Container{*init}, pod.InitContainers...)
	}

	// Non-root security context so the indexer can write to the PVC.
	uid := indexerRunAsUser
	pod.SecurityContext = &corev1.PodSecurityContext{
		RunAsUser:  &uid,
		RunAsGroup: &uid,
		FSGroup:    &uid,
	}
}

// indexerReplicationEnv translates an IndexerReplicationSpec into the REPLICATE_*
// environment the indexer's replicator reads (continuous incrementals + periodic
// physical snapshots, restore-on-boot). Ported from luxfi/operator's
// replicationEnv; the StatefulSet-ordinal election bits are dropped because the
// indexer runs a single writer. Returns nil when replication is disabled.
func indexerReplicationEnv(spec v1alpha1.IndexerReplicationSpec) ([]corev1.EnvVar, []corev1.EnvFromSource) {
	if !spec.Enabled {
		return nil, nil
	}
	env := []corev1.EnvVar{
		{Name: "REPLICATE_S3_ENDPOINT", Value: spec.S3Endpoint},
		{Name: "REPLICATE_S3_USE_SSL", Value: strconv.FormatBool(spec.S3UseSSL)},
		{Name: "REPLICATE_CDC", Value: "true"},
		{Name: "REPLICATE_PHYSICAL_SNAPSHOT", Value: "true"},
		{Name: "REPLICATE_AUTO_ON_OPEN", Value: "true"},
		{Name: "REPLICATE_RESTORE_ON_BOOT", Value: "true"},
		{Name: "REPLICATE_SNAPSHOT_MIN_INCREMENTS", Value: "64"},
	}
	if spec.S3Bucket != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_S3_BUCKET", Value: spec.S3Bucket})
	}
	if spec.S3Region != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_S3_REGION", Value: spec.S3Region})
	}
	if spec.S3Path != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_S3_PATH", Value: spec.S3Path})
	}
	if spec.AgeRecipient != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_AGE_RECIPIENT", Value: spec.AgeRecipient})
	}
	snap := spec.SnapshotIntervalSeconds
	if snap <= 0 {
		snap = 3600
	}
	inc := spec.IncrementalIntervalSeconds
	if inc <= 0 {
		inc = 5
	}
	env = append(env,
		corev1.EnvVar{Name: "REPLICATE_SNAPSHOT_INTERVAL_SECONDS", Value: strconv.FormatInt(snap, 10)},
		corev1.EnvVar{Name: "REPLICATE_INCREMENTAL_INTERVAL_SECONDS", Value: strconv.FormatInt(inc, 10)},
	)

	var from []corev1.EnvFromSource
	if spec.CredentialsSecret != "" {
		from = append(from, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: spec.CredentialsSecret},
			},
		})
	}
	if spec.AgeIdentitySecret != "" {
		from = append(from, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: spec.AgeIdentitySecret},
			},
		})
	}
	return env, from
}

// indexerSeedRestoreInit builds a marker-gated init container that restores the
// data directory from an object-store tarball before the indexer starts. Ported
// from luxfi/operator's seedRestoreInitContainer; returns nil unless enabled with
// SourceType=ObjectStore. The supplied image must contain a shell, curl and tar
// (zstd for .zst tarballs).
func indexerSeedRestoreInit(spec v1alpha1.IndexerSeedRestoreSpec, image string, dataMount corev1.VolumeMount) *corev1.Container {
	if !spec.Enabled || dataMount.Name == "" {
		return nil
	}
	if !strings.EqualFold(spec.SourceType, "ObjectStore") || spec.ObjectStoreURL == "" {
		return nil
	}
	marker := spec.MarkerPath
	if marker == "" {
		marker = indexerDataMount + "/.seeded"
	}
	policy := "IfNotSeeded"
	if strings.EqualFold(spec.RestorePolicy, "Always") {
		policy = "Always"
	}
	script := fmt.Sprintf(`set -eu
MARKER=%q
URL=%q
if [ %q != "Always" ] && [ -f "$MARKER" ]; then
  echo "seed-restore: $MARKER present, skipping"
  exit 0
fi
echo "seed-restore: restoring from $URL"
case "$URL" in
  *.zst)      curl -fsSL "$URL" | zstd -d | tar -xf - -C %s ;;
  *.gz|*.tgz) curl -fsSL "$URL" | tar -xzf - -C %s ;;
  *)          curl -fsSL "$URL" | tar -xf - -C %s ;;
esac
touch "$MARKER"
echo "seed-restore: complete"
`, marker, spec.ObjectStoreURL, policy, indexerDataMount, indexerDataMount, indexerDataMount)

	return &corev1.Container{
		Name:         "seed-restore",
		Image:        image,
		Command:      []string{"/bin/sh", "-c", script},
		VolumeMounts: []corev1.VolumeMount{dataMount},
	}
}

// resolveEndpoints returns the chain RPC and WS endpoints, using explicit spec
// overrides when set, otherwise deriving them from the referenced NodeCluster's
// headless service DNS (pod-0). Ported from the upstream operator's endpoint
// discovery: http://{nc}-0.{nc}-headless.{ns}.svc:{port}/ext/bc/{chain}/rpc.
func (r *IndexerReconciler) resolveEndpoints(ctx context.Context, indexer *v1alpha1.Indexer) (string, string) {
	rpc := indexer.Spec.RPCEndpoint
	ws := indexer.Spec.WSEndpoint
	if rpc != "" && ws != "" {
		return rpc, ws
	}

	net := indexer.Spec.NodeClusterRef
	chain := indexer.Spec.ChainRef
	port := nodeClusterRPCPortDefault
	var cluster v1alpha1.NodeCluster
	if err := r.Get(ctx, client.ObjectKey{Namespace: indexer.Namespace, Name: net}, &cluster); err == nil {
		if cluster.Spec.Ports.RPC > 0 {
			port = cluster.Spec.Ports.RPC
		}
	} else {
		r.Log.Info("NodeCluster not found for endpoint discovery; using default RPC port",
			"nodeCluster", net, "port", port)
	}

	host := fmt.Sprintf("%s-0.%s-headless.%s.svc", net, net, indexer.Namespace)
	if rpc == "" {
		rpc = fmt.Sprintf("http://%s:%d/ext/bc/%s/rpc", host, port, chain)
	}
	if ws == "" {
		ws = fmt.Sprintf("ws://%s:%d/ext/bc/%s/ws", host, port, chain)
	}
	return rpc, ws
}

// indexerImage resolves the container image reference from the spec, matching
// the manifest builder's repository:tag (defaulting an empty tag to "latest").
func indexerImage(indexer *v1alpha1.Indexer) string {
	tag := indexer.Spec.Image.Tag
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", indexer.Spec.Image.Repository, tag)
}

// ensureVolumeMount appends a volume mount unless one with the same name exists.
func ensureVolumeMount(c *corev1.Container, mount corev1.VolumeMount) {
	for _, m := range c.VolumeMounts {
		if m.Name == mount.Name {
			return
		}
	}
	c.VolumeMounts = append(c.VolumeMounts, mount)
}

// ensureVolume appends a pod volume unless one with the same name exists.
func ensureVolume(pod *corev1.PodSpec, vol corev1.Volume) {
	for _, v := range pod.Volumes {
		if v.Name == vol.Name {
			return
		}
	}
	pod.Volumes = append(pod.Volumes, vol)
}

func (r *IndexerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Indexer{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithEventFilter(specChangePred()).
		Complete(r)
}
