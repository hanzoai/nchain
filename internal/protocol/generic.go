package protocol

import (
	"encoding/json"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

const (
	genericDataDir     = "/data"
	genericRPCPort     = int32(8545)
	genericP2PPort     = int32(30303)
	genericMetricsPort = int32(9090)
)

// GenericDriver implements the Driver interface as a passthrough for any
// containerized blockchain node. It relies on the user to specify the
// correct command, args, and configuration via the NodeClusterSpec.
type GenericDriver struct{}

func (d *GenericDriver) Name() string { return "generic" }

func (d *GenericDriver) DefaultImage() string { return "" }

func (d *GenericDriver) DefaultPorts() v1alpha1.PortConfig {
	return v1alpha1.PortConfig{
		RPC:     genericRPCPort,
		P2P:     genericP2PPort,
		Metrics: genericMetricsPort,
	}
}

func (d *GenericDriver) BuildCommand(spec *v1alpha1.NodeClusterSpec) ([]string, []string) {
	// Extract command and args from ProtocolConfig.
	if spec.ProtocolConfig == nil || spec.ProtocolConfig.Raw == nil {
		return nil, nil
	}

	var cfg struct {
		Command []string `json:"command"`
		Args    []string `json:"args"`
	}
	if err := json.Unmarshal(spec.ProtocolConfig.Raw, &cfg); err != nil {
		return nil, nil
	}

	return cfg.Command, cfg.Args
}

func (d *GenericDriver) BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
}

func (d *GenericDriver) BuildVolumeMounts(_ *v1alpha1.NodeClusterSpec) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "data", MountPath: genericDataDir},
	}
}

func (d *GenericDriver) BuildVolumes(_ *v1alpha1.NodeClusterSpec) []corev1.Volume {
	return nil
}

func (d *GenericDriver) HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	rpc := spec.Ports.RPC
	if rpc == 0 {
		rpc = genericRPCPort
	}
	return "/health", rpc
}

func (d *GenericDriver) ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return d.HealthEndpoint(spec)
}

func (d *GenericDriver) BuildInitContainers(_ *v1alpha1.NodeClusterSpec) []corev1.Container {
	return nil
}

func (d *GenericDriver) NeedsConfigMap(_ *v1alpha1.NodeClusterSpec) bool {
	return false
}

func (d *GenericDriver) BuildConfigMap(_ *v1alpha1.NodeClusterSpec) map[string]string {
	return nil
}

func (d *GenericDriver) RecommendedResources(_ string) (corev1.ResourceList, corev1.ResourceList) {
	return corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("1"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		}, corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		}
}

func (d *GenericDriver) RecommendedStorage(_ string) string {
	return "50Gi"
}
