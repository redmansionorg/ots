// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package systx

import (
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
)

var (
	ErrNotSystemTx      = errors.New("systx: not a system transaction (gasPrice != 0)")
	ErrInvalidSender    = errors.New("systx: sender is not coinbase")
	ErrInvalidRecipient = errors.New("systx: invalid recipient address")
	ErrInvalidCalldata  = errors.New("systx: invalid calldata")
)

// Validator validates system transactions
type Validator struct {
	contractAddress common.Address
}

// NewValidator creates a new system transaction validator
func NewValidator(contractAddress common.Address) *Validator {
	return &Validator{
		contractAddress: contractAddress,
	}
}

// ValidateSystemTx validates that a transaction is a valid OTS system transaction.
// Checks:
// 1. gasPrice == 0
// 2. sender == coinbase
// 3. to == CopyrightRegistry contract
// 4. calldata starts with anchor selector
func (v *Validator) ValidateSystemTx(tx *types.Transaction, coinbase common.Address) error {
	// Check gasPrice == 0
	if tx.GasPrice().Cmp(big.NewInt(0)) != 0 {
		return ErrNotSystemTx
	}

	// Check recipient
	if tx.To() == nil || *tx.To() != v.contractAddress {
		return ErrInvalidRecipient
	}

	// Check calldata has valid selector
	data := tx.Data()
	if len(data) < 4 {
		return ErrInvalidCalldata
	}

	// Verify function selector matches anchor(uint64,uint64,bytes32,bytes32,uint64)
	if data[0] != anchorSig[0] ||
		data[1] != anchorSig[1] ||
		data[2] != anchorSig[2] ||
		data[3] != anchorSig[3] {
		return ErrInvalidCalldata
	}

	log.Debug("OTS: System transaction validated",
		"txHash", tx.Hash().Hex(),
		"to", tx.To().Hex(),
	)

	return nil
}

// DecodeCalldata decodes the anchor calldata
// anchor(uint64 startBlock, uint64 endBlock, bytes32 batchRoot, bytes32 btcTxHash, uint64 btcTimestamp)
func (v *Validator) DecodeCalldata(data []byte) (*DecodedCalldata, error) {
	// Minimum size: 4 (selector) + 32*5 (5 fixed params)
	if len(data) < 4+32*5 {
		return nil, ErrInvalidCalldata
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
	var batchRoot common.Hash
	copy(batchRoot[:], data[offset:offset+32])
	offset += 32

	// btcTxHash (bytes32)
	var btcTxHash common.Hash
	copy(btcTxHash[:], data[offset:offset+32])
	offset += 32

	// btcTimestamp (uint64)
	btcTimestamp := new(big.Int).SetBytes(data[offset : offset+32]).Uint64()

	return &DecodedCalldata{
		StartBlock:   startBlock,
		EndBlock:     endBlock,
		BatchRoot:    batchRoot,
		BTCTxHash:    btcTxHash,
		BTCTimestamp: btcTimestamp,
	}, nil
}

// DecodedCalldata represents decoded anchor parameters
type DecodedCalldata struct {
	StartBlock   uint64
	EndBlock     uint64
	BatchRoot    common.Hash
	BTCTxHash    common.Hash
	BTCTimestamp uint64
}
