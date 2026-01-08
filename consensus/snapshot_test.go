// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package consensus

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
)

func TestNewSnapshot(t *testing.T) {
	hash := common.HexToHash("0x1234")
	state := NewOTSState(true)
	state.LastAnchoredBlock = 100

	snap := NewSnapshot(50, hash, state)

	if snap.Number != 50 {
		t.Errorf("Expected Number 50, got %d", snap.Number)
	}
	if snap.Hash != hash {
		t.Error("Hash mismatch")
	}
	if snap.State == nil {
		t.Fatal("State should not be nil")
	}
	if snap.State.LastAnchoredBlock != 100 {
		t.Errorf("Expected LastAnchoredBlock 100, got %d", snap.State.LastAnchoredBlock)
	}
}

func TestSnapshot_Copy(t *testing.T) {
	hash := common.HexToHash("0x1234")
	state := NewOTSState(true)
	state.LastAnchoredBlock = 100

	snap := NewSnapshot(50, hash, state)
	cpy := snap.Copy()

	// Verify copy has same values
	if cpy.Number != snap.Number {
		t.Error("Number mismatch in copy")
	}
	if cpy.Hash != snap.Hash {
		t.Error("Hash mismatch in copy")
	}
	if cpy.State.LastAnchoredBlock != snap.State.LastAnchoredBlock {
		t.Error("State mismatch in copy")
	}

	// Modify original
	snap.State.LastAnchoredBlock = 200
	if cpy.State.LastAnchoredBlock != 100 {
		t.Error("Copy should be independent from original")
	}
}

func TestSnapshot_EncodeDecode(t *testing.T) {
	hash := common.HexToHash("0x1234")
	state := NewOTSState(true)
	state.LastAnchoredBlock = 100
	triggerNode := common.HexToAddress("0xabcd")
	_ = state.Trigger(1, 50, 51, triggerNode, common.HexToHash("0xffff"))

	snap := NewSnapshot(51, hash, state)

	// Encode
	data, err := snap.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeSnapshot(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify
	if decoded.Number != snap.Number {
		t.Error("Number mismatch")
	}
	if decoded.Hash != snap.Hash {
		t.Error("Hash mismatch")
	}
	if decoded.State.LastAnchoredBlock != snap.State.LastAnchoredBlock {
		t.Error("LastAnchoredBlock mismatch")
	}
	if decoded.State.CurrentBatch == nil {
		t.Fatal("CurrentBatch should not be nil")
	}
	if decoded.State.CurrentBatch.StartBlock != snap.State.CurrentBatch.StartBlock {
		t.Error("StartBlock mismatch")
	}
}

func TestSnapshotManager_NewSnapshotManager(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, err := NewSnapshotManager(db, true)
	if err != nil {
		t.Fatalf("NewSnapshotManager failed: %v", err)
	}
	if sm == nil {
		t.Fatal("SnapshotManager should not be nil")
	}
	if !sm.otsEnabled {
		t.Error("otsEnabled should be true")
	}
}

func TestSnapshotManager_StoreAndGet(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	hash := common.HexToHash("0x1234")
	state := NewOTSState(true)
	state.LastAnchoredBlock = 100

	snap := NewSnapshot(50, hash, state)

	// Store snapshot
	if err := sm.StoreSnapshot(snap); err != nil {
		t.Fatalf("StoreSnapshot failed: %v", err)
	}

	// Get snapshot from cache
	retrieved, err := sm.GetSnapshot(hash)
	if err != nil {
		t.Fatalf("GetSnapshot failed: %v", err)
	}

	if retrieved.Number != snap.Number {
		t.Error("Number mismatch")
	}
	if retrieved.State.LastAnchoredBlock != snap.State.LastAnchoredBlock {
		t.Error("LastAnchoredBlock mismatch")
	}
}

func TestSnapshotManager_Persistence(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	// Create snapshot at persistence interval
	hash := common.HexToHash("0x1234")
	state := NewOTSState(true)
	state.LastAnchoredBlock = 500

	// Block 1024 should trigger persistence
	snap := NewSnapshot(1024, hash, state)

	if err := sm.StoreSnapshot(snap); err != nil {
		t.Fatalf("StoreSnapshot failed: %v", err)
	}

	// Clear cache
	sm.Clear()

	// Should be able to load from database
	retrieved, err := sm.GetSnapshot(hash)
	if err != nil {
		t.Fatalf("GetSnapshot from DB failed: %v", err)
	}

	if retrieved.Number != snap.Number {
		t.Error("Number mismatch after DB load")
	}
	if retrieved.State.LastAnchoredBlock != snap.State.LastAnchoredBlock {
		t.Error("LastAnchoredBlock mismatch after DB load")
	}
}

func TestSnapshotManager_ForceStore(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	// Create snapshot NOT at persistence interval
	hash := common.HexToHash("0x5678")
	state := NewOTSState(true)
	state.LastAnchoredBlock = 100

	snap := NewSnapshot(50, hash, state)

	// Force store
	if err := sm.ForceStore(snap); err != nil {
		t.Fatalf("ForceStore failed: %v", err)
	}

	// Clear cache
	sm.Clear()

	// Should be able to load from database
	retrieved, err := sm.GetSnapshot(hash)
	if err != nil {
		t.Fatalf("GetSnapshot after ForceStore failed: %v", err)
	}

	if retrieved.Number != 50 {
		t.Errorf("Expected Number 50, got %d", retrieved.Number)
	}
}

func TestSnapshotManager_HasSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	hash1 := common.HexToHash("0x1111")
	hash2 := common.HexToHash("0x2222")
	state := NewOTSState(true)

	snap := NewSnapshot(100, hash1, state)
	_ = sm.StoreSnapshot(snap)

	if !sm.HasSnapshot(hash1) {
		t.Error("Should have snapshot for hash1")
	}
	if sm.HasSnapshot(hash2) {
		t.Error("Should not have snapshot for hash2")
	}
}

func TestSnapshotManager_DeleteSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	hash := common.HexToHash("0x1234")
	state := NewOTSState(true)
	snap := NewSnapshot(1024, hash, state)

	// Store and force persist
	_ = sm.ForceStore(snap)

	// Verify it exists
	if !sm.HasSnapshot(hash) {
		t.Error("Snapshot should exist before delete")
	}

	// Delete
	if err := sm.DeleteSnapshot(hash); err != nil {
		t.Fatalf("DeleteSnapshot failed: %v", err)
	}

	// Verify it's gone
	if sm.HasSnapshot(hash) {
		t.Error("Snapshot should not exist after delete")
	}
}

func TestSnapshotManager_GetGenesisSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	genesisHash := common.HexToHash("0xgenesis")
	snap := sm.GetGenesisSnapshot(genesisHash)

	if snap.Number != 0 {
		t.Errorf("Genesis snapshot Number should be 0, got %d", snap.Number)
	}
	if snap.Hash != genesisHash {
		t.Error("Genesis hash mismatch")
	}
	if !snap.State.Enabled {
		t.Error("Genesis state should have OTS enabled")
	}
	if snap.State.LastAnchoredBlock != 0 {
		t.Error("Genesis LastAnchoredBlock should be 0")
	}
}

func TestSnapshotManager_CacheStats(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	size, capacity := sm.CacheStats()
	if size != 0 {
		t.Errorf("Initial cache size should be 0, got %d", size)
	}
	if capacity != snapshotCacheSize {
		t.Errorf("Expected capacity %d, got %d", snapshotCacheSize, capacity)
	}

	// Add some snapshots
	for i := 0; i < 5; i++ {
		hash := common.BigToHash(big.NewInt(int64(i + 1)))
		state := NewOTSState(true)
		snap := NewSnapshot(uint64(i), hash, state)
		_ = sm.StoreSnapshot(snap)
	}

	size, _ = sm.CacheStats()
	if size != 5 {
		t.Errorf("Expected cache size 5, got %d", size)
	}
}

func TestSnapshotManager_GetSnapshot_NotFound(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	hash := common.HexToHash("0xnonexistent")
	_, err := sm.GetSnapshot(hash)
	if err != ErrSnapshotNotFound {
		t.Errorf("Expected ErrSnapshotNotFound, got %v", err)
	}
}

func TestSnapshotManager_FindNearestSnapshot(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	sm, _ := NewSnapshotManager(db, true)

	// Store snapshots at persistence intervals
	for i := uint64(1); i <= 5; i++ {
		blockNum := i * snapshotPersistInterval
		hash := common.BigToHash(big.NewInt(int64(blockNum)))
		state := NewOTSState(true)
		state.LastAnchoredBlock = blockNum - 100
		snap := NewSnapshot(blockNum, hash, state)
		_ = sm.ForceStore(snap)
	}

	// Mock getHash function
	getHash := func(num uint64) common.Hash {
		if num%snapshotPersistInterval == 0 && num <= 5*snapshotPersistInterval {
			return common.BigToHash(big.NewInt(int64(num)))
		}
		return common.Hash{}
	}

	// Find nearest to block 3500 (between 3072 and 4096)
	// Since our intervals are 1024, should find 3072
	target := uint64(3500)
	snap, err := sm.FindNearestSnapshot(target, getHash)
	if err != nil {
		t.Fatalf("FindNearestSnapshot failed: %v", err)
	}

	expectedBlock := uint64(3072)
	if snap.Number != expectedBlock {
		t.Errorf("Expected nearest snapshot at %d, got %d", expectedBlock, snap.Number)
	}
}

