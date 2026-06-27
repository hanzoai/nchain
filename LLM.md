# LLM.md - Hanzo Nchain

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
