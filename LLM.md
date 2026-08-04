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

## License

Relicensed from BSD-3-Clause to the dual `MIT OR Apache-2.0` grant under
HIP-0137 ("One License", `hanzoai/hips`). `LICENSE` states the dual grant;
`LICENSE-MIT` and `LICENSE-APACHE` carry the full texts. The original BSD
copyright line — `2026, Hanzo AI, Inc.` — carries over verbatim into
`LICENSE-MIT`: the relicense changes the grant, not the copyright record.
