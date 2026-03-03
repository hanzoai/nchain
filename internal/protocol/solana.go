package protocol

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

const (
	solDefaultImage  = "ghcr.io/hanzoai/solana-validator:latest"
	solDataDir       = "/data"
	solLedgerDir     = "/data/ledger"
	solAccountsDir   = "/data/accounts"
	solKeysDir       = "/keys"
	solRPCPort       = int32(8899)
	solP2PPort       = int32(8001)
	solGossipPort    = int32(8000)
	solMetricsPort   = int32(9090)
	solHealthPath    = "/health"
)

// SolanaDriver implements the Driver interface for the Solana blockchain protocol.
// Solana requires bare-metal-like specs. NVMe SSDs are mandatory.
// RAM-disk for accounts DB is recommended for production validators.
type SolanaDriver struct{}

func (d *SolanaDriver) Name() string { return "solana" }

func (d *SolanaDriver) DefaultImage() string { return solDefaultImage }

func (d *SolanaDriver) DefaultPorts() v1alpha1.PortConfig {
	return v1alpha1.PortConfig{
		RPC:     solRPCPort,
		P2P:     solP2PPort,
		Metrics: solMetricsPort,
		Additional: []v1alpha1.NamedPort{
			{Name: "gossip", Port: solGossipPort, Protocol: corev1.ProtocolUDP},
		},
	}
}

func (d *SolanaDriver) BuildCommand(spec *v1alpha1.NodeClusterSpec) ([]string, []string) {
	args := []string{
		"--identity", fmt.Sprintf("%s/validator-keypair.json", solKeysDir),
		"--ledger", solLedgerDir,
		"--accounts", solAccountsDir,
		"--rpc-port", fmt.Sprintf("%d", d.rpcPort(spec)),
		"--dynamic-port-range", "8000-8020",
		"--limit-ledger-size",
		"--no-poh-speed-test",
		"--full-rpc-api",
		"--rpc-bind-address", "0.0.0.0",
	}

	// Bootstrap entrypoints.
	if len(spec.P2P.BootstrapNodes) > 0 {
		for _, node := range spec.P2P.BootstrapNodes {
			args = append(args, "--entrypoint", node)
		}
	}

	// Network-specific expected genesis hash via protocolConfig is left to the user.

	return []string{"agave-validator"}, args
}

func (d *SolanaDriver) BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar {
	return []corev1.EnvVar{
		{Name: "SOLANA_METRICS_CONFIG", Value: ""},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
			},
		},
	}
}

func (d *SolanaDriver) BuildVolumeMounts(spec *v1alpha1.NodeClusterSpec) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "data", MountPath: solDataDir},
	}

	if spec.Keys != nil && spec.Keys.SecretName != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "keys",
			MountPath: solKeysDir,
			ReadOnly:  true,
		})
	}

	return mounts
}

func (d *SolanaDriver) BuildVolumes(spec *v1alpha1.NodeClusterSpec) []corev1.Volume {
	var volumes []corev1.Volume

	if spec.Keys != nil && spec.Keys.SecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.Keys.SecretName,
					DefaultMode: int32Ptr(0400),
				},
			},
		})
	}

	return volumes
}

func (d *SolanaDriver) HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return solHealthPath, d.rpcPort(spec)
}

func (d *SolanaDriver) ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return solHealthPath, d.rpcPort(spec)
}

func (d *SolanaDriver) BuildInitContainers(spec *v1alpha1.NodeClusterSpec) []corev1.Container {
	var initContainers []corev1.Container

	// Create ledger and accounts directories.
	initContainers = append(initContainers, corev1.Container{
		Name:    "init-dirs",
		Image:   "alpine:3",
		Command: []string{"/bin/sh", "-c"},
		Args: []string{strings.Join([]string{
			fmt.Sprintf("mkdir -p %s", solLedgerDir),
			fmt.Sprintf("mkdir -p %s", solAccountsDir),
		}, " && ")},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: solDataDir},
		},
	})

	return initContainers
}

func (d *SolanaDriver) NeedsConfigMap(_ *v1alpha1.NodeClusterSpec) bool {
	return false
}

func (d *SolanaDriver) BuildConfigMap(_ *v1alpha1.NodeClusterSpec) map[string]string {
	return nil
}

func (d *SolanaDriver) RecommendedResources(role string) (corev1.ResourceList, corev1.ResourceList) {
	switch role {
	case "fullnode":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("128Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("24"),
				corev1.ResourceMemory: resource.MustParse("256Gi"),
			}
	case "archive":
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("24"),
				corev1.ResourceMemory: resource.MustParse("256Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("32"),
				corev1.ResourceMemory: resource.MustParse("512Gi"),
			}
	default: // validator
		return corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("24"),
				corev1.ResourceMemory: resource.MustParse("256Gi"),
			}, corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("32"),
				corev1.ResourceMemory: resource.MustParse("512Gi"),
			}
	}
}

func (d *SolanaDriver) RecommendedStorage(role string) string {
	switch role {
	case "archive":
		return "4Ti"
	default:
		return "2Ti"
	}
}

func (d *SolanaDriver) rpcPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.RPC > 0 {
		return spec.Ports.RPC
	}
	return solRPCPort
}
