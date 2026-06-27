package controller

// This file ports the production-validator behaviors from the mature Rust
// operator (lux-operator) and the Go reference operator (luxfi/operator) into
// nchain, matching nchain's existing patterns:
//
//   - replicationEnv: the node's NATIVE streaming replication (REPLICATE_* env).
//     Full snapshots + continuous incrementals streamed to an object store with
//     restore-on-boot — the preferred ongoing snapshot mechanism (Go->Go reuse).
//   - buildSnapshotCronJob: operator-driven periodic snapshot (admin.snapshot ->
//     tar -> object store) for backup/DR and as a seed-restore source.
//   - buildPerPodService + discoverExternalIPs: per-pod LoadBalancer Services so
//     each validator has a stable external IP for P2P identity, discovered back
//     into status.
//   - buildStartupGateInitContainer: wait for a peer quorum before the node
//     starts, avoiding the "bootstrapped at height 0 with 0 peers" race.
//   - buildRLPImportInitContainer: one-time C-chain RLP import (admin_importChain)
//     for fast bootstrap.
//   - pollNode / pollHealth: per-node /ext/health polling -> per-node and
//     aggregate status (pChainHeight, peers, bootstrapped, healthy, heightSkew).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/hanzoai/nchain/api/v1alpha1"
	"github.com/hanzoai/nchain/internal/manifests"
)

// luxNodeBinary is the luxd path inside the node image. It mirrors the constant
// used by the lux protocol driver; the RLP-import init container starts a
// temporary node from it to run admin_importChain.
const luxNodeBinary = "/luxd/build/luxd"

// imageRef renders an ImageSpec into a "repo:tag" reference (tag defaults to latest).
func imageRef(img v1alpha1.ImageSpec) string {
	tag := img.Tag
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s:%s", img.Repository, tag)
}

// replicationEnv translates a ReplicationSpec into the REPLICATE_* environment
// the node's Replicator reads. When set, the node streams full snapshots plus
// incrementals to an object store on its own and restores from the latest
// snapshot on boot — no operator-side scan, no RLP export. Access/secret keys
// come from EnvFrom on CredentialsSecret. POD_NAME is intentionally omitted: the
// lux driver already injects it, so we avoid a duplicate env entry.
func replicationEnv(spec *v1alpha1.ReplicationSpec) ([]corev1.EnvVar, []corev1.EnvFromSource) {
	if spec == nil || !spec.Enabled {
		return nil, nil
	}
	env := []corev1.EnvVar{
		{Name: "REPLICATE_S3_ENDPOINT", Value: spec.S3Endpoint},
		{Name: "REPLICATE_S3_USE_SSL", Value: strconv.FormatBool(spec.S3UseSSL)},
		// Native pipeline: change-feed incrementals (no keyspace scan), physical
		// SST-copy snapshots, and restore-on-boot so a fresh pod launches straight
		// from the latest snapshot.
		{Name: "REPLICATE_CDC", Value: "true"},
		{Name: "REPLICATE_PHYSICAL_SNAPSHOT", Value: "true"},
		{Name: "REPLICATE_AUTO_ON_OPEN", Value: "true"},
		{Name: "REPLICATE_RESTORE_ON_BOOT", Value: "true"},
		{Name: "REPLICATE_SNAPSHOT_MIN_INCREMENTS", Value: "64"},
	}
	if spec.S3Bucket != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_S3_BUCKET", Value: spec.S3Bucket})
	}
	if spec.S3Region != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_S3_REGION", Value: spec.S3Region})
	}
	if spec.S3Path != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_S3_PATH", Value: spec.S3Path})
	}
	if spec.AgeRecipient != "" {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_AGE_RECIPIENT", Value: spec.AgeRecipient})
	}
	snap := spec.SnapshotIntervalSeconds
	if snap <= 0 {
		snap = 3600
	}
	inc := spec.IncrementalIntervalSeconds
	if inc <= 0 {
		inc = 5
	}
	env = append(env,
		corev1.EnvVar{Name: "REPLICATE_SNAPSHOT_INTERVAL_SECONDS", Value: strconv.FormatInt(snap, 10)},
		corev1.EnvVar{Name: "REPLICATE_INCREMENTAL_INTERVAL_SECONDS", Value: strconv.FormatInt(inc, 10)},
	)
	// Single-writer election: only the source ordinal runs the backup loop; every
	// node still restores on boot. Omitted (every node a writer) when unset.
	if spec.SourceNodeIndex != nil {
		env = append(env, corev1.EnvVar{Name: "REPLICATE_SOURCE_ORDINAL", Value: strconv.FormatInt(int64(*spec.SourceNodeIndex), 10)})
	}
	var from []corev1.EnvFromSource
	if spec.CredentialsSecret != "" {
		from = append(from, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: spec.CredentialsSecret},
		}})
	}
	// AgeIdentitySecret holds REPLICATE_AGE_IDENTITY — the private key used to
	// decrypt snapshots during restore-on-boot. Kept separate from S3 creds.
	if spec.AgeIdentitySecret != "" {
		from = append(from, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: spec.AgeIdentitySecret},
		}})
	}
	return env, from
}

// buildPerPodService creates a per-pod LoadBalancer Service that selects exactly
// one StatefulSet pod, giving that validator a stable external IP for P2P
// identity. Ports are L4 (TCP) for raw RPC/P2P traffic — not HTTP ingress.
func buildPerPodService(cluster *v1alpha1.NodeCluster, index, rpcPort, p2pPort int32) *corev1.Service {
	name := fmt.Sprintf("%s-%d", cluster.Name, index)
	labels := manifests.StandardLabels(cluster.Name, "node", cluster.Name, "")
	labels["nchain.hanzo.ai/pod-index"] = strconv.Itoa(int(index))

	selector := manifests.SelectorLabels(cluster.Name)
	selector["statefulset.kubernetes.io/pod-name"] = name

	annotations := map[string]string{}
	if cluster.Spec.PerPodService != nil {
		for k, v := range cluster.Spec.PerPodService.Annotations {
			annotations[k] = v
		}
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   cluster.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeLoadBalancer,
			Selector: selector,
			Ports: []corev1.ServicePort{
				{Name: "rpc", Port: rpcPort, TargetPort: intstr.FromString("rpc"), Protocol: corev1.ProtocolTCP},
				{Name: "p2p", Port: p2pPort, TargetPort: intstr.FromString("p2p"), Protocol: corev1.ProtocolTCP},
			},
		},
	}
}

// buildStartupGateInitContainer makes a node wait for a quorum of healthy peers
// before the main container starts, avoiding the "bootstrapped at height 0 with
// 0 peers" race right after a restore. It polls every ordinal's health endpoint
// over the headless Service; on timeout it either fails the pod (default —
// kubelet restarts it) or starts anyway. Returns nil when disabled.
func buildStartupGateInitContainer(cluster *v1alpha1.NodeCluster, image, healthPath string, healthPort, replicas int32) *corev1.Container {
	spec := cluster.Spec.StartupGate
	if spec == nil || !spec.Enabled {
		return nil
	}
	minPeers := spec.MinPeers
	if minPeers <= 0 {
		minPeers = 1
	}
	timeout := spec.TimeoutSeconds
	if timeout <= 0 {
		timeout = 300
	}
	interval := spec.CheckIntervalSeconds
	if interval <= 0 {
		interval = 10
	}
	onTimeout := "Fail"
	if spec.OnTimeout == v1alpha1.TimeoutActionStartAnyway {
		onTimeout = "StartAnyway"
	}

	var targets []string
	for i := int32(0); i < replicas; i++ {
		targets = append(targets, fmt.Sprintf("%s-%d.%s-headless:%d", cluster.Name, i, cluster.Name, healthPort))
	}

	script := fmt.Sprintf(`set -u
MIN=%d
TIMEOUT=%d
INTERVAL=%d
ONTIMEOUT=%q
HEALTHPATH=%q
TARGETS=%q
elapsed=0
while [ "$elapsed" -lt "$TIMEOUT" ]; do
  ok=0
  for host in $TARGETS; do
    if curl -fsS -m 2 "http://$host$HEALTHPATH" >/dev/null 2>&1; then
      ok=$((ok+1))
    fi
  done
  echo "startup-gate: $ok/$MIN healthy peers (${elapsed}s elapsed)"
  if [ "$ok" -ge "$MIN" ]; then
    echo "startup-gate: quorum reached, starting"
    exit 0
  fi
  sleep "$INTERVAL"
  elapsed=$((elapsed+INTERVAL))
done
if [ "$ONTIMEOUT" = "StartAnyway" ]; then
  echo "startup-gate: timeout after ${TIMEOUT}s, starting anyway"
  exit 0
fi
echo "startup-gate: timeout after ${TIMEOUT}s waiting for $MIN peers" >&2
exit 1
`, minPeers, timeout, interval, onTimeout, healthPath, strings.Join(targets, " "))

	return &corev1.Container{
		Name:    "startup-gate",
		Image:   image,
		Command: []string{"/bin/sh", "-c", script},
	}
}

// buildRLPImportInitContainer materializes the one-time C-chain RLP import used
// for fast bootstrap: it downloads (optionally multi-part) and assembles the RLP
// file, clears stale C-chain data, starts a temporary local node, waits for it
// to become healthy, and imports the RLP via admin_importChain. A marker file
// makes it run once per volume, and it no-ops when the node was seeded from a
// snapshot. Returns nil when disabled. Requires a "data" volume mounted at /data.
func buildRLPImportInitContainer(cluster *v1alpha1.NodeCluster, image string, rpcPort, p2pPort int32) *corev1.Container {
	spec := cluster.Spec.RLPImport
	if spec == nil || !spec.Enabled || spec.Filename == "" || spec.BaseURL == "" {
		return nil
	}
	minHeight := spec.MinHeight
	if minHeight <= 0 {
		minHeight = 1
	}
	timeout := spec.TimeoutSeconds
	if timeout <= 0 {
		timeout = 7200
	}

	// Download/assembly step.
	var dl strings.Builder
	if spec.MultiPart && len(spec.Parts) > 0 {
		base := strings.TrimSuffix(spec.Filename, ".rlp")
		dl.WriteString("  echo \"rlp-import: downloading parts\"\n")
		for _, p := range spec.Parts {
			dl.WriteString(fmt.Sprintf("  curl -fL --retry 5 --retry-delay 2 --retry-connrefused -o \"$BOOT/%s.part.%s\" \"%s/%s.part.%s\" || true\n",
				spec.Filename, p, spec.BaseURL, base, p))
		}
		dl.WriteString(fmt.Sprintf("  cat \"$BOOT/%s\".part.* > \"$RLP\" 2>/dev/null || true\n", spec.Filename))
	} else {
		dl.WriteString(fmt.Sprintf("  curl -fL --retry 5 --retry-delay 2 --retry-connrefused -o \"$RLP\" \"%s/%s\" || true\n",
			spec.BaseURL, spec.Filename))
	}

	// Clear stale C-chain data to avoid trie corruption (best-effort).
	clearCmd := "true\n"
	if spec.CChainID != "" {
		clearCmd = fmt.Sprintf("rm -rf \"/data/db/%s\" \"/data/chainData/%s\" 2>/dev/null || true\n", spec.CChainID, spec.CChainID)
	}

	script := fmt.Sprintf(`set -e
MARKER=/data/.rlp-imported
BOOT=/data/bootstrap
RLP="$BOOT/%[1]s"
PORT=%[2]d
if [ -f "$MARKER" ]; then echo "rlp-import: already imported"; exit 0; fi
if [ -f /data/.restore-complete ] || [ -f /data/.seeded ]; then
  echo "rlp-import: node seeded from snapshot, skipping"; touch "$MARKER"; exit 0
fi
mkdir -p "$BOOT" /data/db
if [ ! -s "$RLP" ]; then
%[3]s
fi
if [ ! -s "$RLP" ]; then echo "rlp-import: RLP file missing, skipping"; exit 0; fi
%[4]s
echo "rlp-import: starting temporary node for import"
%[5]s \
  --network-id=%[6]s \
  --db-dir=/data/db \
  --http-host=127.0.0.1 \
  --http-port=$PORT \
  --staking-port=%[7]d \
  --sybil-protection-enabled=false &
NODE_PID=$!
i=0
until curl -sf -m 2 "http://127.0.0.1:$PORT/ext/health" >/dev/null 2>&1; do
  i=$((i+1))
  if [ "$i" -gt 150 ]; then echo "rlp-import: node did not become healthy" >&2; kill "$NODE_PID" 2>/dev/null || true; exit 1; fi
  sleep 2
done
HEX=$(curl -fsS -m 5 -X POST -H 'content-type:application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}' \
  "http://127.0.0.1:$PORT/ext/bc/C/rpc" | sed -n 's/.*"result":"\(0x[0-9a-fA-F]*\)".*/\1/p')
HEIGHT=$(printf '%%d' "$HEX" 2>/dev/null || echo 0)
if [ "${HEIGHT:-0}" -ge %[8]d ]; then
  echo "rlp-import: C-chain at $HEIGHT >= %[8]d, skipping import"
else
  echo "rlp-import: importing $RLP (timeout %[9]ds)"
  curl -fsS -m %[9]d -X POST -H 'content-type:application/json' \
    --data "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"admin_importChain\",\"params\":[\"$RLP\"]}" \
    "http://127.0.0.1:$PORT/ext/bc/C/rpc" || echo "rlp-import: import RPC returned error"
fi
kill "$NODE_PID" 2>/dev/null || true
wait "$NODE_PID" 2>/dev/null || true
touch "$MARKER"
echo "rlp-import: complete"
`,
		spec.Filename,                        // 1
		rpcPort,                              // 2
		strings.TrimRight(dl.String(), "\n"), // 3
		strings.TrimRight(clearCmd, "\n"),    // 4
		luxNodeBinary,                        // 5
		cluster.Spec.NetworkID,               // 6
		p2pPort,                              // 7
		minHeight,                            // 8
		timeout,                              // 9
	)

	return &corev1.Container{
		Name:         "rlp-import",
		Image:        image,
		Command:      []string{"/bin/sh", "-c", script},
		VolumeMounts: []corev1.VolumeMount{{Name: "data", MountPath: "/data"}},
	}
}

// buildSnapshotCronJob builds the operator-driven periodic-snapshot CronJob: it
// asks the source node to produce a snapshot (admin.snapshot), streams it, and
// uploads it to the object store via mc. This is the backup/DR + seed-restore
// source path; the ongoing low-latency path is the node's own replication (see
// replicationEnv). Returns nil when disabled.
func buildSnapshotCronJob(cluster *v1alpha1.NodeCluster, image string, rpcPort int32) *batchv1.CronJob {
	spec := cluster.Spec.SnapshotSchedule
	if spec == nil || !spec.Enabled || spec.Schedule == "" {
		return nil
	}
	bucket := spec.S3Bucket
	if bucket == "" {
		bucket = "nchain-snapshots"
	}
	prefix := spec.Prefix
	if prefix == "" {
		prefix = cluster.Name
	}
	retain := spec.RetentionCount
	if retain <= 0 {
		retain = 5
	}
	sourceHost := fmt.Sprintf("%s-%d.%s-headless:%d", cluster.Name, spec.SourceNodeIndex, cluster.Name, rpcPort)
	labels := manifests.StandardLabels(cluster.Name+"-snapshot", "snapshot", cluster.Name, "")

	env := []corev1.EnvVar{
		{Name: "S3_ENDPOINT", Value: spec.S3Endpoint},
		{Name: "S3_BUCKET", Value: bucket},
		{Name: "S3_PREFIX", Value: prefix},
		{Name: "SOURCE_HOST", Value: sourceHost},
		{Name: "MAX_RETAINED", Value: strconv.Itoa(int(retain))},
	}
	var envFrom []corev1.EnvFromSource
	if spec.S3CredentialsSecret != "" {
		envFrom = append(envFrom, corev1.EnvFromSource{SecretRef: &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: spec.S3CredentialsSecret},
		}})
	}

	script := `set -eu
TS=$(date -u +%Y%m%dT%H%M%SZ)
KEY="${S3_PREFIX}/snap-${TS}.tar.zst"
echo "snapshot: requesting admin.snapshot from ${SOURCE_HOST}"
curl -fsS -m 60 -X POST --data '{"jsonrpc":"2.0","id":1,"method":"admin.snapshot","params":{}}' \
  -H 'content-type:application/json' "http://${SOURCE_HOST}/ext/admin" >/dev/null
mc alias set store "${S3_ENDPOINT}" "${S3_ACCESS_KEY:-}" "${S3_SECRET_KEY:-}" >/dev/null 2>&1 || true
curl -fsS -m 600 "http://${SOURCE_HOST}/ext/admin/snapshot.tar.zst" | mc pipe "store/${S3_BUCKET}/${KEY}"
echo "snapshot: uploaded ${KEY}"
mc ls "store/${S3_BUCKET}/${S3_PREFIX}/" | awk '{print $NF}' | sort | head -n -"${MAX_RETAINED}" \
  | while read -r old; do mc rm "store/${S3_BUCKET}/${S3_PREFIX}/${old}" || true; done
echo "snapshot: retained latest ${MAX_RETAINED}"
`

	backoff := int32(2)
	successLimit := int32(3)
	failedLimit := int32(3)

	return &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cluster.Name + "-snapshot",
			Namespace: cluster.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   spec.Schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &successLimit,
			FailedJobsHistoryLimit:     &failedLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: batchv1.JobSpec{
					BackoffLimit: &backoff,
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{Labels: labels},
						Spec: corev1.PodSpec{
							RestartPolicy: corev1.RestartPolicyOnFailure,
							Containers: []corev1.Container{{
								Name:    "snapshot",
								Image:   image,
								Command: []string{"/bin/sh", "-c", script},
								Env:     env,
								EnvFrom: envFrom,
							}},
						},
					},
				},
			},
		},
	}
}

// ───────────────────────── Health polling ─────────────────────────

// nodeRPC issues a single JSON-RPC 2.0 POST and returns the raw "result".
func nodeRPC(ctx context.Context, hc *http.Client, url, method string, params any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rpc %s: status %d", method, resp.StatusCode)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, err
	}
	return env.Result, nil
}

// pollNode queries a single node's health and info APIs and returns its status.
// All calls are best-effort: a failed call leaves the corresponding field at its
// zero value (and an unreachable node is simply reported unhealthy).
func pollNode(ctx context.Context, hc *http.Client, podIP string, rpcPort int32) v1alpha1.NodeStatus {
	base := fmt.Sprintf("http://%s:%d", podIP, rpcPort)
	var ns v1alpha1.NodeStatus

	// Health: a node is bootstrapped when checks.bootstrapped.message is an empty
	// array; healthy when bootstrapped with peers (falls back to the overall flag).
	if res, err := nodeRPC(ctx, hc, base+"/ext/health", "health.health", map[string]any{}); err == nil {
		var h struct {
			Healthy *bool `json:"healthy"`
			Checks  map[string]struct {
				Message json.RawMessage `json:"message"`
			} `json:"checks"`
		}
		if json.Unmarshal(res, &h) == nil {
			ns.Bootstrapped = isEmptyJSONArray(h.Checks["bootstrapped"].Message)
			var nm struct {
				ConnectedPeers int32 `json:"connectedPeers"`
			}
			_ = json.Unmarshal(h.Checks["network"].Message, &nm)
			ns.ConnectedPeers = nm.ConnectedPeers
			ns.Healthy = ns.Bootstrapped && nm.ConnectedPeers > 0
			if !ns.Healthy && h.Healthy != nil && *h.Healthy {
				ns.Healthy = true
			}
		}
	}

	// P-chain height (platform.getHeight returns a decimal string).
	if res, err := nodeRPC(ctx, hc, base+"/ext/bc/P", "platform.getHeight", map[string]any{}); err == nil {
		var p struct {
			Height string `json:"height"`
		}
		if json.Unmarshal(res, &p) == nil {
			if h, err := strconv.ParseInt(p.Height, 10, 64); err == nil {
				ns.PChainHeight = h
			}
		}
	}

	// C-chain height (eth_blockNumber returns a hex string).
	if res, err := nodeRPC(ctx, hc, base+"/ext/bc/C/rpc", "eth_blockNumber", []any{}); err == nil {
		var hexStr string
		if json.Unmarshal(res, &hexStr) == nil {
			if h, err := strconv.ParseInt(strings.TrimPrefix(hexStr, "0x"), 16, 64); err == nil {
				ns.CChainHeight = h
			}
		}
	}

	// Node ID.
	if res, err := nodeRPC(ctx, hc, base+"/ext/info", "info.getNodeID", map[string]any{}); err == nil {
		var n struct {
			NodeID string `json:"nodeID"`
		}
		if json.Unmarshal(res, &n) == nil {
			ns.NodeID = n.NodeID
		}
	}

	// Peer count (info.peers overrides the health-derived count when present).
	if res, err := nodeRPC(ctx, hc, base+"/ext/info", "info.peers", map[string]any{}); err == nil {
		var pr struct {
			NumPeers string            `json:"numPeers"`
			Peers    []json.RawMessage `json:"peers"`
		}
		if json.Unmarshal(res, &pr) == nil {
			if len(pr.Peers) > 0 {
				ns.ConnectedPeers = int32(len(pr.Peers))
			} else if pr.NumPeers != "" {
				if n, err := strconv.ParseInt(pr.NumPeers, 10, 32); err == nil {
					ns.ConnectedPeers = int32(n)
				}
			}
		}
	}

	return ns
}

// isEmptyJSONArray reports whether raw is a JSON array with no elements.
func isEmptyJSONArray(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return false
	}
	return len(arr) == 0
}

// pollHealth polls every node's health/info APIs in parallel and writes per-node
// and aggregate status (healthy count, total peers, P-chain height skew, external
// IPs, degraded reasons). It returns false when there are no pods to poll, so the
// caller can fall back to StatefulSet-readiness-based status.
func (r *NodeClusterReconciler) pollHealth(ctx context.Context, cluster *v1alpha1.NodeCluster, rpcPort, replicas int32) bool {
	var pods corev1.PodList
	if err := r.List(ctx, &pods,
		client.InNamespace(cluster.Namespace),
		client.MatchingLabels(manifests.SelectorLabels(cluster.Name)),
	); err != nil {
		return false
	}
	if len(pods.Items) == 0 {
		return false
	}

	extIPs := r.discoverExternalIPs(ctx, cluster, replicas)
	hc := &http.Client{Timeout: 3 * time.Second}

	results := make([]v1alpha1.NodeStatus, len(pods.Items))
	var wg sync.WaitGroup
	for i := range pods.Items {
		pod := &pods.Items[i]
		results[i] = v1alpha1.NodeStatus{Name: pod.Name, ExternalIP: extIPs[pod.Name]}
		ip := pod.Status.PodIP
		if ip == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, ip string) {
			defer wg.Done()
			s := pollNode(ctx, hc, ip, rpcPort)
			s.Name = results[idx].Name
			s.ExternalIP = results[idx].ExternalIP
			results[idx] = s
		}(i, ip)
	}
	wg.Wait()
	sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })

	var healthy, totalPeers int32
	var heights []int64
	var degraded []string
	for _, n := range results {
		if n.Healthy {
			healthy++
		} else {
			degraded = append(degraded, fmt.Sprintf("NodeUnhealthy: %s failed health check", n.Name))
		}
		totalPeers += n.ConnectedPeers
		if n.PChainHeight > 0 {
			heights = append(heights, n.PChainHeight)
		}
	}

	var skew int64
	if len(heights) > 1 {
		min, max := heights[0], heights[0]
		for _, h := range heights {
			if h < min {
				min = h
			}
			if h > max {
				max = h
			}
		}
		skew = max - min
		maxSkew := cluster.Spec.HealthPolicy.MaxHeightSkew
		if maxSkew <= 0 {
			maxSkew = 100
		}
		if skew > maxSkew {
			degraded = append(degraded, fmt.Sprintf("HeightSkew: P-chain height skew %d exceeds max %d", skew, maxSkew))
		}
	}

	cluster.Status.Nodes = results
	cluster.Status.HealthyReplicas = healthy
	cluster.Status.TotalPeers = totalPeers
	cluster.Status.HeightSkew = skew
	cluster.Status.DegradedReasons = degraded
	if len(extIPs) > 0 {
		cluster.Status.ExternalIPs = extIPs
	}
	return true
}

// discoverExternalIPs reads each per-pod LoadBalancer Service's assigned external
// IP (or hostname) and returns a pod-name -> address map.
func (r *NodeClusterReconciler) discoverExternalIPs(ctx context.Context, cluster *v1alpha1.NodeCluster, replicas int32) map[string]string {
	ips := map[string]string{}
	for i := int32(0); i < replicas; i++ {
		name := fmt.Sprintf("%s-%d", cluster.Name, i)
		var svc corev1.Service
		if err := r.Get(ctx, client.ObjectKey{Name: name, Namespace: cluster.Namespace}, &svc); err != nil {
			continue
		}
		for _, ing := range svc.Status.LoadBalancer.Ingress {
			if ing.IP != "" {
				ips[name] = ing.IP
				break
			}
			if ing.Hostname != "" {
				ips[name] = ing.Hostname
				break
			}
		}
	}
	return ips
}

// ───────────────────────── Apply helpers ─────────────────────────

// reconcileOwned creates or updates an owned resource with a controller
// reference, reusing the shared mutate helper.
func (r *NodeClusterReconciler) reconcileOwned(ctx context.Context, cluster *v1alpha1.NodeCluster, desired client.Object) error {
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}
	existing := desired.DeepCopyObject().(client.Object)
	_, err := ctrl.CreateOrUpdate(ctx, r.Client, existing, func() error {
		existing.SetName(desired.GetName())
		existing.SetNamespace(desired.GetNamespace())
		return manifests.MutateFuncFor(existing, desired)()
	})
	return err
}

// reconcileSnapshotCronJob applies the snapshot CronJob (when configured) and
// reflects its last schedule time into status. CronJob is reconciled directly
// here because the shared mutate helper does not handle batch/v1 types.
func (r *NodeClusterReconciler) reconcileSnapshotCronJob(ctx context.Context, cluster *v1alpha1.NodeCluster, image string, rpcPort int32) error {
	desired := buildSnapshotCronJob(cluster, image, rpcPort)
	if desired == nil {
		return nil
	}
	if err := ctrl.SetControllerReference(cluster, desired, r.Scheme); err != nil {
		return err
	}
	var existing batchv1.CronJob
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}
	existing.Spec = desired.Spec
	if existing.Labels == nil {
		existing.Labels = map[string]string{}
	}
	for k, v := range desired.Labels {
		existing.Labels[k] = v
	}
	if err := r.Update(ctx, &existing); err != nil {
		return err
	}
	cluster.Status.LastSnapshotTime = existing.Status.LastScheduleTime
	return nil
}
