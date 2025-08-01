# 📦 Redmansion OTS Plugin

On-chain Timestamping Consensus Module

This repository provides the On-chain Timestamping (OTS) consensus plugin for Redmansion Chain. It enables block-level timestamp proofs anchored to Bitcoin via OpenTimestamps, supporting decentralized copyright notarization for literature and art metadata.

## ✨ Features

- Modular consensus integration for forked BSC
- Supports Merkle Root batching for RUIDs
- Records BTC txHash and timestamp anchor on-chain
- Designed for integration with Redmansion Chain (RMC)

## 📁 Directory

- `src/` – Plugin logic (`consensus/ots_engine.go`, `utils/merkle.go`)
- `test/` – Unit tests
- `integration-guide.md` – Integration steps for RMC/BSC
- `compatibility.json` – BSC version compatibility matrix

## 🔧 Integration with RMC

Follow `integration-guide.md` to patch or embed OTS into Redmansion Chain node (RMC). Compatible with BSC v1.5.17+.

## 📜 License

This plugin is licensed under the **Business Source License 1.1**. It restricts commercial usage until:

> 🗓 Change Date: **2030-01-01**  
> 📖 Future License: GNU General Public License v3.0 or later

See [LICENSE](./LICENSE) for details.
