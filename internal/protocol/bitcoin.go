package protocol

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

const (
	btcDefaultImage = "ghcr.io/hanzoai/bitcoind:latest"
	btcDataDir      = "/data"
	btcRPCPort      = int32(8332)
	btcP2PPort      = int32(8333)
	btcMetricsPort  = int32(9090)
)

// BitcoinDriver implements the Driver interface for Bitcoin (bitcoind).
type BitcoinDriver struct{}

func (d *BitcoinDriver) Name() string { return "bitcoin" }

func (d *BitcoinDriver) DefaultImage() string { return btcDefaultImage }

func (d *BitcoinDriver) DefaultPorts() v1alpha1.PortConfig {
	return v1alpha1.PortConfig{
		RPC:     btcRPCPort,
		P2P:     btcP2PPort,
		Metrics: btcMetricsPort,
	}
}

func (d *BitcoinDriver) BuildCommand(spec *v1alpha1.NodeClusterSpec) ([]string, []string) {
	args := []string{
		"-server",
		"-rpcallowip=0.0.0.0/0",
		"-rpcbind=0.0.0.0",
		fmt.Sprintf("-rpcport=%d", d.rpcPort(spec)),
		fmt.Sprintf("-port=%d", d.p2pPort(spec)),
		fmt.Sprintf("-datadir=%s", btcDataDir),
		"-printtoconsole",
	}

	// Enable transaction index for fullnode and archive roles.
	if spec.Role == "fullnode" || spec.Role == "archive" {
		args = append(args, "-txindex")
	}

	return []string{"bitcoind"}, args
}

func (d *BitcoinDriver) BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
}

func (d *BitcoinDriver) BuildVolumeMounts(_ *v1alpha1.NodeClusterSpec) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "data", MountPath: btcDataDir},
	}
}

func (d *BitcoinDriver) BuildVolumes(_ *v1alpha1.NodeClusterSpec) []corev1.Volume {
	return nil
}

func (d *BitcoinDriver) HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return "/", d.rpcPort(spec)
}

func (d *BitcoinDriver) ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return "/", d.rpcPort(spec)
}

func (d *BitcoinDriver) BuildInitContainers(_ *v1alpha1.NodeClusterSpec) []corev1.Container {
	return nil
}

func (d *BitcoinDriver) NeedsConfigMap(_ *v1alpha1.NodeClusterSpec) bool {
	return false
}

func (d *BitcoinDriver) BuildConfigMap(_ *v1alpha1.NodeClusterSpec) map[string]string {
	return nil
}

// RecommendedResources returns recommended resource requirements for Bitcoin.
// Bitcoin does not have validators in the traditional sense; use fullnode for all roles.
func (d *BitcoinDriver) RecommendedResources(role string) (corev1.ResourceList, corev1.ResourceList) {
	switch role {
	case "archive":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("8"),
				corev1.ResourceMemory: resource.MustParse("16Gi"),
			}
	default: // fullnode, validator (treated as fullnode)
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			}
	}
}

func (d *BitcoinDriver) RecommendedStorage(_ string) string {
	return "600Gi"
}

func (d *BitcoinDriver) rpcPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.RPC > 0 {
		return spec.Ports.RPC
	}
	return btcRPCPort
}

func (d *BitcoinDriver) p2pPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.P2P > 0 {
		return spec.Ports.P2P
	}
	return btcP2PPort
}
