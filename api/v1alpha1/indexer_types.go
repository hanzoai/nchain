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

	// Port is the HTTP API port the indexer serves. The canonical
	// ghcr.io/luxfi/indexer image listens on 8091.
	// +kubebuilder:default=8091
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	// +optional
	Port int32 `json:"port,omitempty"`

	// RPCEndpoint overrides the auto-discovered chain JSON-RPC endpoint.
	// When empty it is derived from the referenced NodeCluster headless service.
	// +optional
	RPCEndpoint string `json:"rpcEndpoint,omitempty"`

	// WSEndpoint overrides the auto-discovered chain WebSocket endpoint.
	// When empty it is derived from the referenced NodeCluster headless service.
	// +optional
	WSEndpoint string `json:"wsEndpoint,omitempty"`

	// Replication configures native streaming replication of the embedded
	// SQLite store to object storage for high availability and fast restore.
	// +optional
	Replication IndexerReplicationSpec `json:"replication,omitempty"`

	// SeedRestore restores the embedded store from an object-store snapshot on
	// first boot via a marker-gated init container.
	// +optional
	SeedRestore IndexerSeedRestoreSpec `json:"seedRestore,omitempty"`

	// DefiIndexing enables DeFi protocol indexing (AMM, lending, staking).
	// +optional
	DefiIndexing bool `json:"defiIndexing,omitempty"`

	// NFTIndexing enables NFT indexing.
	// +optional
	NFTIndexing bool `json:"nftIndexing,omitempty"`

	// ContractVerification enables contract source verification.
	// +kubebuilder:default=true
	// +optional
	ContractVerification bool `json:"contractVerification,omitempty"`

	// TraceEnabled enables internal transaction tracing.
	// +kubebuilder:default=true
	// +optional
	TraceEnabled bool `json:"traceEnabled,omitempty"`

	// PollInterval is the block polling interval in seconds.
	// +kubebuilder:default=2
	// +kubebuilder:validation:Minimum=1
	// +optional
	PollInterval int32 `json:"pollInterval,omitempty"`
}

// IndexerReplicationSpec drives native streaming replication of the indexer's
// embedded SQLite store to S3-compatible object storage via REPLICATE_* env.
// Mirrors the luxfi/operator ReplicationSpec model: continuous incrementals plus
// periodic full snapshots, optionally age-encrypted, with restore-on-boot — so a
// fresh pod launches straight from the latest snapshot instead of reindexing.
type IndexerReplicationSpec struct {
	// Enabled toggles replication.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// S3Endpoint is the S3-compatible object-store endpoint.
	// +optional
	S3Endpoint string `json:"s3Endpoint,omitempty"`

	// S3Bucket holds replication objects. Defaults to "replicate".
	// +optional
	S3Bucket string `json:"s3Bucket,omitempty"`

	// S3Region is the object-store region.
	// +optional
	S3Region string `json:"s3Region,omitempty"`

	// S3Path is the key prefix for replication objects.
	// +optional
	S3Path string `json:"s3Path,omitempty"`

	// S3UseSSL enables TLS to the object store.
	// +optional
	S3UseSSL bool `json:"s3UseSsl,omitempty"`

	// CredentialsSecret is a Secret with REPLICATE_S3_ACCESS_KEY / _SECRET_KEY.
	// +optional
	CredentialsSecret string `json:"credentialsSecret,omitempty"`

	// AgeRecipient (age public key) enables client-side snapshot encryption.
	// +optional
	AgeRecipient string `json:"ageRecipient,omitempty"`

	// AgeIdentitySecret is a Secret holding the age identity for restore.
	// +optional
	AgeIdentitySecret string `json:"ageIdentitySecret,omitempty"`

	// SnapshotIntervalSeconds between full snapshots. Defaults to 3600.
	// +optional
	SnapshotIntervalSeconds int64 `json:"snapshotIntervalSeconds,omitempty"`

	// IncrementalIntervalSeconds between incrementals. Defaults to 5.
	// +optional
	IncrementalIntervalSeconds int64 `json:"incrementalIntervalSeconds,omitempty"`
}

// IndexerSeedRestoreSpec restores the indexer's embedded store from an
// object-store snapshot on first boot, gated by a marker file so it runs once
// per volume. Mirrors the luxfi/operator SeedRestoreSpec object-store path.
type IndexerSeedRestoreSpec struct {
	// Enabled toggles seed restoration.
	// +optional
	Enabled bool `json:"enabled,omitempty"`

	// SourceType selects the restore mechanism. Only "ObjectStore" is handled
	// at the init-container layer.
	// +optional
	SourceType string `json:"sourceType,omitempty"`

	// ObjectStoreURL is the snapshot tarball URL when SourceType=ObjectStore.
	// +optional
	ObjectStoreURL string `json:"objectStoreUrl,omitempty"`

	// RestorePolicy controls when to restore: IfNotSeeded (default) or Always.
	// +optional
	RestorePolicy string `json:"restorePolicy,omitempty"`

	// MarkerPath tracks completion. Defaults to /data/.seeded.
	// +optional
	MarkerPath string `json:"markerPath,omitempty"`
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

	// ChainHeight is the latest block height reported by the chain RPC.
	// +optional
	ChainHeight int64 `json:"chainHeight,omitempty"`

	// APIEndpoint is the in-cluster URL of the indexer HTTP API.
	// +optional
	APIEndpoint string `json:"apiEndpoint,omitempty"`

	// DatabaseReady indicates whether the indexer's embedded store is ready.
	// +optional
	DatabaseReady bool `json:"databaseReady,omitempty"`

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
