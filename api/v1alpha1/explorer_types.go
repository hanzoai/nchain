package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExplorerSpec defines the desired state of a block explorer.
type ExplorerSpec struct {
	// BackendImage is the explorer backend container image (e.g., blockscout API).
	// +kubebuilder:validation:Required
	BackendImage ImageSpec `json:"backendImage"`

	// FrontendImage is the optional separate UI container image.
	// +optional
	FrontendImage *ImageSpec `json:"frontendImage,omitempty"`

	// ChainRef is the name of the Chain to explore.
	// +kubebuilder:validation:Required
	ChainRef string `json:"chainRef"`

	// IndexerRef is the optional name of an Indexer dependency.
	// +optional
	IndexerRef string `json:"indexerRef,omitempty"`

	// Database configures the explorer's database.
	// +optional
	Database DatabaseSpec `json:"database,omitempty"`

	// Ingress configures external access to the explorer.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Resources (CPU/memory) for the explorer containers.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// ExplorerStatus defines the observed state of Explorer.
type ExplorerStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// BackendReady indicates whether the backend is running and healthy.
	// +optional
	BackendReady bool `json:"backendReady,omitempty"`

	// FrontendReady indicates whether the frontend is running and healthy.
	// +optional
	FrontendReady bool `json:"frontendReady,omitempty"`

	// Conditions represent the latest observations of the explorer's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=exp
// +kubebuilder:printcolumn:name="Chain",type=string,JSONPath=`.spec.chainRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Backend",type=boolean,JSONPath=`.status.backendReady`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Explorer is the Schema for the explorers API.
// It deploys a block explorer UI and API for a specific Chain.
type Explorer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ExplorerSpec   `json:"spec,omitempty"`
	Status ExplorerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ExplorerList contains a list of Explorer.
type ExplorerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Explorer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Explorer{}, &ExplorerList{})
}
