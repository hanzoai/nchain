package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExplorerSpec defines the desired state of a block explorer.
//
// The explorer is a stateless web UI (e.g. ghcr.io/luxfi/explore) that reads
// chain data from one or more Indexers over their graph/indexer HTTP API. It
// owns no database: indexers persist data in SQLite with native streaming
// replication, so the explorer never provisions Postgres/Redis.
type ExplorerSpec struct {
	// BackendImage is the explorer application container image
	// (e.g. ghcr.io/luxfi/explore).
	// +kubebuilder:validation:Required
	BackendImage ImageSpec `json:"backendImage"`

	// FrontendImage is the optional separate UI container image.
	// +optional
	FrontendImage *ImageSpec `json:"frontendImage,omitempty"`

	// ChainRef is the name of the primary Chain this explorer surfaces.
	// +kubebuilder:validation:Required
	ChainRef string `json:"chainRef"`

	// IndexerRefs lists the per-chain indexers this explorer surfaces. Each ref
	// is wired into the explorer as CHAIN_{i}_NAME / CHAIN_{i}_INDEXER, with the
	// default ref driving NETWORK_ID and the explorer's read API host.
	// +optional
	IndexerRefs []ExplorerIndexerRef `json:"indexerRefs,omitempty"`

	// Branding customizes the explorer UI (network name, logo, primary color).
	// +optional
	Branding *ExplorerBranding `json:"branding,omitempty"`

	// Ingress configures external access to the explorer.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Resources (CPU/memory) for the explorer containers.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// ExplorerIndexerRef references a chain's Indexer for display in the explorer.
type ExplorerIndexerRef struct {
	// IndexerName is the name of the Indexer (same namespace) to source data from.
	// +kubebuilder:validation:Required
	IndexerName string `json:"indexerName"`

	// DisplayName is the chain's human-readable name shown in the explorer UI.
	// +kubebuilder:validation:Required
	DisplayName string `json:"displayName"`

	// Color is an optional hex color for the chain in the UI.
	// +optional
	Color string `json:"color,omitempty"`

	// Default marks this chain as the one shown by default. The first ref is
	// used when none is marked default.
	// +optional
	Default bool `json:"default,omitempty"`
}

// ExplorerBranding customizes the explorer UI.
type ExplorerBranding struct {
	// NetworkName is the network name displayed in the explorer UI.
	// +kubebuilder:validation:Required
	NetworkName string `json:"networkName"`

	// LogoURL is an optional URL to the network logo.
	// +optional
	LogoURL string `json:"logoUrl,omitempty"`

	// PrimaryColor is an optional hex primary color for the UI.
	// +optional
	PrimaryColor string `json:"primaryColor,omitempty"`
}

// ExplorerStatus defines the observed state of Explorer.
type ExplorerStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// URL is the public URL of the explorer, derived from the ingress host.
	// +optional
	URL string `json:"url,omitempty"`

	// ReadyReplicas is the number of ready explorer replicas.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

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
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.status.url`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Explorer is the Schema for the explorers API.
// It deploys a block explorer UI for one or more Chains via their Indexers.
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
