package protocol

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

const (
	cosmosDefaultImage = "ghcr.io/hanzoai/gaiad:latest"
	cosmosDataDir      = "/data"
	cosmosRPCPort      = int32(26657)
	cosmosP2PPort      = int32(26656)
	cosmosGRPCPort     = int32(9090)
	cosmosAPIPort      = int32(1317)
	cosmosHealthPath   = "/status"
)

// CosmosDriver implements the Driver interface for Cosmos SDK chains (gaiad).
type CosmosDriver struct{}

func (d *CosmosDriver) Name() string { return "cosmos" }

func (d *CosmosDriver) DefaultImage() string { return cosmosDefaultImage }

func (d *CosmosDriver) DefaultPorts() v1alpha1.PortConfig {
	return v1alpha1.PortConfig{
		RPC:     cosmosRPCPort,
		P2P:     cosmosP2PPort,
		Metrics: cosmosGRPCPort,
		Additional: []v1alpha1.NamedPort{
			{Name: "api", Port: cosmosAPIPort, Protocol: corev1.ProtocolTCP},
		},
	}
}

func (d *CosmosDriver) BuildCommand(spec *v1alpha1.NodeClusterSpec) ([]string, []string) {
	args := []string{
		"start",
		fmt.Sprintf("--home=%s", cosmosDataDir),
		fmt.Sprintf("--rpc.laddr=tcp://0.0.0.0:%d", d.rpcPort(spec)),
		fmt.Sprintf("--p2p.laddr=tcp://0.0.0.0:%d", d.p2pPort(spec)),
		fmt.Sprintf("--grpc.address=0.0.0.0:%d", cosmosGRPCPort),
		"--api.enable",
		fmt.Sprintf("--api.address=tcp://0.0.0.0:%d", cosmosAPIPort),
	}

	// Bootstrap peers.
	if len(spec.P2P.BootstrapNodes) > 0 {
		persistent := ""
		for i, node := range spec.P2P.BootstrapNodes {
			if i > 0 {
				persistent += ","
			}
			persistent += node
		}
		args = append(args, fmt.Sprintf("--p2p.persistent_peers=%s", persistent))
	}

	return []string{"gaiad"}, args
}

func (d *CosmosDriver) BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
}

func (d *CosmosDriver) BuildVolumeMounts(_ *v1alpha1.NodeClusterSpec) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "data", MountPath: cosmosDataDir},
	}
}

func (d *CosmosDriver) BuildVolumes(_ *v1alpha1.NodeClusterSpec) []corev1.Volume {
	return nil
}

func (d *CosmosDriver) HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return cosmosHealthPath, d.rpcPort(spec)
}

func (d *CosmosDriver) ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return cosmosHealthPath, d.rpcPort(spec)
}

func (d *CosmosDriver) BuildInitContainers(_ *v1alpha1.NodeClusterSpec) []corev1.Container {
	return nil
}

func (d *CosmosDriver) NeedsConfigMap(_ *v1alpha1.NodeClusterSpec) bool {
	return false
}

func (d *CosmosDriver) BuildConfigMap(_ *v1alpha1.NodeClusterSpec) map[string]string {
	return nil
}

func (d *CosmosDriver) RecommendedResources(role string) (corev1.ResourceList, corev1.ResourceList) {
	switch role {
	case "archive":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("64Gi"),
			}
	case "validator":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("64Gi"),
			}
	default: // fullnode
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("32Gi"),
			}
	}
}

func (d *CosmosDriver) RecommendedStorage(role string) string {
	switch role {
	case "archive":
		return "2Ti"
	default:
		return "500Gi"
	}
}

func (d *CosmosDriver) rpcPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.RPC > 0 {
		return spec.Ports.RPC
	}
	return cosmosRPCPort
}

func (d *CosmosDriver) p2pPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.P2P > 0 {
		return spec.Ports.P2P
	}
	return cosmosP2PPort
}
