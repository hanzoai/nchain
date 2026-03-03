package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NamedNodeCluster is a NodeClusterSpec with a name for inline declaration.
type NamedNodeCluster struct {
	// Name is the name for this node cluster.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Spec is the inline NodeCluster specification.
	// +kubebuilder:validation:Required
	Spec NodeClusterSpec `json:"spec"`
}

// NamedChain is a ChainSpec with a name for inline declaration.
type NamedChain struct {
	// Name is the name for this chain.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Spec is the inline Chain specification.
	// +kubebuilder:validation:Required
	Spec ChainSpec `json:"spec"`
}

// NamedIndexer is an IndexerSpec with a name for inline declaration.
type NamedIndexer struct {
	// Name is the name for this indexer.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Spec is the inline Indexer specification.
	// +kubebuilder:validation:Required
	Spec IndexerSpec `json:"spec"`
}

// NamedExplorer is an ExplorerSpec with a name for inline declaration.
type NamedExplorer struct {
	// Name is the name for this explorer.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Spec is the inline Explorer specification.
	// +kubebuilder:validation:Required
	Spec ExplorerSpec `json:"spec"`
}

// NamedBridge is a BridgeSpec with a name for inline declaration.
type NamedBridge struct {
	// Name is the name for this bridge.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Spec is the inline Bridge specification.
	// +kubebuilder:validation:Required
	Spec BridgeSpec `json:"spec"`
}

// NamedGateway is a GatewaySpec with a name for inline declaration.
type NamedGateway struct {
	// Name is the name for this gateway.
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Spec is the inline Gateway specification.
	// +kubebuilder:validation:Required
	Spec GatewaySpec `json:"spec"`
}

// NetworkSpec defines the desired state of a complete blockchain network.
// Network is the top-level composer that creates and manages all child CRDs.
type NetworkSpec struct {
	// Protocol is inherited by all children unless overridden at the child level.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=lux;ethereum;bitcoin;cosmos;substrate;generic
	Protocol string `json:"protocol"`

	// NetworkID is the blockchain network identifier.
	// +kubebuilder:validation:Required
	NetworkID string `json:"networkID"`

	// Clusters are inline NodeCluster specifications.
	// +optional
	Clusters []NamedNodeCluster `json:"clusters,omitempty"`

	// Chains are inline Chain specifications.
	// +optional
	Chains []NamedChain `json:"chains,omitempty"`

	// Indexers are inline Indexer specifications.
	// +optional
	Indexers []NamedIndexer `json:"indexers,omitempty"`

	// Explorers are inline Explorer specifications.
	// +optional
	Explorers []NamedExplorer `json:"explorers,omitempty"`

	// Bridges are inline Bridge specifications.
	// +optional
	Bridges []NamedBridge `json:"bridges,omitempty"`

	// Gateways are inline Gateway specifications.
	// +optional
	Gateways []NamedGateway `json:"gateways,omitempty"`

	// Cloud configures the cloud management platform (bootnode API + web UI).
	// +optional
	Cloud *CloudSpec `json:"cloud,omitempty"`

	// Labels are additional labels applied to all managed child resources.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are additional annotations applied to all managed child resources.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// NetworkStatus defines the observed state of Network.
type NetworkStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ClusterCount is the number of managed NodeClusters.
	// +optional
	ClusterCount int32 `json:"clusterCount,omitempty"`

	// ChainCount is the number of managed Chains.
	// +optional
	ChainCount int32 `json:"chainCount,omitempty"`

	// ReadyClusters is the number of NodeClusters in Running phase.
	// +optional
	ReadyClusters int32 `json:"readyClusters,omitempty"`

	// Conditions represent the latest observations of the network's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=net
// +kubebuilder:printcolumn:name="Protocol",type=string,JSONPath=`.spec.protocol`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Clusters",type=integer,JSONPath=`.status.clusterCount`
// +kubebuilder:printcolumn:name="Chains",type=integer,JSONPath=`.status.chainCount`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyClusters`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Network is the Schema for the networks API.
// It is the top-level composer that manages an entire blockchain network
// by creating and reconciling NodeClusters, Chains, Indexers, Explorers,
// Bridges, and Gateways.
type Network struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkSpec   `json:"spec,omitempty"`
	Status NetworkStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// NetworkList contains a list of Network.
type NetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Network `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Network{}, &NetworkList{})
}
