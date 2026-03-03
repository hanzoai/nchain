package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BrandConfig defines per-brand configuration for multi-tenant deployments.
type BrandConfig struct {
	// Name is the brand identifier (hanzo, lux, zoo, pars).
	// +kubebuilder:validation:Required
	Name string `json:"name"`

	// Domain is the base domain for this brand (e.g., web3.hanzo.ai).
	// +kubebuilder:validation:Required
	Domain string `json:"domain"`

	// IAM configuration for this brand.
	// +kubebuilder:validation:Required
	IAM IAMConfig `json:"iam"`
}

// IAMConfig defines IAM/OAuth integration.
type IAMConfig struct {
	// URL is the IAM endpoint (e.g., https://hanzo.id).
	// +kubebuilder:validation:Required
	URL string `json:"url"`

	// Org is the IAM organization.
	// +kubebuilder:validation:Required
	Org string `json:"org"`

	// ClientID is the OAuth client ID.
	// +kubebuilder:validation:Required
	ClientID string `json:"clientID"`
}

// CloudAPISpec defines the API server configuration.
type CloudAPISpec struct {
	// Image for the API server.
	// +optional
	Image *ImageSpec `json:"image,omitempty"`

	// Replicas for the API deployment.
	// +kubebuilder:default=3
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources for the API pods.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Autoscaling configuration.
	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`
}

// CloudWebSpec defines the web frontend configuration.
type CloudWebSpec struct {
	// Image for the web frontend.
	// +optional
	Image *ImageSpec `json:"image,omitempty"`

	// Replicas for the web deployment.
	// +kubebuilder:default=2
	// +optional
	Replicas *int32 `json:"replicas,omitempty"`

	// Resources for the web pods.
	// +optional
	Resources *ResourceRequirements `json:"resources,omitempty"`

	// Autoscaling configuration.
	// +optional
	Autoscaling *AutoscalingSpec `json:"autoscaling,omitempty"`
}

// AutoscalingSpec defines HPA configuration.
type AutoscalingSpec struct {
	// Enabled controls whether HPA is deployed.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// MinReplicas is the minimum number of replicas.
	// +kubebuilder:default=2
	// +optional
	MinReplicas *int32 `json:"minReplicas,omitempty"`

	// MaxReplicas is the maximum number of replicas.
	// +kubebuilder:default=20
	// +optional
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// TargetCPU is the target CPU utilization percentage.
	// +kubebuilder:default=70
	// +optional
	TargetCPU *int32 `json:"targetCPU,omitempty"`

	// TargetMemory is the target memory utilization percentage.
	// +optional
	TargetMemory *int32 `json:"targetMemory,omitempty"`
}

// CloudDatabaseSpec defines database connection configuration.
type CloudDatabaseSpec struct {
	// CredentialsSecret is the name of the K8s Secret containing database credentials.
	// Expected keys: database-url, redis-url, datastore-url (optional)
	// +kubebuilder:validation:Required
	CredentialsSecret string `json:"credentialsSecret"`
}

// CloudFeaturesSpec defines which platform features are enabled.
type CloudFeaturesSpec struct {
	// Wallets enables smart wallet management.
	// +kubebuilder:default=true
	// +optional
	Wallets bool `json:"wallets,omitempty"`

	// Bundler enables ERC-4337 account abstraction bundling.
	// +optional
	Bundler bool `json:"bundler,omitempty"`

	// NFTs enables NFT API.
	// +kubebuilder:default=true
	// +optional
	NFTs bool `json:"nfts,omitempty"`

	// Gas enables gas estimation API.
	// +kubebuilder:default=true
	// +optional
	Gas bool `json:"gas,omitempty"`

	// Webhooks enables webhook subscriptions.
	// +kubebuilder:default=true
	// +optional
	Webhooks bool `json:"webhooks,omitempty"`
}

// CloudSpec defines the desired state of Cloud.
type CloudSpec struct {
	// Brands defines the multi-tenant brand configurations.
	// Each brand gets its own Ingress with separate domain and IAM config.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:Required
	Brands []BrandConfig `json:"brands"`

	// API configures the backend API server.
	// +optional
	API CloudAPISpec `json:"api,omitempty"`

	// Web configures the frontend web application.
	// +optional
	Web CloudWebSpec `json:"web,omitempty"`

	// Database configures database connections.
	// +kubebuilder:validation:Required
	Database CloudDatabaseSpec `json:"database"`

	// Features controls which platform features are enabled.
	// +optional
	Features CloudFeaturesSpec `json:"features,omitempty"`

	// Ingress configures external access for all brands.
	// +optional
	Ingress *IngressSpec `json:"ingress,omitempty"`

	// KMSSecrets configures KMS secret syncing.
	// +optional
	KMSSecrets []KMSSecretRef `json:"kmsSecrets,omitempty"`

	// Env provides additional environment variables.
	// +optional
	Env []corev1.EnvVar `json:"env,omitempty"`

	// Labels are additional labels applied to all managed resources.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// Annotations are additional annotations applied to all managed resources.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// ImagePullSecrets for pulling container images.
	// +optional
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`
}

// CloudStatus defines the observed state of Cloud.
type CloudStatus struct {
	// Phase is the current lifecycle phase.
	// +optional
	Phase Phase `json:"phase,omitempty"`

	// APIReadyReplicas is the number of ready API replicas.
	// +optional
	APIReadyReplicas int32 `json:"apiReadyReplicas,omitempty"`

	// WebReadyReplicas is the number of ready web replicas.
	// +optional
	WebReadyReplicas int32 `json:"webReadyReplicas,omitempty"`

	// BrandCount is the number of configured brands.
	// +optional
	BrandCount int32 `json:"brandCount,omitempty"`

	// Conditions represent the latest observations.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the most recent generation observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=cloud
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="API",type=integer,JSONPath=`.status.apiReadyReplicas`
// +kubebuilder:printcolumn:name="Web",type=integer,JSONPath=`.status.webReadyReplicas`
// +kubebuilder:printcolumn:name="Brands",type=integer,JSONPath=`.status.brandCount`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Cloud is the Schema for the clouds API.
// It manages a multi-brand Web3 cloud platform (bootnode) with API + Web UI.
type Cloud struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudSpec   `json:"spec,omitempty"`
	Status CloudStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// CloudList contains a list of Cloud.
type CloudList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cloud `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cloud{}, &CloudList{})
}
