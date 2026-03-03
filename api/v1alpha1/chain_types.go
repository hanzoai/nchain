package v1alpha1

import (
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ChainSpec defines the desired state of a blockchain/subnet/L2.
type ChainSpec struct {
	// Protocol must match the parent NodeCluster's protocol.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Enum=lux;ethereum;bitcoin;cosmos;substrate;generic
	Protocol string `json:"protocol"`

	// ChainID is the blockchain-specific chain identifier.
	// +kubebuilder:validation:Required
	ChainID string `json:"chainID"`

	// VMID is the virtual machine identifier (e.g., for Lux subnet VMs).
	// +optional
	VMID string `json:"vmID,omitempty"`

	// Genesis is the raw genesis configuration JSON.
	// +optional
	Genesis *apiextensionsv1.JSON `json:"genesis,omitempty"`

	// GenesisConfigMap references an existing ConfigMap containing genesis data.
	// The ConfigMap key should be "genesis.json".
	// +optional
	GenesisConfigMap string `json:"genesisConfigMap,omitempty"`

	// SubnetID associates this chain with a Lux subnet.
	// +optional
	SubnetID string `json:"subnetID,omitempty"`

	// EVMConfig holds EVM-specific chain configuration (gas limits, fees, etc.).
	// +optional
	EVMConfig *apiextensionsv1.JSON `json:"evmConfig,omitempty"`

	// NodeClusterRef is the name of the NodeCluster that runs this chain.
	// +kubebuilder:validation:Required
	NodeClusterRef string `json:"nodeClusterRef"`

	// ProtocolConfig is opaque protocol-specific chain settings.
	// +optional
	ProtocolConfig *apiextensionsv1.JSON `json:"protocolConfig,omitempty"`
}

// ChainStatus defines the observed state of Chain.
type ChainStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// BlockHeight is the latest known block height.
	// +optional
	BlockHeight int64 `json:"blockHeight,omitempty"`

	// Healthy indicates whether the chain is producing blocks normally.
	// +optional
	Healthy bool `json:"healthy,omitempty"`

	// Conditions represent the latest observations of the chain's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=chain
// +kubebuilder:printcolumn:name="Protocol",type=string,JSONPath=`.spec.protocol`
// +kubebuilder:printcolumn:name="ChainID",type=string,JSONPath=`.spec.chainID`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Height",type=integer,JSONPath=`.status.blockHeight`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Chain is the Schema for the chains API.
// It defines a blockchain, subnet, or L2 that runs on a NodeCluster.
type Chain struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ChainSpec   `json:"spec,omitempty"`
	Status ChainStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ChainList contains a list of Chain.
type ChainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Chain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Chain{}, &ChainList{})
}
