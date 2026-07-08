# Hanzo Nchain

## Overview
Go module: github.com/hanzoai/nchain

## Tech Stack
- **Language**: Go

## Build & Run
```bash
go build ./...
go test ./...
```

## Structure
```
nchain/
  Dockerfile
  Makefile
  README.md
  api/
  cmd/
  config/
  go.mod
  go.sum
  internal/
```

## Key Files
- `README.md` -- Project documentation
- `go.mod` -- Go module definition
- `Makefile` -- Build automation
- `Dockerfile` -- Container build

## Web3 / operator architecture (plan of record)

Decomplected layers — **nothing archived**:
- **bootnode/operator** — "web3 backend in a box": easy local->cloud runner;
  `Cluster`/`ChainDeployment`/Fleet + pluggable chain drivers. KEEP (the easy front door).
- **hanzoai/nchain** (this repo, Go) — in-cluster web3 workload operator:
  `Network/NodeCluster/Chain/Indexer/Explorer/Gateway/Bridge/Cloud`. The canonical engine.
  Stack: **hanzoai/gateway + hanzoai/ingress** (TLS termination); **SQLite + native streaming
  replication** (no Postgres/Redis); **luxfi/explore + luxfi/indexer + luxfi/graph** (no
  Blockscout); per-pod L4 LoadBalancers for validator P2P.
- **hanzoai/operator (Rust) + luxfi/operator (Go)** — PaaS twins (apps/functions/IAM/KMS/
  datastores). luxfi/operator drops blockchain CRDs and consumes nchain.
- **luxfi/universe** — declarative infra (charts/k8s/CRs/configs); absorbs devops `charts/`+`k8s/`.
- **luxfi/devops** — public OSS run-tooling (`bench`/`benchup`/`luxd`/`luxup`/`lux-faucet`/
  `staking-*`/`deploy-all.sh`/`lux-core`). KEEP (public). Remove only the stale Rust
  `lux-operator/` (superseded by nchain).

Decomplect note: nchain's `Cloud.ClusterTarget` (cross-cluster) overlaps bootnode's Fleet —
bootnode owns provisioning/fleet + local-box; nchain owns in-cluster workloads + cross-cluster
targeting. Optional follow-up: move fleet semantics fully to bootnode.

Cutover safety: nchain must pass devnet runtime tests before retiring the live Rust
`lux/devops/lux-operator` (it reconciles mainnet/testnet/devnet today). Migrate devnet -> testnet -> mainnet.

## Operator Consolidation — plan of record (2026-06)

One operator per concern (decomplected). Consolidate INTO nchain; do not rewrite.

- **WEB3 (this repo, Go):** the single composable, brand-agnostic blockchain/node
  operator. CRDs: `Network, NodeCluster, Chain, Indexer, Explorer, Gateway, Bridge,
  Cloud` (+ `Named*`/`ChainRef` composition, `BrandConfig`/`IAMConfig`/`KMS*`).
  Powers `web3.hanzo.ai`; lux/zoo/all consume it.
- **PaaS twins:** `hanzoai/operator` (Rust) + `luxfi/operator` (Go) — apps/functions/
  datastores/IAM/KMS/gateway-as-PaaS. Keep ONLY PaaS; blockchain CRDs move here.
- **INFRA:** `luxfi/universe` — charts, k8s manifests, CRs, configs (declarative).
- **RETIRE:** `luxfi/devops` (Rust `lux-operator` superseded; `charts/`+`k8s/` → universe;
  ops tools → universe/own) then archive. `bootnode/operator` (`Cluster`/`ChainDeployment`
  → nchain `NodeCluster`/`Cloud`) fold in.

Merge approach (no rewrites): Go→Go is cheap (compatible libs k8s.io v0.35.x /
controller-runtime v0.23.x) — bring `luxfi/operator` blockchain Go logic
(`network_snapshot.go` snapshot/replication/seed-restore) into nchain. Production-
validator parity is only mature in Rust `lux/devops/lux-operator` — port the missing
reconcile BEHAVIOR (same resources/env/ports), Pike-style, not a redesign.

Gap list to merge into nchain (priority; refs = `lux/devops/lux-operator/src/controller.rs`):
1. Tier-1 (blocks mainnet): per-pod LoadBalancer + external-IP discovery (`:1375`);
   startup-gate init container (`:783`); SnapshotSchedule reconcile → CronJob +
   `admin.snapshot`/`admin_exportChain` + S3 (`:~4000`); RLP import (`admin_importChain`).
2. Tier-2: managed PostgreSQL for Indexer (`:3798`) + Explorer; Gateway auto-route
   discovery via `platform.getBlockchains` + KrakenD config (`:4707`, `:~4791`).
3. Tier-3: per-node `/ext/health` polling; Indexer DeFi/NFT/trace flags (`:3852`);
   KMS staking-key sync; structured consensus/EVM config.

Sequenced PRs: nchain Tier-1→2→3 (compile + devnet-runtime verified each) →
luxfi/operator drops blockchain CRDs (PaaS only, consumes nchain) → universe absorbs
devops `charts/`+`k8s/` (gateway de-duped to one source) → archive devops → fold+archive
bootnode. **Cutover safety:** nchain must reach Tier-1 parity + pass devnet runtime tests
BEFORE retiring the live Rust `devops/lux-operator` (it reconciles mainnet/testnet/devnet
today). Migrate devnet → testnet → mainnet.
