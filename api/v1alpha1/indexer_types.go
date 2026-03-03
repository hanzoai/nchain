package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IndexerSpec defines the desired state of a blockchain data indexer.
type IndexerSpec struct {
	// Image for the indexer container.
	// +kubebuilder:validation:Required
	Image ImageSpec `json:"image"`

	// ChainRef is the name of the Chain to index.
	// +kubebuilder:validation:Required
	ChainRef string `json:"chainRef"`

	// NodeClusterRef is the name of the NodeCluster to connect to for chain data.
	// +kubebuilder:validation:Required
	NodeClusterRef string `json:"nodeClusterRef"`

	// Replicas is the number of indexer instances.
	// +kubebuilder:default=1
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Storage configures persistent storage for index data.
	// +optional
	Storage *StorageSpec `json:"storage,omitempty"`

	// Resources (CPU/memory) for the indexer container.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Snapshots configures S3-based snapshots for the indexer data.
	// +optional
	Snapshots *S3SnapshotSpec `json:"snapshots,omitempty"`

	// Ingress configures external access to the indexer API.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`
}

// IndexerStatus defines the observed state of Indexer.
type IndexerStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas is the number of indexer replicas that are ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// IndexedHeight is the latest indexed block height.
	// +optional
	IndexedHeight int64 `json:"indexedHeight,omitempty"`

	// Conditions represent the latest observations of the indexer's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=idx
// +kubebuilder:printcolumn:name="Chain",type=string,JSONPath=`.spec.chainRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Height",type=integer,JSONPath=`.status.indexedHeight`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Indexer is the Schema for the indexers API.
// It deploys a blockchain data indexer for a specific Chain.
type Indexer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IndexerSpec   `json:"spec,omitempty"`
	Status IndexerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IndexerList contains a list of Indexer.
type IndexerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Indexer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Indexer{}, &IndexerList{})
}
