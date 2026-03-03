package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BridgeSpec defines the desired state of a cross-chain bridge.
type BridgeSpec struct {
	// ServerImage is the bridge server container image.
	// +kubebuilder:validation:Required
	ServerImage ImageSpec `json:"serverImage"`

	// UIImage is the optional bridge UI container image.
	// +optional
	UIImage *ImageSpec `json:"uiImage,omitempty"`

	// SourceChainRef is the name of the source Chain.
	// +kubebuilder:validation:Required
	SourceChainRef string `json:"sourceChainRef"`

	// TargetChainRef is the name of the target Chain.
	// +kubebuilder:validation:Required
	TargetChainRef string `json:"targetChainRef"`

	// MPCRef is the name of an MPC key management resource for bridge signing.
	// +optional
	MPCRef string `json:"mpcRef,omitempty"`

	// Ingress configures external access to the bridge service.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Resources (CPU/memory) for the bridge containers.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// BridgeStatus defines the observed state of Bridge.
type BridgeStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ServerReady indicates whether the bridge server is running and healthy.
	// +optional
	ServerReady bool `json:"serverReady,omitempty"`

	// UIReady indicates whether the bridge UI is running and healthy.
	// +optional
	UIReady bool `json:"uiReady,omitempty"`

	// Conditions represent the latest observations of the bridge's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=br
// +kubebuilder:printcolumn:name="Source",type=string,JSONPath=`.spec.sourceChainRef`
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetChainRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Bridge is the Schema for the bridges API.
// It deploys a cross-chain bridge between two Chains.
type Bridge struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BridgeSpec   `json:"spec,omitempty"`
	Status BridgeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BridgeList contains a list of Bridge.
type BridgeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Bridge `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Bridge{}, &BridgeList{})
}
