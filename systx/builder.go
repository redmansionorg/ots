// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// Package systx implements the system transaction builder for OTS.
// Design reference: docs/08-05-system-transaction.md

package systx

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	otstypes "github.com/ethereum/go-ethereum/ots/types"
)

var (
	ErrInvalidCandidate = errors.New("systx: invalid candidate batch")
	ErrRootMismatch     = errors.New("systx: root hash mismatch during validation")
	ErrBuildFailed      = errors.New("systx: failed to build transaction")
)

// anchor function signature
// function anchor(
//     uint64  startBlock,
//     uint64  endBlock,
//     bytes32 batchRoot,
//     bytes32 btcTxHash,
//     uint64  btcTimestamp
// ) external onlyInit onlyCoinbase onlySystemTx;
var anchorSig = crypto.Keccak256([]byte("anchor(uint64,uint64,bytes32,bytes32,uint64)"))[:4]

// Builder constructs system transactions for OTS anchoring
type Builder struct {
	contractAddress common.Address
	contractABI     abi.ABI
}

// NewBuilder creates a new system transaction builder
func NewBuilder(contractAddress common.Address) *Builder {
	return &Builder{
		contractAddress: contractAddress,
	}
}

// BuildSystemTx constructs a system transaction for the given candidate batch.
// The transaction has:
// - from: coinbase (block producer)
// - to: CopyrightRegistry contract (0x9000)
// - gasPrice: 0 (system transaction)
// - data: anchor(startBlock, endBlock, batchRoot, btcTxHash, btcTimestamp)
//
// Note: Empty batches are allowed (batchRoot=0, btcTxHash=0, btcTimestamp=0)
func (b *Builder) BuildSystemTx(
	candidate *otstypes.CandidateBatch,
	coinbase common.Address,
	nonce uint64,
	gasLimit uint64,
) (*types.Transaction, error) {
	// Validate candidate
	if candidate == nil || candidate.BatchMeta == nil {
		return nil, ErrInvalidCandidate
	}

	// Build calldata
	data, err := b.encodeCalldata(candidate)
	if err != nil {
		return nil, err
	}

	// Create transaction with zero gas price (system transaction)
	tx := types.NewTransaction(
		nonce,
		b.contractAddress,
		big.NewInt(0), // value = 0
		gasLimit,
		big.NewInt(0), // gasPrice = 0 (system transaction)
		data,
	)

	log.Debug("OTS: Built system transaction",
		"batchId", candidate.BatchID,
		"txHash", tx.Hash().Hex(),
		"startBlock", candidate.StartBlock,
		"endBlock", candidate.EndBlock,
		"ruids", len(candidate.EventRUIDs),
		"gasLimit", gasLimit,
	)

	return tx, nil
}

// encodeCalldata encodes the anchor function call
func (b *Builder) encodeCalldata(candidate *otstypes.CandidateBatch) ([]byte, error) {
	// Manual ABI encoding for anchor(uint64,uint64,bytes32,bytes32,uint64)
	//
	// Layout:
	// - 4 bytes: function selector
	// - 32 bytes: startBlock (uint64 padded to 32 bytes)
	// - 32 bytes: endBlock (uint64 padded to 32 bytes)
	// - 32 bytes: batchRoot (bytes32)
	// - 32 bytes: btcTxHash (bytes32)
	// - 32 bytes: btcTimestamp (uint64 padded to 32 bytes)

	dataSize := 4 + 32*5
	data := make([]byte, dataSize)

	offset := 0

	// Function selector
	copy(data[offset:offset+4], anchorSig)
	offset += 4

	// startBlock (uint64)
	startValue := new(big.Int).SetUint64(candidate.StartBlock)
	copy(data[offset+32-len(startValue.Bytes()):offset+32], startValue.Bytes())
	offset += 32

	// endBlock (uint64)
	endValue := new(big.Int).SetUint64(candidate.EndBlock)
	copy(data[offset+32-len(endValue.Bytes()):offset+32], endValue.Bytes())
	offset += 32

	// batchRoot (bytes32) - can be 0 for empty batches
	copy(data[offset:offset+32], candidate.RootHash[:])
	offset += 32

	// btcTxHash (bytes32) - convert BTCTxID string to bytes32
	btcTxHash := btcTxIDToBytes32(candidate.BTCTxID)
	copy(data[offset:offset+32], btcTxHash[:])
	offset += 32

	// btcTimestamp (uint64)
	tsValue := new(big.Int).SetUint64(candidate.BTCTimestamp)
	copy(data[offset+32-len(tsValue.Bytes()):offset+32], tsValue.Bytes())

	return data, nil
}

// btcTxIDToBytes32 converts a Bitcoin transaction ID (hex string) to bytes32.
// Bitcoin txids are 32 bytes displayed as 64 hex characters.
// Returns zero bytes32 if the input is empty or invalid.
func btcTxIDToBytes32(txid string) common.Hash {
	if txid == "" {
		return common.Hash{}
	}
	// Remove "0x" prefix if present
	if len(txid) >= 2 && txid[:2] == "0x" {
		txid = txid[2:]
	}
	// Bitcoin txids are displayed in reverse byte order, but we store as-is
	return common.HexToHash(txid)
}

// ValidateCandidate validates a candidate batch before building a system transaction.
// This performs the "double verification" by recomputing the MerkleRoot.
func (b *Builder) ValidateCandidate(candidate *otstypes.CandidateBatch, computedRoot common.Hash) error {
	if candidate == nil || candidate.BatchMeta == nil {
		return ErrInvalidCandidate
	}

	// Verify root matches
	if candidate.RootHash != computedRoot {
		log.Error("OTS: Root mismatch during validation",
			"batchId", candidate.BatchID,
			"candidateRoot", candidate.RootHash.Hex(),
			"computedRoot", computedRoot.Hex(),
		)
		return ErrRootMismatch
	}

	candidate.Validated = true
	return nil
}

// EstimateGas estimates the gas required for the system transaction.
// The anchor function has fixed parameters (no dynamic arrays), so gas is predictable.
func (b *Builder) EstimateGas() uint64 {
	// Base cost: ~30,000 gas for contract call overhead
	baseCost := uint64(30000)

	// Storage cost: ~20,000 gas for new BatchRecord
	storageCost := uint64(20000)

	// State update: ~5,000 gas for lastAnchoredEndBlock and batchCount
	stateUpdateCost := uint64(5000)

	// Event emission: ~5,000 gas
	eventCost := uint64(5000)

	return baseCost + storageCost + stateUpdateCost + eventCost
}
