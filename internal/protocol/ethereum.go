package protocol

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

const (
	ethDefaultImage = "ghcr.io/hanzoai/reth:latest"
	ethDataDir      = "/data"
	ethRPCPort      = int32(8545)
	ethP2PPort      = int32(30303)
	ethMetricsPort  = int32(9090)
	ethAuthRPCPort  = int32(8551)
	ethHealthPath   = "/health"
)

// EthereumDriver implements the Driver interface for Ethereum-compatible chains.
type EthereumDriver struct{}

func (d *EthereumDriver) Name() string { return "ethereum" }

func (d *EthereumDriver) DefaultImage() string { return ethDefaultImage }

func (d *EthereumDriver) DefaultPorts() v1alpha1.PortConfig {
	return v1alpha1.PortConfig{
		RPC:     ethRPCPort,
		P2P:     ethP2PPort,
		Metrics: ethMetricsPort,
		Additional: []v1alpha1.NamedPort{
			{Name: "authrpc", Port: ethAuthRPCPort, Protocol: corev1.ProtocolTCP},
		},
	}
}

func (d *EthereumDriver) BuildCommand(spec *v1alpha1.NodeClusterSpec) ([]string, []string) {
	args := []string{
		"node",
		fmt.Sprintf("--chain=%s", spec.NetworkID),
		fmt.Sprintf("--datadir=%s", ethDataDir),
		"--http",
		"--http.addr=0.0.0.0",
		fmt.Sprintf("--http.port=%d", d.rpcPort(spec)),
		fmt.Sprintf("--authrpc.port=%d", ethAuthRPCPort),
		fmt.Sprintf("--metrics=%d", d.metricsPort(spec)),
	}

	// P2P port.
	args = append(args, fmt.Sprintf("--p2p.port=%d", d.p2pPort(spec)))

	// Bootstrap nodes.
	if len(spec.P2P.BootstrapNodes) > 0 {
		for _, node := range spec.P2P.BootstrapNodes {
			args = append(args, fmt.Sprintf("--bootnodes=%s", node))
		}
	}

	return []string{"reth"}, args
}

func (d *EthereumDriver) BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "CHAIN_ID", Value: spec.NetworkID},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
}

func (d *EthereumDriver) BuildVolumeMounts(spec *v1alpha1.NodeClusterSpec) []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{Name: "data", MountPath: ethDataDir},
	}
}

func (d *EthereumDriver) BuildVolumes(_ *v1alpha1.NodeClusterSpec) []corev1.Volume {
	return nil
}

func (d *EthereumDriver) HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return ethHealthPath, d.rpcPort(spec)
}

func (d *EthereumDriver) ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return ethHealthPath, d.rpcPort(spec)
}

func (d *EthereumDriver) BuildInitContainers(spec *v1alpha1.NodeClusterSpec) []corev1.Container {
	var initContainers []corev1.Container

	if spec.SeedRestore != nil && spec.SeedRestore.Enabled {
		markerPath := spec.SeedRestore.MarkerPath
		if markerPath == "" {
			markerPath = "/data/.restore-complete"
		}
		initContainers = append(initContainers, corev1.Container{
			Name:    "seed-restore",
			Image:   "alpine:3",
			Command: []string{"/bin/sh", "-c"},
			Args: []string{fmt.Sprintf(`set -e
if [ "%s" = "IfEmpty" ] && [ -f "%s" ]; then
  echo "Already restored, skipping"
  exit 0
fi
echo "Restoring data..."
touch "%s"
`, spec.SeedRestore.RestorePolicy, markerPath, markerPath)},
			VolumeMounts: []corev1.VolumeMount{
				{Name: "data", MountPath: ethDataDir},
			},
		})
	}

	return initContainers
}

func (d *EthereumDriver) NeedsConfigMap(_ *v1alpha1.NodeClusterSpec) bool {
	return false
}

func (d *EthereumDriver) BuildConfigMap(_ *v1alpha1.NodeClusterSpec) map[string]string {
	return nil
}

func (d *EthereumDriver) rpcPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.RPC > 0 {
		return spec.Ports.RPC
	}
	return ethRPCPort
}

func (d *EthereumDriver) p2pPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.P2P > 0 {
		return spec.Ports.P2P
	}
	return ethP2PPort
}

func (d *EthereumDriver) metricsPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.Metrics > 0 {
		return spec.Ports.Metrics
	}
	return ethMetricsPort
}
