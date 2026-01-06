// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package rpc

import (
	"context"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/ots"
	"github.com/ethereum/go-ethereum/ots/merkle"
	"github.com/ethereum/go-ethereum/ots/storage"
	"github.com/ethereum/go-ethereum/ots/types"
)

// newTestStore creates a test store with in-memory database
func newTestStore() *storage.Store {
	memdb := memorydb.New()
	db := rawdb.NewDatabase(memdb)
	return storage.NewStoreWithDB(db)
}

// mockModule implements ModuleInterface for testing
type mockModule struct {
	running bool
}

func (m *mockModule) IsRunning() bool {
	return m.running
}

func (m *mockModule) Health() ots.HealthStatus {
	return ots.HealthStatus{Status: "healthy"}
}

func (m *mockModule) Config() *ots.Config {
	return &ots.Config{Mode: ots.ModeWatcher}
}

func TestVerifyRUID_NotFound(t *testing.T) {
	store := newTestStore()
	module := &mockModule{running: true}
	api := NewAPI(module, store)

	result, err := api.VerifyRUID(context.Background(), "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	if err != nil {
		t.Fatalf("VerifyRUID returned error: %v", err)
	}

	if result.Verified {
		t.Error("expected Verified=false for non-existent RUID")
	}

	if result.Message != "RUID not found in any batch" {
		t.Errorf("unexpected message: %s", result.Message)
	}
}

func TestVerifyRUID_PendingBatch(t *testing.T) {
	store := newTestStore()

	// Create a batch with an RUID
	ruid := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	meta := &types.BatchMeta{
		BatchID:    "test-batch-1",
		StartBlock: 1,
		EndBlock:   100,
		RootHash:   common.Hash{},
		EventRUIDs: []common.Hash{ruid},
		CreatedAt:  time.Now(),
	}

	if err := store.SaveBatchMeta(meta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Save attempt as pending
	attempt := &types.Attempt{
		BatchID: "test-batch-1",
		Status:  types.BatchStatusPending,
	}
	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatalf("SaveAttempt failed: %v", err)
	}

	module := &mockModule{running: true}
	api := NewAPI(module, store)

	result, err := api.VerifyRUID(context.Background(), ruid.Hex())
	if err != nil {
		t.Fatalf("VerifyRUID returned error: %v", err)
	}

	if result.Verified {
		t.Error("expected Verified=false for pending batch")
	}

	if result.BatchID != "test-batch-1" {
		t.Errorf("expected BatchID=test-batch-1, got %s", result.BatchID)
	}
}

func TestVerifyRUID_Confirmed(t *testing.T) {
	store := newTestStore()

	// Create RUIDs
	ruid1 := common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	ruid2 := common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	ruid3 := common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")

	ruids := []common.Hash{ruid1, ruid2, ruid3}

	// Build Merkle tree to get correct root using the merkle package
	tree, err := merkle.BuildFromRUIDs(ruids)
	if err != nil {
		t.Fatalf("BuildFromRUIDs failed: %v", err)
	}
	rootHash := tree.Root()

	meta := &types.BatchMeta{
		BatchID:    "test-batch-confirmed",
		StartBlock: 1,
		EndBlock:   100,
		RootHash:   rootHash,
		EventRUIDs: ruids,
		CreatedAt:  time.Now(),
	}

	if err := store.SaveBatchMeta(meta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Save attempt as confirmed
	attempt := &types.Attempt{
		BatchID:        "test-batch-confirmed",
		Status:         types.BatchStatusConfirmed,
		BTCBlockHeight: 800000,
		BTCTimestamp:   1700000000,
		BTCTxID:        "btctx123",
	}
	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatalf("SaveAttempt failed: %v", err)
	}

	module := &mockModule{running: true}
	api := NewAPI(module, store)

	// Verify one of the RUIDs
	result, err := api.VerifyRUID(context.Background(), ruid2.Hex())
	if err != nil {
		t.Fatalf("VerifyRUID returned error: %v", err)
	}

	if result.BatchID != "test-batch-confirmed" {
		t.Errorf("expected BatchID=test-batch-confirmed, got %s", result.BatchID)
	}

	// Now should be verified successfully
	if !result.Verified {
		t.Errorf("expected Verified=true, got false. Message: %s", result.Message)
	}

	if result.BTCBlockHeight != 800000 {
		t.Errorf("expected BTCBlockHeight=800000, got %d", result.BTCBlockHeight)
	}

	if result.BTCTimestamp != 1700000000 {
		t.Errorf("expected BTCTimestamp=1700000000, got %d", result.BTCTimestamp)
	}
}

func TestVerifyRUID_ModuleNotRunning(t *testing.T) {
	store := newTestStore()
	module := &mockModule{running: false}
	api := NewAPI(module, store)

	_, err := api.VerifyRUID(context.Background(), "0x1234")
	if err != ErrModuleNotRunning {
		t.Errorf("expected ErrModuleNotRunning, got %v", err)
	}
}

func TestGetBatchByRUID(t *testing.T) {
	store := newTestStore()

	ruid := common.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")

	meta := &types.BatchMeta{
		BatchID:    "ruid-test-batch",
		StartBlock: 50,
		EndBlock:   150,
		EventRUIDs: []common.Hash{ruid},
		CreatedAt:  time.Now(),
	}

	if err := store.SaveBatchMeta(meta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Retrieve by RUID
	var ruidBytes [32]byte
	copy(ruidBytes[:], ruid[:])

	retrieved, err := store.GetBatchByRUID(ruidBytes)
	if err != nil {
		t.Fatalf("GetBatchByRUID failed: %v", err)
	}

	if retrieved.BatchID != "ruid-test-batch" {
		t.Errorf("expected BatchID=ruid-test-batch, got %s", retrieved.BatchID)
	}

	if retrieved.StartBlock != 50 {
		t.Errorf("expected StartBlock=50, got %d", retrieved.StartBlock)
	}
}
