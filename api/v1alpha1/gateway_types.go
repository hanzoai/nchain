package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GatewaySpec defines the desired state of an RPC/API gateway.
type GatewaySpec struct {
	// Image for the gateway container.
	// Defaults to ghcr.io/hanzoai/gateway:latest (KrakenD-based).
	// +optional
	Image *ImageSpec `json:"image,omitempty"`

	// Replicas is the number of gateway instances.
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Routes defines explicit routing rules.
	// +optional
	Routes []GatewayRoute `json:"routes,omitempty"`

	// AutoRoutes automatically discovers and routes all chains in the referenced NodeCluster.
	// For the lux protocol this queries the cluster's P-chain (platform.getBlockchains)
	// and generates a route per non-primary subnet chain.
	// +optional
	AutoRoutes bool `json:"autoRoutes,omitempty"`

	// NodeClusterRef is the name of the NodeCluster to route traffic to.
	// +kubebuilder:validation:Required
	NodeClusterRef string `json:"nodeClusterRef"`

	// IndexerRefs lists Indexer names to expose under /api/indexer/{ref}.
	// +optional
	IndexerRefs []string `json:"indexerRefs,omitempty"`

	// RateLimits configures rate limiting applied to RPC endpoints and the
	// gateway as a whole (KrakenD qos/ratelimit).
	// +optional
	RateLimits *RateLimitConfig `json:"rateLimits,omitempty"`

	// CORS configures cross-origin resource sharing for all gateway endpoints.
	// When unset, sensible defaults are applied (origins "*", methods GET/POST/OPTIONS).
	// +optional
	CORS *GatewayCORS `json:"cors,omitempty"`

	// Ingress configures external access to the gateway.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// Resources (CPU/memory) for the gateway containers.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`
}

// GatewayStatus defines the observed state of Gateway.
type GatewayStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// ObservedGeneration is the most recent generation observed by the controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// ReadyReplicas is the number of gateway replicas that are ready.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`

	// ActiveRoutes is the number of routes (gateway endpoints) currently configured.
	// +optional
	ActiveRoutes int32 `json:"activeRoutes,omitempty"`

	// ExternalEndpoint is the externally reachable URL of the gateway, derived
	// from the configured ingress host when available.
	// +optional
	ExternalEndpoint string `json:"externalEndpoint,omitempty"`

	// ChainRoutes maps auto-discovered chain names to their blockchain IDs.
	// +optional
	ChainRoutes map[string]string `json:"chainRoutes,omitempty"`

	// Conditions represent the latest observations of the gateway's state.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// GatewayCORS configures cross-origin resource sharing emitted by the gateway.
type GatewayCORS struct {
	// AllowedOrigins lists permitted CORS origins. Defaults to ["*"].
	// +optional
	AllowedOrigins []string `json:"allowedOrigins,omitempty"`

	// AllowedMethods lists permitted HTTP methods. Defaults to ["GET","POST","OPTIONS"].
	// +optional
	AllowedMethods []string `json:"allowedMethods,omitempty"`

	// AllowedHeaders lists permitted request headers. Defaults to ["Content-Type","Authorization"].
	// +optional
	AllowedHeaders []string `json:"allowedHeaders,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=gw
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.nodeClusterRef`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Routes",type=integer,JSONPath=`.status.activeRoutes`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.externalEndpoint`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Gateway is the Schema for the gateways API.
// It deploys an RPC/API gateway with routing, rate limiting, and load balancing
// in front of a NodeCluster.
type Gateway struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GatewaySpec   `json:"spec,omitempty"`
	Status GatewayStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GatewayList contains a list of Gateway.
type GatewayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Gateway `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Gateway{}, &GatewayList{})
}
