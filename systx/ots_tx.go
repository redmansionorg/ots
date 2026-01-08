// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// This file implements OTS-specific system transactions for consensus state changes.
// These transactions are used to synchronize OTS state across all nodes.

package systx

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

var (
	ErrInvalidOTSTx = errors.New("invalid OTS system transaction")

	// Function selectors for OTS system transactions
	// otsSubmitted(bytes32 rootHash, bytes32 otsDigest)
	OTSSubmittedSelector = crypto.Keccak256([]byte("otsSubmitted(bytes32,bytes32)"))[:4]

	// otsConfirmed(bytes32 rootHash, uint64 btcBlockHeight, bytes32 btcTxID, uint64 btcTimestamp)
	OTSConfirmedSelector = crypto.Keccak256([]byte("otsConfirmed(bytes32,uint64,bytes32,uint64)"))[:4]

	// anchor(uint64 startBlock, uint64 endBlock, bytes32 batchRoot, bytes32 btcTxHash, uint64 btcTimestamp)
	anchorSelector = crypto.Keccak256([]byte("anchor(uint64,uint64,bytes32,bytes32,uint64)"))[:4]
)

// CandidateBatch contains batch data for anchor transaction (local definition to avoid circular imports)
type CandidateBatch struct {
	RootHash       common.Hash
	StartBlock     uint64
	EndBlock       uint64
	EventRUIDs     []common.Hash
	BTCBlockHeight uint64
	BTCTxID        string
	BTCTimestamp   uint64
}

// OTSSubmittedParams contains parameters for otsSubmitted transaction
type OTSSubmittedParams struct {
	RootHash  common.Hash
	OTSDigest [32]byte
}

// OTSConfirmedParams contains parameters for otsConfirmed transaction
type OTSConfirmedParams struct {
	RootHash       common.Hash
	BTCBlockHeight uint64
	BTCTxID        [32]byte
	BTCTimestamp   uint64
}

// BuildOTSSubmittedTx builds an otsSubmitted system transaction
func (b *Builder) BuildOTSSubmittedTx(params *OTSSubmittedParams, coinbase common.Address, nonce uint64, gasLimit uint64) (*types.Transaction, error) {
	if params == nil {
		return nil, ErrInvalidOTSTx
	}

	// Build calldata: selector + rootHash + otsDigest
	calldata := make([]byte, 4+32+32)
	copy(calldata[0:4], OTSSubmittedSelector)
	copy(calldata[4:36], params.RootHash[:])
	copy(calldata[36:68], params.OTSDigest[:])

	// Create transaction with zero gas price (system transaction)
	tx := types.NewTransaction(
		nonce,
		b.contractAddress,
		big.NewInt(0), // zero value
		gasLimit,
		big.NewInt(0), // zero gas price
		calldata,
	)

	return tx, nil
}

// BuildOTSConfirmedTx builds an otsConfirmed system transaction
func (b *Builder) BuildOTSConfirmedTx(params *OTSConfirmedParams, coinbase common.Address, nonce uint64, gasLimit uint64) (*types.Transaction, error) {
	if params == nil {
		return nil, ErrInvalidOTSTx
	}

	// Build calldata: selector + rootHash + btcBlockHeight + btcTxID + btcTimestamp
	// Each parameter is padded to 32 bytes (ABI encoding)
	calldata := make([]byte, 4+32+32+32+32)
	copy(calldata[0:4], OTSConfirmedSelector)
	copy(calldata[4:36], params.RootHash[:])

	// btcBlockHeight (uint64 -> bytes32, right-aligned)
	btcBlockHeightBytes := common.BigToHash(big.NewInt(int64(params.BTCBlockHeight)))
	copy(calldata[36:68], btcBlockHeightBytes[:])

	// btcTxID (bytes32)
	copy(calldata[68:100], params.BTCTxID[:])

	// btcTimestamp (uint64 -> bytes32, right-aligned)
	btcTimestampBytes := common.BigToHash(big.NewInt(int64(params.BTCTimestamp)))
	copy(calldata[100:132], btcTimestampBytes[:])

	// Create transaction with zero gas price (system transaction)
	tx := types.NewTransaction(
		nonce,
		b.contractAddress,
		big.NewInt(0), // zero value
		gasLimit,
		big.NewInt(0), // zero gas price
		calldata,
	)

	return tx, nil
}

// BuildAnchorTx builds an anchor system transaction for the consensus-based OTS
func (b *Builder) BuildAnchorTx(candidate *CandidateBatch, coinbase common.Address, nonce uint64, gasLimit uint64) (*types.Transaction, error) {
	if candidate == nil {
		return nil, ErrInvalidOTSTx
	}

	// Build calldata using the same encoding as the original anchor function
	// anchor(uint64,uint64,bytes32,bytes32,uint64)
	dataSize := 4 + 32*5
	calldata := make([]byte, dataSize)

	offset := 0

	// Function selector
	copy(calldata[offset:offset+4], anchorSelector[:])
	offset += 4

	// startBlock (uint64)
	startValue := new(big.Int).SetUint64(candidate.StartBlock)
	copy(calldata[offset+32-len(startValue.Bytes()):offset+32], startValue.Bytes())
	offset += 32

	// endBlock (uint64)
	endValue := new(big.Int).SetUint64(candidate.EndBlock)
	copy(calldata[offset+32-len(endValue.Bytes()):offset+32], endValue.Bytes())
	offset += 32

	// batchRoot (bytes32)
	copy(calldata[offset:offset+32], candidate.RootHash[:])
	offset += 32

	// btcTxHash (bytes32)
	btcTxHash := btcTxIDToBytes32Local(candidate.BTCTxID)
	copy(calldata[offset:offset+32], btcTxHash[:])
	offset += 32

	// btcTimestamp (uint64)
	tsValue := new(big.Int).SetUint64(candidate.BTCTimestamp)
	copy(calldata[offset+32-len(tsValue.Bytes()):offset+32], tsValue.Bytes())

	// Create transaction with zero gas price (system transaction)
	tx := types.NewTransaction(
		nonce,
		b.contractAddress,
		big.NewInt(0), // zero value
		gasLimit,
		big.NewInt(0), // zero gas price
		calldata,
	)

	log.Debug("OTS: Built anchor transaction",
		"txHash", tx.Hash().Hex(),
		"startBlock", candidate.StartBlock,
		"endBlock", candidate.EndBlock,
		"rootHash", candidate.RootHash.Hex(),
	)

	return tx, nil
}

// btcTxIDToBytes32Local converts a Bitcoin transaction ID to bytes32
func btcTxIDToBytes32Local(txid string) common.Hash {
	if txid == "" {
		return common.Hash{}
	}
	if len(txid) >= 2 && txid[:2] == "0x" {
		txid = txid[2:]
	}
	return common.HexToHash(txid)
}

// DecodeOTSSubmittedTx decodes an otsSubmitted transaction
func DecodeOTSSubmittedTx(tx *types.Transaction) (*OTSSubmittedParams, error) {
	data := tx.Data()
	if len(data) < 68 {
		return nil, ErrInvalidOTSTx
	}

	// Check selector
	if !matchSelector(data[:4], OTSSubmittedSelector) {
		return nil, ErrInvalidOTSTx
	}

	params := &OTSSubmittedParams{}
	copy(params.RootHash[:], data[4:36])
	copy(params.OTSDigest[:], data[36:68])

	return params, nil
}

// DecodeOTSConfirmedTx decodes an otsConfirmed transaction
func DecodeOTSConfirmedTx(tx *types.Transaction) (*OTSConfirmedParams, error) {
	data := tx.Data()
	if len(data) < 132 {
		return nil, ErrInvalidOTSTx
	}

	// Check selector
	if !matchSelector(data[:4], OTSConfirmedSelector) {
		return nil, ErrInvalidOTSTx
	}

	params := &OTSConfirmedParams{}
	copy(params.RootHash[:], data[4:36])

	// btcBlockHeight
	params.BTCBlockHeight = common.BytesToHash(data[36:68]).Big().Uint64()

	// btcTxID
	copy(params.BTCTxID[:], data[68:100])

	// btcTimestamp
	params.BTCTimestamp = common.BytesToHash(data[100:132]).Big().Uint64()

	return params, nil
}

// IsOTSSubmittedTx checks if a transaction is an otsSubmitted system transaction
func IsOTSSubmittedTx(tx *types.Transaction) bool {
	data := tx.Data()
	if len(data) < 4 {
		return false
	}
	return matchSelector(data[:4], OTSSubmittedSelector)
}

// IsOTSConfirmedTx checks if a transaction is an otsConfirmed system transaction
func IsOTSConfirmedTx(tx *types.Transaction) bool {
	data := tx.Data()
	if len(data) < 4 {
		return false
	}
	return matchSelector(data[:4], OTSConfirmedSelector)
}

// IsOTSSystemTx checks if a transaction is any OTS system transaction
func IsOTSSystemTx(tx *types.Transaction) bool {
	return IsOTSSubmittedTx(tx) || IsOTSConfirmedTx(tx) || IsAnchorTx(tx)
}

// IsAnchorTx checks if a transaction is an anchor system transaction
func IsAnchorTx(tx *types.Transaction) bool {
	data := tx.Data()
	if len(data) < 4 {
		return false
	}
	return matchSelector(data[:4], anchorSelector[:])
}

// DecodedAnchorCalldata represents decoded anchor parameters
type DecodedAnchorCalldata struct {
	StartBlock     uint64
	EndBlock       uint64
	RootHash       common.Hash
	BTCTxHash      common.Hash
	BTCBlockHeight uint64
	BTCTimestamp   uint64
}

// DecodeCalldata decodes anchor calldata from transaction data
func DecodeCalldata(data []byte) (*DecodedAnchorCalldata, error) {
	// Minimum size: 4 (selector) + 32*5 (5 fixed params)
	if len(data) < 4+32*5 {
		return nil, ErrInvalidOTSTx
	}

	// Skip function selector
	offset := 4

	// startBlock (uint64)
	startBlock := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()
	offset += 32

	// endBlock (uint64)
	endBlock := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()
	offset += 32

	// batchRoot (bytes32)
	var rootHash common.Hash
	copy(rootHash[:], data[offset:offset+32])
	offset += 32

	// btcTxHash (bytes32)
	var btcTxHash common.Hash
	copy(btcTxHash[:], data[offset:offset+32])
	offset += 32

	// btcTimestamp (uint64)
	btcTimestamp := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()

	return &DecodedAnchorCalldata{
		StartBlock:   startBlock,
		EndBlock:     endBlock,
		RootHash:     rootHash,
		BTCTxHash:    btcTxHash,
		BTCTimestamp: btcTimestamp,
	}, nil
}

// matchSelector compares two byte slices for selector matching
func matchSelector(a, b []byte) bool {
	if len(a) < 4 || len(b) < 4 {
		return false
	}
	return a[0] == b[0] && a[1] == b[1] && a[2] == b[2] && a[3] == b[3]
}

// ValidateSystemTx validates basic system transaction properties
func ValidateSystemTx(tx *types.Transaction, contractAddr common.Address) error {
	// Check gasPrice == 0
	if tx.GasPrice().Cmp(big.NewInt(0)) != 0 {
		return errors.New("not a system transaction (gasPrice != 0)")
	}

	// Check recipient
	if tx.To() == nil || *tx.To() != contractAddr {
		return errors.New("invalid recipient address")
	}

	return nil
}

// ValidateOTSSubmittedTx validates an otsSubmitted system transaction
func ValidateOTSSubmittedTx(tx *types.Transaction, contractAddr common.Address) error {
	// Check basic system tx properties
	if err := ValidateSystemTx(tx, contractAddr); err != nil {
		return err
	}

	// Check selector
	if !IsOTSSubmittedTx(tx) {
		return ErrInvalidOTSTx
	}

	// Check data length
	if len(tx.Data()) < 68 {
		return ErrInvalidOTSTx
	}

	return nil
}

// ValidateOTSConfirmedTx validates an otsConfirmed system transaction
func ValidateOTSConfirmedTx(tx *types.Transaction, contractAddr common.Address) error {
	// Check basic system tx properties
	if err := ValidateSystemTx(tx, contractAddr); err != nil {
		return err
	}

	// Check selector
	if !IsOTSConfirmedTx(tx) {
		return ErrInvalidOTSTx
	}

	// Check data length
	if len(tx.Data()) < 132 {
		return ErrInvalidOTSTx
	}

	return nil
}
