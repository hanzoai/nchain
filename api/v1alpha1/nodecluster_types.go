package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeClusterSpec defines the desired state of a blockchain node cluster.
type NodeClusterSpec struct {
	// Protocol selects the blockchain-specific driver.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=lux;ethereum;bitcoin;cosmos;substrate;generic
	Protocol string `json:"protocol"`

	// Replicas is the number of nodes.
	// +kubebuilder:default=3
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Role defines the node role.
	// +kubebuilder:default="validator"
	// +kubebuilder:validation:Enum=validator;fullnode;archive;bootnode;sentry
	// +optional
	Role string `json:"role,omitempty"`

	// Image for the node container.
	// +kubebuilder:validation:Required
	Image ImageSpec `json:"image"`

	// NetworkID is the blockchain network identifier.
	// +kubebuilder:validation:Required
	NetworkID string `json:"networkID"`

	// Ports configuration.
	// +optional
	Ports PortConfig `json:"ports,omitempty"`

	// Storage for persistent chain data.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// Resources (CPU/memory) for the node container.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Consensus configuration (protocol-specific).
	// +optional
	Consensus *ConsensusConfig `json:"consensus,omitempty"`

	// P2P networking configuration.
	// +optional
	P2P P2PConfig `json:"p2p,omitempty"`

	// API/RPC configuration.
	// +optional
	API APIConfig `json:"api,omitempty"`

	// Keys configures cryptographic key management.
	// +optional
	Keys *KeyManagementSpec `json:"keys,omitempty"`

	// SeedRestore enables fast bootstrap from a seed or snapshot.
	// +optional
	SeedRestore *SeedRestoreSpec `json:"seedRestore,omitempty"`

	// SnapshotSchedule configures automated periodic backups.
	// +optional
	SnapshotSchedule *SnapshotScheduleSpec `json:"snapshotSchedule,omitempty"`

	// UpgradeStrategy controls how node upgrades are rolled out.
	// +optional
	UpgradeStrategy UpgradeStrategySpec `json:"upgradeStrategy,omitempty"`

	// HealthPolicy configures degraded detection.
	// +optional
	HealthPolicy HealthPolicySpec `json:"healthPolicy,omitempty"`

	// StartupGate waits for peers before starting nodes.
	// +optional
	StartupGate *StartupGateSpec `json:"startupGate,omitempty"`

	// Chains lists the blockchain networks this cluster tracks.
	// +optional
	Chains []ChainRef `json:"chains,omitempty"`

	// Init configures init containers for setup and plugins.
	// +optional
	Init *InitSpec `json:"init,omitempty"`

	// ProtocolConfig is opaque JSON passed to the protocol driver.
	// Use this for protocol-specific flags that don't map to common fields.
	// +optional
	ProtocolConfig *apiextensionsv1.JSON `json:"protocolConfig,omitempty"`

	// Security sets the pod security context.
	// +optional
	Security SecuritySpec `json:"security,omitempty"`

	// Env is a list of additional environment variables for the node container.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// EnvFrom is a list of sources for environment variables.
	// +optional
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Labels are additional labels applied to all managed resources.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are additional annotations applied to all managed resources.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// ImagePullSecrets lists references to secrets for pulling container images.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// KMSSecrets lists KMS secret references to sync into Kubernetes Secrets.
	// +optional
	KMSSecrets []KMSSecretRef `json:"kmsSecrets,omitempty"`
}

// NodeClusterStatus defines the observed state of NodeCluster.
type NodeClusterStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas is the number of nodes that are ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// CurrentReplicas is the total number of running node pods.
	// +optional
	CurrentReplicas int32 `json:"currentReplicas,omitempty"`

	// BootstrapComplete indicates whether the cluster has completed initial sync.
	// +optional
	BootstrapComplete bool `json:"bootstrapComplete,omitempty"`

	// Conditions represent the latest observations of the cluster's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=nc
// +kubebuilder:printcolumn:name="Protocol",type=string,JSONPath=`.spec.protocol`
// +kubebuilder:printcolumn:name="Role",type=string,JSONPath=`.spec.role`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// NodeCluster is the Schema for the nodeclusters API.
// It manages a set of blockchain nodes (validators, full nodes, archive nodes, etc.)
// for any supported protocol.
type NodeCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NodeClusterSpec   `json:"spec,omitempty"`
	Status NodeClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NodeClusterList contains a list of NodeCluster.
type NodeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []NodeCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&NodeCluster{}, &NodeClusterList{})
}
