// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package systx

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	otstypes "github.com/ethereum/go-ethereum/ots/types"
)

func TestAnchorSig(t *testing.T) {
	// Verify the anchor function selector matches Solidity's
	// anchor(uint64,uint64,bytes32,bytes32,uint64)
	expectedSig := crypto.Keccak256([]byte("anchor(uint64,uint64,bytes32,bytes32,uint64)"))[:4]

	if len(anchorSig) != 4 {
		t.Fatalf("anchorSig should be 4 bytes, got %d", len(anchorSig))
	}

	for i := 0; i < 4; i++ {
		if anchorSig[i] != expectedSig[i] {
			t.Errorf("anchorSig[%d] = %x, want %x", i, anchorSig[i], expectedSig[i])
		}
	}
}

func TestBtcTxIDToBytes32(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected common.Hash
	}{
		{
			name:     "empty string",
			input:    "",
			expected: common.Hash{},
		},
		{
			name:     "valid hex without prefix",
			input:    "0000000000000000000000000000000000000000000000000000000000000001",
			expected: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
		},
		{
			name:     "valid hex with prefix",
			input:    "0x0000000000000000000000000000000000000000000000000000000000000001",
			expected: common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000001"),
		},
		{
			name:     "typical btc txid",
			input:    "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			expected: common.HexToHash("0xa1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := btcTxIDToBytes32(tt.input)
			if result != tt.expected {
				t.Errorf("btcTxIDToBytes32(%q) = %s, want %s", tt.input, result.Hex(), tt.expected.Hex())
			}
		})
	}
}

func TestEncodeCalldata(t *testing.T) {
	builder := NewBuilder(common.HexToAddress("0x9000"))

	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:    "test-batch",
			StartBlock: 1,
			EndBlock:   100,
			RootHash:   common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"),
			CreatedAt:  time.Now(),
		},
		BTCTxID:      "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
		BTCTimestamp: 1700000000,
	}

	data, err := builder.encodeCalldata(candidate)
	if err != nil {
		t.Fatalf("encodeCalldata failed: %v", err)
	}

	// Expected size: 4 (selector) + 32*5 (params) = 164 bytes
	expectedSize := 4 + 32*5
	if len(data) != expectedSize {
		t.Errorf("calldata length = %d, want %d", len(data), expectedSize)
	}

	// Verify function selector
	if data[0] != anchorSig[0] || data[1] != anchorSig[1] ||
		data[2] != anchorSig[2] || data[3] != anchorSig[3] {
		t.Error("function selector mismatch")
	}

	// Verify startBlock (offset 4, uint64 padded to 32 bytes)
	startBlockBytes := data[4 : 4+32]
	startBlock := new(big.Int).SetBytes(startBlockBytes).Uint64()
	if startBlock != 1 {
		t.Errorf("startBlock = %d, want 1", startBlock)
	}

	// Verify endBlock (offset 36)
	endBlockBytes := data[36 : 36+32]
	endBlock := new(big.Int).SetBytes(endBlockBytes).Uint64()
	if endBlock != 100 {
		t.Errorf("endBlock = %d, want 100", endBlock)
	}

	// Verify batchRoot (offset 68)
	var batchRoot common.Hash
	copy(batchRoot[:], data[68:68+32])
	expectedRoot := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if batchRoot != expectedRoot {
		t.Errorf("batchRoot = %s, want %s", batchRoot.Hex(), expectedRoot.Hex())
	}

	// Verify btcTxHash (offset 100)
	var btcTxHash common.Hash
	copy(btcTxHash[:], data[100:100+32])
	expectedTxHash := common.HexToHash("0xa1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2")
	if btcTxHash != expectedTxHash {
		t.Errorf("btcTxHash = %s, want %s", btcTxHash.Hex(), expectedTxHash.Hex())
	}

	// Verify btcTimestamp (offset 132)
	timestampBytes := data[132 : 132+32]
	timestamp := new(big.Int).SetBytes(timestampBytes).Uint64()
	if timestamp != 1700000000 {
		t.Errorf("btcTimestamp = %d, want 1700000000", timestamp)
	}
}

func TestEncodeCalldata_EmptyBatch(t *testing.T) {
	builder := NewBuilder(common.HexToAddress("0x9000"))

	// Empty batch: batchRoot = 0, btcTxHash = 0, btcTimestamp = 0
	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:    "empty-batch",
			StartBlock: 101,
			EndBlock:   200,
			RootHash:   common.Hash{}, // Zero hash for empty batch
			CreatedAt:  time.Now(),
		},
		BTCTxID:      "",
		BTCTimestamp: 0,
	}

	data, err := builder.encodeCalldata(candidate)
	if err != nil {
		t.Fatalf("encodeCalldata failed: %v", err)
	}

	// Verify batchRoot is zero (offset 68)
	var batchRoot common.Hash
	copy(batchRoot[:], data[68:68+32])
	if batchRoot != (common.Hash{}) {
		t.Errorf("batchRoot should be zero for empty batch, got %s", batchRoot.Hex())
	}

	// Verify btcTxHash is zero (offset 100)
	var btcTxHash common.Hash
	copy(btcTxHash[:], data[100:100+32])
	if btcTxHash != (common.Hash{}) {
		t.Errorf("btcTxHash should be zero for empty batch, got %s", btcTxHash.Hex())
	}

	// Verify btcTimestamp is zero (offset 132)
	timestampBytes := data[132 : 132+32]
	timestamp := new(big.Int).SetBytes(timestampBytes).Uint64()
	if timestamp != 0 {
		t.Errorf("btcTimestamp should be 0 for empty batch, got %d", timestamp)
	}
}

func TestBuildSystemTx(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	builder := NewBuilder(contractAddr)
	coinbase := common.HexToAddress("0x1234")

	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:    "test-batch",
			StartBlock: 1,
			EndBlock:   100,
			RootHash:   common.HexToHash("0xabcd"),
			CreatedAt:  time.Now(),
		},
		BTCTxID:      "deadbeef",
		BTCTimestamp: 1700000000,
	}

	tx, err := builder.BuildSystemTx(candidate, coinbase, 0, 100000)
	if err != nil {
		t.Fatalf("BuildSystemTx failed: %v", err)
	}

	// Verify transaction properties
	if tx.To() == nil || *tx.To() != contractAddr {
		t.Error("transaction to address incorrect")
	}

	if tx.GasPrice().Cmp(big.NewInt(0)) != 0 {
		t.Error("gasPrice should be 0 for system transaction")
	}

	if tx.Value().Cmp(big.NewInt(0)) != 0 {
		t.Error("value should be 0")
	}

	if tx.Gas() != 100000 {
		t.Errorf("gas = %d, want 100000", tx.Gas())
	}

	if tx.Nonce() != 0 {
		t.Errorf("nonce = %d, want 0", tx.Nonce())
	}
}

func TestBuildSystemTx_NilCandidate(t *testing.T) {
	builder := NewBuilder(common.HexToAddress("0x9000"))
	coinbase := common.HexToAddress("0x1234")

	_, err := builder.BuildSystemTx(nil, coinbase, 0, 100000)
	if err != ErrInvalidCandidate {
		t.Errorf("expected ErrInvalidCandidate, got %v", err)
	}
}

func TestBuildSystemTx_NilBatchMeta(t *testing.T) {
	builder := NewBuilder(common.HexToAddress("0x9000"))
	coinbase := common.HexToAddress("0x1234")

	candidate := &otstypes.CandidateBatch{
		BatchMeta: nil,
	}

	_, err := builder.BuildSystemTx(candidate, coinbase, 0, 100000)
	if err != ErrInvalidCandidate {
		t.Errorf("expected ErrInvalidCandidate, got %v", err)
	}
}

func TestEstimateGas(t *testing.T) {
	builder := NewBuilder(common.HexToAddress("0x9000"))

	gas := builder.EstimateGas()

	// Should be a reasonable estimate (> 50000 for contract call + storage)
	if gas < 50000 {
		t.Errorf("estimated gas %d seems too low", gas)
	}

	// Should not be unreasonably high
	if gas > 200000 {
		t.Errorf("estimated gas %d seems too high", gas)
	}
}

func TestValidateCandidate(t *testing.T) {
	builder := NewBuilder(common.HexToAddress("0x9000"))

	rootHash := common.HexToHash("0xabcd1234")
	candidate := &otstypes.CandidateBatch{
		BatchMeta: &otstypes.BatchMeta{
			BatchID:  "test",
			RootHash: rootHash,
		},
	}

	// Valid case: computed root matches
	err := builder.ValidateCandidate(candidate, rootHash)
	if err != nil {
		t.Errorf("ValidateCandidate failed: %v", err)
	}

	if !candidate.Validated {
		t.Error("candidate.Validated should be true")
	}

	// Invalid case: root mismatch
	candidate.Validated = false
	wrongRoot := common.HexToHash("0xdead")
	err = builder.ValidateCandidate(candidate, wrongRoot)
	if err != ErrRootMismatch {
		t.Errorf("expected ErrRootMismatch, got %v", err)
	}
}
