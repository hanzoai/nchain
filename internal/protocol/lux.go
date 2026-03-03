package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/hanzoai/nchain/api/v1alpha1"
)

const (
	luxDefaultImage   = "ghcr.io/luxfi/node:latest"
	luxDataDir        = "/data"
	luxStakingDir     = "/staking"
	luxPluginDir      = "/plugins"
	luxConfigDir      = "/configs"
	luxHTTPPort       = int32(9650)
	luxStakingPort    = int32(9651)
	luxMetricsPort    = int32(9090)
	luxBinaryPath     = "/luxd/build/luxd"
	luxHealthPath     = "/ext/health"
	luxInfoPath       = "/ext/info"
	luxStartupScript  = "startup.sh"
)

// LuxDriver implements the Driver interface for the Lux blockchain protocol.
type LuxDriver struct{}

func (d *LuxDriver) Name() string { return "lux" }

func (d *LuxDriver) DefaultImage() string { return luxDefaultImage }

func (d *LuxDriver) DefaultPorts() v1alpha1.PortConfig {
	return v1alpha1.PortConfig{
		RPC:     luxHTTPPort,
		P2P:     luxStakingPort,
		Metrics: luxMetricsPort,
	}
}

func (d *LuxDriver) BuildCommand(spec *v1alpha1.NodeClusterSpec) ([]string, []string) {
	// Use startup script from ConfigMap when available.
	if d.NeedsConfigMap(spec) {
		return []string{"/bin/sh"}, []string{fmt.Sprintf("%s/%s", luxConfigDir, luxStartupScript)}
	}

	args := []string{
		fmt.Sprintf("--network-id=%s", spec.NetworkID),
		fmt.Sprintf("--public-ip-resolution-service=ifcfg.me"),
		fmt.Sprintf("--db-dir=%s/db", luxDataDir),
		fmt.Sprintf("--http-port=%d", d.rpcPort(spec)),
		fmt.Sprintf("--staking-port=%d", d.stakingPort(spec)),
	}

	// HTTP host binding.
	args = append(args, "--http-host=0.0.0.0")

	// Admin API.
	if spec.API.AdminEnabled {
		args = append(args, "--api-admin-enabled=true")
	}

	// Index API.
	if spec.API.IndexEnabled {
		args = append(args, "--index-enabled=true")
	}

	// Metrics.
	if spec.API.MetricsEnabled == nil || *spec.API.MetricsEnabled {
		args = append(args, "--api-metrics-enabled=true")
	}

	// Sybil protection off for private/local networks.
	if isPrivateNetwork(spec.NetworkID) {
		args = append(args, "--sybil-protection-enabled=false")
	}

	// Bootstrap nodes.
	if len(spec.P2P.BootstrapNodes) > 0 {
		args = append(args, fmt.Sprintf("--bootstrap-ips=%s", strings.Join(spec.P2P.BootstrapNodes, ",")))
	}

	// Private IP allowance.
	if spec.P2P.AllowPrivateIPs {
		args = append(args, "--allow-private-ips=true")
	}

	// Consensus parameters from spec.
	args = append(args, d.consensusArgs(spec)...)

	// Staking keys.
	if spec.Keys != nil && spec.Keys.SecretName != "" {
		args = append(args, fmt.Sprintf("--staking-tls-cert-file=%s/staker.crt", luxStakingDir))
		args = append(args, fmt.Sprintf("--staking-tls-key-file=%s/staker.key", luxStakingDir))
		args = append(args, fmt.Sprintf("--staking-signer-key-file=%s/signer.key", luxStakingDir))
	}

	// Chain tracking.
	for _, chain := range spec.Chains {
		args = append(args, fmt.Sprintf("--track-subnets=%s", chain.BlockchainID))
	}

	// Plugin dir.
	if spec.Init != nil && len(spec.Init.Plugins) > 0 {
		args = append(args, fmt.Sprintf("--plugin-dir=%s", luxPluginDir))
	}

	return []string{luxBinaryPath}, args
}

func (d *LuxDriver) BuildEnv(spec *v1alpha1.NodeClusterSpec) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "LUX_NETWORK_ID", Value: spec.NetworkID},
		{Name: "LUX_HTTP_PORT", Value: fmt.Sprintf("%d", d.rpcPort(spec))},
		{Name: "LUX_STAKING_PORT", Value: fmt.Sprintf("%d", d.stakingPort(spec))},
	}

	// Pod name for identity.
	envs = append(envs, corev1.EnvVar{
		Name: "POD_NAME",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
		},
	})

	// Pod IP for public IP configuration.
	envs = append(envs, corev1.EnvVar{
		Name: "POD_IP",
		ValueFrom: &corev1.EnvVarSource{
			FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
		},
	})

	return envs
}

func (d *LuxDriver) BuildVolumeMounts(spec *v1alpha1.NodeClusterSpec) []corev1.VolumeMount {
	mounts := []corev1.VolumeMount{
		{Name: "data", MountPath: luxDataDir},
	}

	// Staking keys volume.
	if spec.Keys != nil && spec.Keys.SecretName != "" {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "staking-keys",
			MountPath: luxStakingDir,
			ReadOnly:  true,
		})
	}

	// Plugin volume.
	if spec.Init != nil && len(spec.Init.Plugins) > 0 {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "plugins",
			MountPath: luxPluginDir,
		})
	}

	// Config volume.
	if d.NeedsConfigMap(spec) {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      "config",
			MountPath: luxConfigDir,
		})
	}

	return mounts
}

func (d *LuxDriver) BuildVolumes(spec *v1alpha1.NodeClusterSpec) []corev1.Volume {
	var volumes []corev1.Volume

	// Staking keys from secret.
	if spec.Keys != nil && spec.Keys.SecretName != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "staking-keys",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  spec.Keys.SecretName,
					DefaultMode: int32Ptr(0400),
				},
			},
		})
	}

	// Plugin emptyDir for init container to populate.
	if spec.Init != nil && len(spec.Init.Plugins) > 0 {
		volumes = append(volumes, corev1.Volume{
			Name: "plugins",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}

	// ConfigMap volume.
	if d.NeedsConfigMap(spec) {
		mode := int32(0755)
		volumes = append(volumes, corev1.Volume{
			Name: "config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: ""},
					DefaultMode:          &mode,
				},
			},
		})
	}

	return volumes
}

func (d *LuxDriver) HealthEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return luxHealthPath, d.rpcPort(spec)
}

func (d *LuxDriver) ReadinessEndpoint(spec *v1alpha1.NodeClusterSpec) (string, int32) {
	return luxInfoPath, d.rpcPort(spec)
}

func (d *LuxDriver) BuildInitContainers(spec *v1alpha1.NodeClusterSpec) []corev1.Container {
	var initContainers []corev1.Container

	// Plugin init container copies VM plugins from init image.
	if spec.Init != nil && len(spec.Init.Plugins) > 0 {
		initContainers = append(initContainers, d.pluginInitContainer(spec))
	}

	// Seed restore init container.
	if spec.SeedRestore != nil && spec.SeedRestore.Enabled {
		initContainers = append(initContainers, d.seedRestoreInitContainer(spec))
	}

	return initContainers
}

func (d *LuxDriver) NeedsConfigMap(spec *v1alpha1.NodeClusterSpec) bool {
	// Generate a ConfigMap if there are chains, consensus params, or protocol config.
	return len(spec.Chains) > 0 || spec.ProtocolConfig != nil
}

func (d *LuxDriver) BuildConfigMap(spec *v1alpha1.NodeClusterSpec) map[string]string {
	data := make(map[string]string)

	// Generate startup script.
	data[luxStartupScript] = d.buildStartupScript(spec)

	// Chain aliases config.
	if len(spec.Chains) > 0 {
		aliases := make(map[string][]string)
		for _, chain := range spec.Chains {
			if chain.Alias != "" {
				aliases[chain.BlockchainID] = []string{chain.Alias}
			}
		}
		if len(aliases) > 0 {
			if b, err := json.Marshal(aliases); err == nil {
				data["chain-aliases.json"] = string(b)
			}
		}
	}

	// EVM chain config from ProtocolConfig.
	if spec.ProtocolConfig != nil && spec.ProtocolConfig.Raw != nil {
		var cfg map[string]json.RawMessage
		if err := json.Unmarshal(spec.ProtocolConfig.Raw, &cfg); err == nil {
			if evmCfg, ok := cfg["evmConfig"]; ok {
				data["evm-config.json"] = string(evmCfg)
			}
			if upgradeCfg, ok := cfg["upgradeConfig"]; ok {
				data["upgrade.json"] = string(upgradeCfg)
			}
		}
	}

	return data
}

// buildStartupScript generates a bash startup script for luxd.
func (d *LuxDriver) buildStartupScript(spec *v1alpha1.NodeClusterSpec) string {
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\nset -e\n\n")

	// Set data directory permissions.
	sb.WriteString(fmt.Sprintf("mkdir -p %s/db\n", luxDataDir))

	// Copy chain aliases if present.
	if len(spec.Chains) > 0 {
		sb.WriteString(fmt.Sprintf("mkdir -p %s/configs\n", luxDataDir))
		sb.WriteString(fmt.Sprintf("cp %s/chain-aliases.json %s/configs/ 2>/dev/null || true\n", luxConfigDir, luxDataDir))
	}

	// Copy EVM config if present.
	if spec.ProtocolConfig != nil {
		sb.WriteString(fmt.Sprintf("cp %s/evm-config.json %s/configs/ 2>/dev/null || true\n", luxConfigDir, luxDataDir))
		sb.WriteString(fmt.Sprintf("cp %s/upgrade.json %s/configs/ 2>/dev/null || true\n", luxConfigDir, luxDataDir))
	}

	// Build the command.
	sb.WriteString(fmt.Sprintf("\nexec %s \\\n", luxBinaryPath))
	sb.WriteString(fmt.Sprintf("  --network-id=%s \\\n", spec.NetworkID))
	sb.WriteString(fmt.Sprintf("  --public-ip-resolution-service=ifcfg.me \\\n"))
	sb.WriteString(fmt.Sprintf("  --db-dir=%s/db \\\n", luxDataDir))
	sb.WriteString(fmt.Sprintf("  --http-port=%d \\\n", d.rpcPort(spec)))
	sb.WriteString(fmt.Sprintf("  --staking-port=%d \\\n", d.stakingPort(spec)))
	sb.WriteString("  --http-host=0.0.0.0 \\\n")

	if spec.API.AdminEnabled {
		sb.WriteString("  --api-admin-enabled=true \\\n")
	}
	if spec.API.IndexEnabled {
		sb.WriteString("  --index-enabled=true \\\n")
	}
	if spec.API.MetricsEnabled == nil || *spec.API.MetricsEnabled {
		sb.WriteString("  --api-metrics-enabled=true \\\n")
	}
	if isPrivateNetwork(spec.NetworkID) {
		sb.WriteString("  --sybil-protection-enabled=false \\\n")
	}
	if len(spec.P2P.BootstrapNodes) > 0 {
		sb.WriteString(fmt.Sprintf("  --bootstrap-ips=%s \\\n", strings.Join(spec.P2P.BootstrapNodes, ",")))
	}
	if spec.P2P.AllowPrivateIPs {
		sb.WriteString("  --allow-private-ips=true \\\n")
	}

	for _, arg := range d.consensusArgs(spec) {
		sb.WriteString(fmt.Sprintf("  %s \\\n", arg))
	}

	if spec.Keys != nil && spec.Keys.SecretName != "" {
		sb.WriteString(fmt.Sprintf("  --staking-tls-cert-file=%s/staker.crt \\\n", luxStakingDir))
		sb.WriteString(fmt.Sprintf("  --staking-tls-key-file=%s/staker.key \\\n", luxStakingDir))
		sb.WriteString(fmt.Sprintf("  --staking-signer-key-file=%s/signer.key \\\n", luxStakingDir))
	}

	for _, chain := range spec.Chains {
		sb.WriteString(fmt.Sprintf("  --track-subnets=%s \\\n", chain.BlockchainID))
	}

	if spec.Init != nil && len(spec.Init.Plugins) > 0 {
		sb.WriteString(fmt.Sprintf("  --plugin-dir=%s \\\n", luxPluginDir))
	}

	// Terminate the last line.
	content := sb.String()
	content = strings.TrimRight(content, " \\\n") + "\n"

	return content
}

// consensusArgs builds consensus-specific flags from the ConsensusConfig.
func (d *LuxDriver) consensusArgs(spec *v1alpha1.NodeClusterSpec) []string {
	if spec.Consensus == nil {
		return nil
	}

	var args []string

	// Map well-known consensus parameters to luxd flags.
	for key, val := range spec.Consensus.Params {
		var value string
		if err := json.Unmarshal(val.Raw, &value); err != nil {
			// Try as raw value.
			value = strings.Trim(string(val.Raw), "\"")
		}

		switch key {
		case "snowSampleSize":
			args = append(args, fmt.Sprintf("--snow-sample-size=%s", value))
		case "snowQuorumSize":
			args = append(args, fmt.Sprintf("--snow-quorum-size=%s", value))
		case "snowVirtuousCommitThreshold":
			args = append(args, fmt.Sprintf("--snow-virtuous-commit-threshold=%s", value))
		case "snowRogueCommitThreshold":
			args = append(args, fmt.Sprintf("--snow-rogue-commit-threshold=%s", value))
		case "snowConcurrentRepolls":
			args = append(args, fmt.Sprintf("--snow-concurrent-repolls=%s", value))
		default:
			// Pass through as --key=value for unknown params.
			args = append(args, fmt.Sprintf("--%s=%s", key, value))
		}
	}

	return args
}

func (d *LuxDriver) pluginInitContainer(spec *v1alpha1.NodeClusterSpec) corev1.Container {
	// Build copy commands for each plugin.
	var cmds []string
	for _, plugin := range spec.Init.Plugins {
		dest := plugin.DestPath
		if dest == "" {
			dest = luxPluginDir
		}
		cmds = append(cmds, fmt.Sprintf("cp -v /plugins/%s %s/", plugin.Name, dest))
	}

	return corev1.Container{
		Name:    "init-plugins",
		Image:   fmt.Sprintf("%s:%s", spec.Init.Image.Repository, imageTag(spec.Init.Image.Tag)),
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{strings.Join(cmds, " && ")},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "plugins", MountPath: luxPluginDir},
		},
	}
}

func (d *LuxDriver) seedRestoreInitContainer(spec *v1alpha1.NodeClusterSpec) corev1.Container {
	restore := spec.SeedRestore
	markerPath := restore.MarkerPath
	if markerPath == "" {
		markerPath = "/data/.restore-complete"
	}

	script := fmt.Sprintf(`#!/bin/sh
set -e
if [ "%s" = "Never" ]; then
  echo "Restore policy is Never, skipping"
  exit 0
fi
if [ "%s" = "IfEmpty" ] && [ -f "%s" ]; then
  echo "Marker exists, skipping restore"
  exit 0
fi
echo "Restoring data from %s source: %s"
`,
		restore.RestorePolicy,
		restore.RestorePolicy,
		markerPath,
		restore.SourceType,
		restore.ObjectStoreURL,
	)

	switch restore.SourceType {
	case "ObjectStore":
		script += fmt.Sprintf(`
wget -qO- "%s" | tar xzf - -C %s
touch "%s"
echo "Restore complete"
`, restore.ObjectStoreURL, luxDataDir, markerPath)
	default:
		script += fmt.Sprintf("touch \"%s\"\necho \"Restore complete\"\n", markerPath)
	}

	return corev1.Container{
		Name:    "seed-restore",
		Image:   "alpine:3",
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "data", MountPath: luxDataDir},
		},
	}
}

func (d *LuxDriver) rpcPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.RPC > 0 {
		return spec.Ports.RPC
	}
	return luxHTTPPort
}

func (d *LuxDriver) stakingPort(spec *v1alpha1.NodeClusterSpec) int32 {
	if spec.Ports.P2P > 0 {
		return spec.Ports.P2P
	}
	return luxStakingPort
}

// isPrivateNetwork returns true for custom/private network IDs.
// Well-known Lux networks are 1 (mainnet), 5 (fuji testnet).
func isPrivateNetwork(networkID string) bool {
	return networkID != "1" && networkID != "5"
}

func imageTag(tag string) string {
	if tag == "" {
		return "latest"
	}
	return tag
}

func int32Ptr(v int32) *int32 { return &v }
