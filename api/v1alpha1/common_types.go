package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// Phase represents the lifecycle phase of a managed resource.
// +kubebuilder:validation:Enum=Pending;Creating;Bootstrapping;Running;Degraded;Deleting
type Phase string

const (
	PhasePending       Phase = "Pending"
	PhaseCreating      Phase = "Creating"
	PhaseBootstrapping Phase = "Bootstrapping"
	PhaseRunning       Phase = "Running"
	PhaseDegraded      Phase = "Degraded"
	PhaseDeleting      Phase = "Deleting"
)

// ConditionType constants for nchain.hanzo.ai resources.
const (
	ConditionTypeReady       = "Ready"
	ConditionTypeDegraded    = "Degraded"
	ConditionTypeProgressing = "Progressing"
	ConditionTypeReconciled  = "Reconciled"
)

// ImageSpec defines the container image to run.
type ImageSpec struct {
	// Repository is the container image repository.
	// +kubebuilder:validation:Required
	Repository string `json:"repository"`

	// Tag is the container image tag.
	// +kubebuilder:default="latest"
	// +optional
	Tag string `json:"tag,omitempty"`

	// PullPolicy defines when to pull the image.
	// +kubebuilder:default="IfNotPresent"
	// +kubebuilder:validation:Enum=Always;IfNotPresent;Never
	// +optional
	PullPolicy corev1.PullPolicy `json:"pullPolicy,omitempty"`
}

// StorageSpec configures persistent storage.
type StorageSpec struct {
	// StorageClassName specifies the Kubernetes StorageClass to use.
	// +kubebuilder:default="do-block-storage"
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// Size is the requested storage capacity.
	// +kubebuilder:validation:Required
	Size resource.Quantity `json:"size"`

	// RetentionPolicy determines whether the PVC is retained or deleted
	// when the parent resource is removed.
	// +kubebuilder:default="Retain"
	// +kubebuilder:validation:Enum=Retain;Delete
	// +optional
	RetentionPolicy RetentionPolicy `json:"retentionPolicy,omitempty"`
}

// RetentionPolicy determines PVC lifecycle on resource deletion.
// +kubebuilder:validation:Enum=Retain;Delete
type RetentionPolicy string

const (
	RetentionPolicyRetain RetentionPolicy = "Retain"
	RetentionPolicyDelete RetentionPolicy = "Delete"
)

// ResourceRequirements describes CPU and memory resource requests and limits.
type ResourceRequirements struct {
	// Requests describes the minimum amount of compute resources required.
	// +optional
	Requests corev1.ResourceList `json:"requests,omitempty"`

	// Limits describes the maximum amount of compute resources allowed.
	// +optional
	Limits corev1.ResourceList `json:"limits,omitempty"`
}

// IngressSpec configures ingress for a service.
type IngressSpec struct {
	// Enabled controls whether an Ingress resource is created.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Hosts lists the hostnames for the Ingress.
	// +optional
	Hosts []string `json:"hosts,omitempty"`

	// TLS enables TLS termination on the Ingress.
	// +kubebuilder:default=true
	// +optional
	TLS bool `json:"tls,omitempty"`

	// ClusterIssuer is the cert-manager ClusterIssuer to use for TLS certificates.
	// +kubebuilder:default="letsencrypt-prod"
	// +optional
	ClusterIssuer string `json:"clusterIssuer,omitempty"`

	// IngressClassName specifies the Ingress class to use.
	// +kubebuilder:default="ingress"
	// +optional
	IngressClassName string `json:"ingressClassName,omitempty"`

	// Annotations are additional annotations applied to the Ingress resource.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// PortConfig defines network ports for a blockchain node.
type PortConfig struct {
	// RPC is the port for RPC/HTTP API.
	// +kubebuilder:default=8545
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	RPC int32 `json:"rpc,omitempty"`

	// P2P is the port for peer-to-peer networking.
	// +kubebuilder:default=30303
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	P2P int32 `json:"p2p,omitempty"`

	// Metrics is the port for Prometheus metrics.
	// +kubebuilder:default=9090
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Metrics int32 `json:"metrics,omitempty"`

	// Additional lists extra named ports.
	// +optional
	Additional []NamedPort `json:"additional,omitempty"`
}

// NamedPort is a port with a name.
type NamedPort struct {
	// Name is the port name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Port number.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// Protocol is the port protocol (TCP or UDP).
	// +kubebuilder:default="TCP"
	// +kubebuilder:validation:Enum=TCP;UDP
	// +optional
	Protocol corev1.Protocol `json:"protocol,omitempty"`
}

// ConsensusConfig holds consensus-related settings.
type ConsensusConfig struct {
	// Algorithm selects the consensus mechanism.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=snow;tendermint;nakamoto;generic
	Algorithm string `json:"algorithm"`

	// Params are consensus-specific parameters (e.g., snow sample size, block time).
	// Keys and values are protocol-specific and passed through to the driver.
	// +optional
	Params map[string]apiextensionsv1.JSON `json:"params,omitempty"`
}

// P2PConfig configures peer-to-peer networking.
type P2PConfig struct {
	// BootstrapNodes lists initial peer addresses for network bootstrapping.
	// +optional
	BootstrapNodes []string `json:"bootstrapNodes,omitempty"`

	// AllowPrivateIPs permits connections to private IP ranges.
	// +optional
	AllowPrivateIPs bool `json:"allowPrivateIPs,omitempty"`

	// UseHostnames resolves peers by hostname instead of IP.
	// +optional
	UseHostnames bool `json:"useHostnames,omitempty"`

	// ExternalIPs are externally reachable IPs advertised to peers.
	// +optional
	ExternalIPs []string `json:"externalIPs,omitempty"`
}

// APIConfig configures the node's API/RPC interface.
type APIConfig struct {
	// AdminEnabled exposes the admin RPC namespace.
	// +optional
	AdminEnabled bool `json:"adminEnabled,omitempty"`

	// MetricsEnabled exposes the metrics endpoint.
	// +kubebuilder:default=true
	// +optional
	MetricsEnabled *bool `json:"metricsEnabled,omitempty"`

	// IndexEnabled enables on-node transaction indexing.
	// +optional
	IndexEnabled bool `json:"indexEnabled,omitempty"`

	// AllowedHosts restricts which hosts can connect to the API.
	// +optional
	AllowedHosts []string `json:"allowedHosts,omitempty"`
}

// KeyManagementSpec configures cryptographic key management.
type KeyManagementSpec struct {
	// SecretName references a Kubernetes Secret containing node keys.
	// +optional
	SecretName string `json:"secretName,omitempty"`

	// KMS configures fetching keys from Hanzo KMS.
	// +optional
	KMS *KMSKeyConfig `json:"kms,omitempty"`
}

// KMSKeyConfig configures KMS-based key retrieval.
type KMSKeyConfig struct {
	// HostAPI is the KMS API endpoint.
	// +kubebuilder:default="https://kms.hanzo.ai/api"
	// +optional
	HostAPI string `json:"hostAPI,omitempty"`

	// ProjectSlug identifies the KMS project.
	// +kubebuilder:validation:Required
	ProjectSlug string `json:"projectSlug"`

	// EnvSlug identifies the KMS environment.
	// +kubebuilder:validation:Required
	EnvSlug string `json:"envSlug"`

	// SecretsPath is the path within the KMS project.
	// +kubebuilder:validation:Required
	SecretsPath string `json:"secretsPath"`

	// CredentialsRef references a Kubernetes Secret containing KMS credentials.
	// +kubebuilder:validation:Required
	CredentialsRef corev1.SecretReference `json:"credentialsRef"`

	// ResyncInterval is the interval in seconds between KMS secret re-syncs.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=10
	// +optional
	ResyncInterval int32 `json:"resyncInterval,omitempty"`
}

// KMSSecretRef generates a KMSSecret CR to sync secrets from Hanzo KMS into a Kubernetes Secret.
type KMSSecretRef struct {
	// HostAPI is the KMS API endpoint.
	// +kubebuilder:default="https://kms.hanzo.ai/api"
	// +optional
	HostAPI string `json:"hostAPI,omitempty"`

	// ProjectSlug identifies the KMS project.
	// +kubebuilder:validation:Required
	ProjectSlug string `json:"projectSlug"`

	// EnvSlug identifies the KMS environment.
	// +kubebuilder:validation:Required
	EnvSlug string `json:"envSlug"`

	// SecretsPath is the path within the KMS project.
	// +kubebuilder:validation:Required
	SecretsPath string `json:"secretsPath"`

	// CredentialsRef references a Kubernetes Secret containing KMS credentials.
	// +kubebuilder:validation:Required
	CredentialsRef corev1.SecretReference `json:"credentialsRef"`

	// ResyncInterval is the interval in seconds between KMS secret re-syncs.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=10
	// +optional
	ResyncInterval int32 `json:"resyncInterval,omitempty"`

	// ManagedSecretName is the name of the Kubernetes Secret to create/update.
	// +kubebuilder:validation:Required
	ManagedSecretName string `json:"managedSecretName"`
}

// SeedRestoreSpec configures restoring chain data from a seed or snapshot.
type SeedRestoreSpec struct {
	// Enabled controls whether seed restoration is active.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// SourceType selects the restore mechanism.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=VolumeSnapshot;PVCClone;ObjectStore;None
	SourceType string `json:"sourceType"`

	// ObjectStoreURL is the S3/GCS/MinIO URL for ObjectStore restores.
	// +optional
	ObjectStoreURL string `json:"objectStoreURL,omitempty"`

	// VolumeSnapshotName is the name of the VolumeSnapshot for VolumeSnapshot restores.
	// +optional
	VolumeSnapshotName string `json:"volumeSnapshotName,omitempty"`

	// DonorPVCName is the source PVC name for PVCClone restores.
	// +optional
	DonorPVCName string `json:"donorPVCName,omitempty"`

	// RestorePolicy controls when to restore.
	// +kubebuilder:default="IfEmpty"
	// +kubebuilder:validation:Enum=IfEmpty;Always;Never
	// +optional
	RestorePolicy string `json:"restorePolicy,omitempty"`

	// MarkerPath is the file path used to detect completed restores.
	// +kubebuilder:default="/data/.restore-complete"
	// +optional
	MarkerPath string `json:"markerPath,omitempty"`
}

// SnapshotScheduleSpec configures periodic snapshots of chain data.
type SnapshotScheduleSpec struct {
	// Enabled controls whether snapshotting is active.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// Schedule is a cron expression for snapshot frequency.
	// +kubebuilder:validation:Required
	Schedule string `json:"schedule"`

	// S3Endpoint is the S3-compatible endpoint.
	// +optional
	S3Endpoint string `json:"s3Endpoint,omitempty"`

	// S3Bucket is the target bucket.
	// +optional
	S3Bucket string `json:"s3Bucket,omitempty"`

	// S3CredentialsSecret references a Secret with S3 credentials.
	// +optional
	S3CredentialsSecret string `json:"s3CredentialsSecret,omitempty"`

	// RetentionCount is the number of snapshots to retain.
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	// +optional
	RetentionCount int32 `json:"retentionCount,omitempty"`
}

// UpgradeStrategyType defines how node upgrades are performed.
// +kubebuilder:validation:Enum=OnDelete;RollingCanary;RollingUpdate
type UpgradeStrategyType string

const (
	UpgradeOnDelete       UpgradeStrategyType = "OnDelete"
	UpgradeRollingCanary  UpgradeStrategyType = "RollingCanary"
	UpgradeRollingUpdate  UpgradeStrategyType = "RollingUpdate"
)

// UpgradeStrategySpec configures how node upgrades are rolled out.
type UpgradeStrategySpec struct {
	// Type selects the upgrade strategy.
	// +kubebuilder:default="RollingUpdate"
	// +optional
	Type UpgradeStrategyType `json:"type,omitempty"`

	// MaxUnavailable is the maximum number of nodes that can be unavailable during an upgrade.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxUnavailable *int32 `json:"maxUnavailable,omitempty"`

	// HealthCheckBetweenRestarts requires health verification between each node restart.
	// +kubebuilder:default=true
	// +optional
	HealthCheckBetweenRestarts *bool `json:"healthCheckBetweenRestarts,omitempty"`

	// StabilizationSeconds is the time to wait after each node restart before proceeding.
	// +kubebuilder:default=60
	// +kubebuilder:validation:Minimum=0
	// +optional
	StabilizationSeconds int32 `json:"stabilizationSeconds,omitempty"`
}

// HealthPolicySpec configures health monitoring for degraded detection.
type HealthPolicySpec struct {
	// RequireInboundPeers marks the node degraded if it has no inbound peers.
	// +kubebuilder:default=true
	// +optional
	RequireInboundPeers *bool `json:"requireInboundPeers,omitempty"`

	// MinInbound is the minimum number of inbound peers before marking degraded.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinInbound int32 `json:"minInbound,omitempty"`

	// GracePeriodSeconds is the time to wait after startup before enforcing health checks.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	// +optional
	GracePeriodSeconds int32 `json:"gracePeriodSeconds,omitempty"`

	// MaxHeightSkew is the maximum allowed block height difference from the best known height.
	// +kubebuilder:default=100
	// +kubebuilder:validation:Minimum=0
	// +optional
	MaxHeightSkew int64 `json:"maxHeightSkew,omitempty"`

	// CheckIntervalSeconds is how often the health check runs.
	// +kubebuilder:default=30
	// +kubebuilder:validation:Minimum=5
	// +optional
	CheckIntervalSeconds int32 `json:"checkIntervalSeconds,omitempty"`
}

// TimeoutAction defines what happens when a startup gate times out.
// +kubebuilder:validation:Enum=Fail;StartAnyway
type TimeoutAction string

const (
	TimeoutActionFail        TimeoutAction = "Fail"
	TimeoutActionStartAnyway TimeoutAction = "StartAnyway"
)

// StartupGateSpec configures gating node startup until peers are available.
type StartupGateSpec struct {
	// Enabled controls whether startup gating is active.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// MinPeers is the minimum number of peers that must be reachable before starting.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=0
	// +optional
	MinPeers int32 `json:"minPeers,omitempty"`

	// WaitForHealthyPeer requires at least one peer to be healthy (synced).
	// +optional
	WaitForHealthyPeer bool `json:"waitForHealthyPeer,omitempty"`

	// TimeoutSeconds is the maximum time to wait for peers.
	// +kubebuilder:default=300
	// +kubebuilder:validation:Minimum=0
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`

	// CheckIntervalSeconds is how often to check for peers.
	// +kubebuilder:default=10
	// +kubebuilder:validation:Minimum=1
	// +optional
	CheckIntervalSeconds int32 `json:"checkIntervalSeconds,omitempty"`

	// OnTimeout defines what happens when the gate times out.
	// +kubebuilder:default="Fail"
	// +optional
	OnTimeout TimeoutAction `json:"onTimeout,omitempty"`
}

// ChainRef references a blockchain tracked by a NodeCluster.
type ChainRef struct {
	// BlockchainID is the chain's unique identifier.
	// +kubebuilder:validation:Required
	BlockchainID string `json:"blockchainID"`

	// Alias is a human-readable alias for the chain.
	// +optional
	Alias string `json:"alias,omitempty"`

	// TrackingID is an operator-internal tracking identifier.
	// +optional
	TrackingID string `json:"trackingID,omitempty"`
}

// PluginSpec defines a plugin to install via init container.
type PluginSpec struct {
	// URL is the download URL for the plugin binary.
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Name is the plugin file name.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// DestPath is where to place the plugin binary inside the container.
	// +kubebuilder:default="/plugins"
	// +optional
	DestPath string `json:"destPath,omitempty"`
}

// InitSpec configures init containers for setup, plugins, etc.
type InitSpec struct {
	// Image is the init container image.
	// +kubebuilder:validation:Required
	Image ImageSpec `json:"image"`

	// Plugins lists plugins to download and install.
	// +optional
	Plugins []PluginSpec `json:"plugins,omitempty"`

	// ClearData removes existing chain data before starting.
	// +optional
	ClearData bool `json:"clearData,omitempty"`
}

// SecuritySpec sets pod security context fields.
type SecuritySpec struct {
	// RunAsUser sets the UID for the container process.
	// +optional
	RunAsUser *int64 `json:"runAsUser,omitempty"`

	// RunAsGroup sets the primary GID for the container process.
	// +optional
	RunAsGroup *int64 `json:"runAsGroup,omitempty"`

	// FSGroup sets the filesystem group for volumes.
	// +optional
	FSGroup *int64 `json:"fsGroup,omitempty"`
}

// DatabaseSpec configures a database for explorer or other services.
type DatabaseSpec struct {
	// Type selects the database engine.
	// +kubebuilder:default="postgresql"
	// +kubebuilder:validation:Enum=postgresql;sqlite
	// +optional
	Type string `json:"type,omitempty"`

	// Image is the database container image (ignored for sqlite).
	// +optional
	Image *ImageSpec `json:"image,omitempty"`

	// Storage configures persistent storage for the database.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// CredentialsSecret references a Secret containing database credentials.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`
}

// S3SnapshotSpec configures S3-based snapshots for an indexer.
type S3SnapshotSpec struct {
	// Endpoint is the S3-compatible endpoint.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// Bucket is the target bucket.
	// +optional
	Bucket string `json:"bucket,omitempty"`

	// CredentialsSecret references a Secret with S3 credentials.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// Schedule is a cron expression for snapshot frequency.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// RetentionCount is the number of snapshots to retain.
	// +kubebuilder:default=5
	// +kubebuilder:validation:Minimum=1
	// +optional
	RetentionCount int32 `json:"retentionCount,omitempty"`
}

// CloudSpec configures the cloud management platform (bootnode API + UI).
type CloudSpec struct {
	// Enabled controls whether the cloud management platform is deployed.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// APIImage is the bootnode API container image.
	// +optional
	APIImage *ImageSpec `json:"apiImage,omitempty"`

	// WebImage is the cloud UI container image.
	// +optional
	WebImage *ImageSpec `json:"webImage,omitempty"`

	// Replicas is the number of API replicas.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Ingress configures external access to the cloud platform.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Database configures storage for the cloud platform.
	// +optional
	Database *DatabaseSpec `json:"database,omitempty"`
}

// GatewayRoute defines a single route in a Gateway.
type GatewayRoute struct {
	// Prefix is the URL path prefix to match.
	// +kubebuilder:validation:Required
	Prefix string `json:"prefix"`

	// Backend is the service name (or NodeCluster chain alias) to route to.
	// +kubebuilder:validation:Required
	Backend string `json:"backend"`

	// Methods restricts the route to specific HTTP methods.
	// +optional
	Methods []string `json:"methods,omitempty"`

	// StripPrefix removes the matched prefix before forwarding.
	// +optional
	StripPrefix bool `json:"stripPrefix,omitempty"`

	// RateLimit overrides the global rate limit for this route.
	// +optional
	RateLimit *RateLimitConfig `json:"rateLimit,omitempty"`
}

// RateLimitConfig configures rate limiting.
type RateLimitConfig struct {
	// MaxRate is the maximum number of requests in the time window.
	// +kubebuilder:validation:Minimum=1
	// +optional
	MaxRate int32 `json:"maxRate,omitempty"`

	// Every is the time window duration (e.g., "1s", "1m").
	// +optional
	Every string `json:"every,omitempty"`

	// ClientMaxRate is the per-client rate limit.
	// +kubebuilder:validation:Minimum=1
	// +optional
	ClientMaxRate int32 `json:"clientMaxRate,omitempty"`
}
