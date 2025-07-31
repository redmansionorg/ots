# 🔌 Integrating OTS Plugin with Redmansion Node (BSC Fork)

This guide shows how to integrate the OTS plugin into a Redmansion Chain node based on BSC.

## ✅ Prerequisites

- BSC forked node (tag: v1.5.17 or later)
- Go 1.21+
- `ots/` plugin cloned or added as submodule

## 🔧 Steps

1. Copy `src/consensus/ots_engine.go` into `node/consensus/ots/`
2. Modify `node/core/blockchain.go` to call `otsEngine.FinalizeBlock()`
3. Import `ots/utils/merkle.go` for building RUID Merkle Tree
4. Register `ots` engine in consensus engine switcher
5. Rebuild node with `make geth`
6. Monitor OTS events via `eth_getLogs` on `NewOTSAnchor(uint timestamp, bytes32 merkleRoot, string btcTxHash)`

## 🔄 Maintaining Compatibility

See `compatibility.json` for tested versions.
