package protocol

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

// Driver encapsulates all protocol-specific logic for a blockchain node.
// Each supported blockchain implements this interface to generate the correct
// container commands, environment variables, volumes, health checks, and ports.
type Driver interface {
	// Name returns the protocol identifier (e.g., "lux", "ethereum", "generic").
	Name() string

	// DefaultImage returns the default container image for this protocol.
	DefaultImage() string

	// BuildCommand generates the container command and args for the node.
	BuildCommand(spec *v1alpha1.NodeClusterSpec) (command []string, args []string)

	// BuildEnv generates protocol-specific environment variables.
	BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar

	// BuildVolumeMounts generates protocol-specific volume mounts.
	BuildVolumeMounts(spec *v1alpha1.NodeClusterSpec) []corev1.VolumeMount

	// BuildVolumes generates protocol-specific volumes.
	BuildVolumes(spec *v1alpha1.NodeClusterSpec) []corev1.Volume

	// HealthEndpoint returns the HTTP health check path and port.
	HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (path string, port int32)

	// ReadinessEndpoint returns the HTTP readiness check path and port.
	ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (path string, port int32)

	// DefaultPorts returns the default port configuration.
	DefaultPorts() v1alpha1.PortConfig

	// BuildInitContainers generates protocol-specific init containers.
	BuildInitContainers(spec *v1alpha1.NodeClusterSpec) []corev1.Container

	// NeedsConfigMap returns true if the protocol generates a ConfigMap.
	NeedsConfigMap(spec *v1alpha1.NodeClusterSpec) bool

	// BuildConfigMap generates protocol-specific ConfigMap data.
	BuildConfigMap(spec *v1alpha1.NodeClusterSpec) map[string]string
}
