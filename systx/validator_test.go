// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package systx

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	otstypes "github.com/ethereum/go-ethereum/ots/types"
)

func TestDecodeCalldata(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	builder := NewBuilder(contractAddr)
	validator := NewValidator(contractAddr)

	// Build a valid calldata
	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:    "test-batch",
			StartBlock: 100,
			EndBlock:   200,
			RootHash:   common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			CreatedAt:  time.Now(),
		},
		BTCTxID:      "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		BTCTimestamp: 1700000000,
	}

	data, err := builder.encodeCalldata(candidate)
	if err != nil {
		t.Fatalf("encodeCalldata failed: %v", err)
	}

	// Decode the calldata
	decoded, err := validator.DecodeCalldata(data)
	if err != nil {
		t.Fatalf("DecodeCalldata failed: %v", err)
	}

	// Verify decoded values
	if decoded.StartBlock != 100 {
		t.Errorf("StartBlock = %d, want 100", decoded.StartBlock)
	}

	if decoded.EndBlock != 200 {
		t.Errorf("EndBlock = %d, want 200", decoded.EndBlock)
	}

	expectedRoot := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if decoded.BatchRoot != expectedRoot {
		t.Errorf("BatchRoot = %s, want %s", decoded.BatchRoot.Hex(), expectedRoot.Hex())
	}

	expectedTxHash := common.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if decoded.BTCTxHash != expectedTxHash {
		t.Errorf("BTCTxHash = %s, want %s", decoded.BTCTxHash.Hex(), expectedTxHash.Hex())
	}

	if decoded.BTCTimestamp != 1700000000 {
		t.Errorf("BTCTimestamp = %d, want 1700000000", decoded.BTCTimestamp)
	}
}

func TestDecodeCalldata_TooShort(t *testing.T) {
	validator := NewValidator(common.HexToAddress("0x9000"))

	// Too short calldata (less than 4 + 32*5 = 164 bytes)
	shortData := make([]byte, 100)
	copy(shortData[:4], anchorSig)

	_, err := validator.DecodeCalldata(shortData)
	if err != ErrInvalidCalldata {
		t.Errorf("expected ErrInvalidCalldata for short data, got %v", err)
	}
}

func TestDecodeCalldata_EmptyBatch(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	builder := NewBuilder(contractAddr)
	validator := NewValidator(contractAddr)

	// Empty batch
	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:    "empty",
			StartBlock: 1,
			EndBlock:   100,
			RootHash:   common.Hash{}, // Zero
			CreatedAt:  time.Now(),
		},
		BTCTxID:      "",
		BTCTimestamp: 0,
	}

	data, err := builder.encodeCalldata(candidate)
	if err != nil {
		t.Fatalf("encodeCalldata failed: %v", err)
	}

	decoded, err := validator.DecodeCalldata(data)
	if err != nil {
		t.Fatalf("DecodeCalldata failed: %v", err)
	}

	if decoded.BatchRoot != (common.Hash{}) {
		t.Errorf("BatchRoot should be zero, got %s", decoded.BatchRoot.Hex())
	}

	if decoded.BTCTxHash != (common.Hash{}) {
		t.Errorf("BTCTxHash should be zero, got %s", decoded.BTCTxHash.Hex())
	}

	if decoded.BTCTimestamp != 0 {
		t.Errorf("BTCTimestamp should be 0, got %d", decoded.BTCTimestamp)
	}
}

func TestValidateSystemTx(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	validator := NewValidator(contractAddr)
	coinbase := common.HexToAddress("0x1234")

	// Build valid calldata
	builder := NewBuilder(contractAddr)
	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:    "test",
			StartBlock: 1,
			EndBlock:   100,
			RootHash:   common.HexToHash("0xabcd"),
			CreatedAt:  time.Now(),
		},
		BTCTxID:      "1234",
		BTCTimestamp: 1700000000,
	}

	data, _ := builder.encodeCalldata(candidate)

	// Create a valid system transaction
	tx := types.NewTransaction(
		0,              // nonce
		contractAddr,   // to
		big.NewInt(0),  // value
		100000,         // gas
		big.NewInt(0),  // gasPrice = 0 (system tx)
		data,
	)

	err := validator.ValidateSystemTx(tx, coinbase)
	if err != nil {
		t.Errorf("ValidateSystemTx failed for valid tx: %v", err)
	}
}

func TestValidateSystemTx_NonZeroGasPrice(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	validator := NewValidator(contractAddr)
	coinbase := common.HexToAddress("0x1234")

	// Transaction with non-zero gas price
	tx := types.NewTransaction(
		0,
		contractAddr,
		big.NewInt(0),
		100000,
		big.NewInt(1), // Non-zero gasPrice
		anchorSig,
	)

	err := validator.ValidateSystemTx(tx, coinbase)
	if err != ErrNotSystemTx {
		t.Errorf("expected ErrNotSystemTx, got %v", err)
	}
}

func TestValidateSystemTx_WrongRecipient(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	validator := NewValidator(contractAddr)
	coinbase := common.HexToAddress("0x1234")

	wrongAddr := common.HexToAddress("0x9001")
	tx := types.NewTransaction(
		0,
		wrongAddr, // Wrong recipient
		big.NewInt(0),
		100000,
		big.NewInt(0),
		anchorSig,
	)

	err := validator.ValidateSystemTx(tx, coinbase)
	if err != ErrInvalidRecipient {
		t.Errorf("expected ErrInvalidRecipient, got %v", err)
	}
}

func TestValidateSystemTx_ShortCalldata(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	validator := NewValidator(contractAddr)
	coinbase := common.HexToAddress("0x1234")

	// Calldata too short (less than 4 bytes)
	tx := types.NewTransaction(
		0,
		contractAddr,
		big.NewInt(0),
		100000,
		big.NewInt(0),
		[]byte{0x01, 0x02}, // Only 2 bytes
	)

	err := validator.ValidateSystemTx(tx, coinbase)
	if err != ErrInvalidCalldata {
		t.Errorf("expected ErrInvalidCalldata, got %v", err)
	}
}

func TestValidateSystemTx_WrongSelector(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	validator := NewValidator(contractAddr)
	coinbase := common.HexToAddress("0x1234")

	// Wrong function selector
	wrongSelector := []byte{0xde, 0xad, 0xbe, 0xef}
	tx := types.NewTransaction(
		0,
		contractAddr,
		big.NewInt(0),
		100000,
		big.NewInt(0),
		wrongSelector,
	)

	err := validator.ValidateSystemTx(tx, coinbase)
	if err != ErrInvalidCalldata {
		t.Errorf("expected ErrInvalidCalldata, got %v", err)
	}
}
