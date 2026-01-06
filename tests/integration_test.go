// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// Package tests contains integration tests that verify the complete OTS workflow:
// 1. Event collection -> Merkle tree construction
// 2. Batch creation -> Storage
// 3. System transaction building
// 4. RUID verification

package tests

import (
	"context"
	"crypto/sha256"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/ots/merkle"
	"github.com/ethereum/go-ethereum/ots/storage"
	"github.com/ethereum/go-ethereum/ots/systx"
	"github.com/ethereum/go-ethereum/ots/types"
)

// TestIntegration_EventToMerkleTree tests the flow from events to Merkle tree
func TestIntegration_EventToMerkleTree(t *testing.T) {
	// Simulate copyright claimed events
	events := []types.EventForMerkle{
		{
			RUID:      common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			SortKey:   types.SortKey{BlockNumber: 100, TxIndex: 0, LogIndex: 0},
			TxHash:    common.HexToHash("0xaaaa"),
			BlockHash: common.HexToHash("0xbbbb"),
		},
		{
			RUID:      common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			SortKey:   types.SortKey{BlockNumber: 100, TxIndex: 1, LogIndex: 0},
			TxHash:    common.HexToHash("0xcccc"),
			BlockHash: common.HexToHash("0xdddd"),
		},
		{
			RUID:      common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
			SortKey:   types.SortKey{BlockNumber: 101, TxIndex: 0, LogIndex: 0},
			TxHash:    common.HexToHash("0xeeee"),
			BlockHash: common.HexToHash("0xffff"),
		},
	}

	// Build Merkle tree from events
	tree, err := merkle.BuildFromEvents(events)
	if err != nil {
		t.Fatalf("BuildFromEvents failed: %v", err)
	}

	// Verify tree properties
	if tree.LeafCount() != 3 {
		t.Errorf("expected 3 leaves, got %d", tree.LeafCount())
	}

	root := tree.Root()
	if root == (common.Hash{}) {
		t.Error("root should not be empty")
	}

	// Verify OTS digest (SHA256 of root)
	otsDigest := tree.OTSDigest()
	expectedDigest := sha256.Sum256(root[:])
	if otsDigest != expectedDigest {
		t.Error("OTS digest mismatch")
	}

	// Verify we can generate proofs for each RUID
	for _, event := range events {
		proof, err := tree.GetProof(event.RUID)
		if err != nil {
			t.Errorf("GetProof failed for RUID %s: %v", event.RUID.Hex(), err)
			continue
		}

		// Verify the proof
		if !proof.VerifyRUID(event.RUID) {
			t.Errorf("proof verification failed for RUID %s", event.RUID.Hex())
		}

		// Verify proof root matches tree root
		if proof.Root != root {
			t.Errorf("proof root mismatch for RUID %s", event.RUID.Hex())
		}
	}

	t.Logf("Merkle tree built successfully: root=%s, leaves=%d", root.Hex()[:18], tree.LeafCount())
}

// TestIntegration_BatchCreationAndStorage tests batch creation and storage
func TestIntegration_BatchCreationAndStorage(t *testing.T) {
	// Create in-memory storage
	memdb := memorydb.New()
	db := rawdb.NewDatabase(memdb)
	store := storage.NewStoreWithDB(db)

	// Create RUIDs
	ruids := []common.Hash{
		common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
	}

	// Build Merkle tree
	tree, err := merkle.BuildFromRUIDs(ruids)
	if err != nil {
		t.Fatalf("BuildFromRUIDs failed: %v", err)
	}

	// Create batch metadata
	batchID := "20240101-000001"
	meta := &types.BatchMeta{
		BatchID:     batchID,
		StartBlock:  100,
		EndBlock:    200,
		RootHash:    tree.Root(),
		OTSDigest:   tree.OTSDigest(),
		RUIDCount:   uint32(len(ruids)),
		EventRUIDs:  ruids,
		CreatedAt:   time.Now(),
		TriggerType: types.TriggerTypeDaily,
	}

	// Save batch meta
	if err := store.SaveBatchMeta(meta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Save attempt (simulating OTS submission)
	attempt := &types.Attempt{
		BatchID:       batchID,
		Status:        types.BatchStatusSubmitted,
		AttemptCount:  1,
		LastAttemptAt: time.Now(),
	}
	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatalf("SaveAttempt failed: %v", err)
	}

	// Retrieve and verify
	retrievedMeta, err := store.GetBatchMeta(batchID)
	if err != nil {
		t.Fatalf("GetBatchMeta failed: %v", err)
	}

	if retrievedMeta.RootHash != meta.RootHash {
		t.Error("RootHash mismatch after retrieval")
	}

	if len(retrievedMeta.EventRUIDs) != len(ruids) {
		t.Error("EventRUIDs count mismatch after retrieval")
	}

	// Test RUID lookup
	for _, ruid := range ruids {
		var ruidBytes [32]byte
		copy(ruidBytes[:], ruid[:])
		foundMeta, err := store.GetBatchByRUID(ruidBytes)
		if err != nil {
			t.Errorf("GetBatchByRUID failed for %s: %v", ruid.Hex()[:18], err)
			continue
		}
		if foundMeta.BatchID != batchID {
			t.Errorf("wrong batch found for RUID %s", ruid.Hex()[:18])
		}
	}

	// Test digest lookup
	foundMeta, err := store.GetBatchByDigest(meta.OTSDigest)
	if err != nil {
		t.Fatalf("GetBatchByDigest failed: %v", err)
	}
	if foundMeta.BatchID != batchID {
		t.Error("wrong batch found by digest")
	}

	t.Logf("Batch created and stored successfully: id=%s, ruids=%d", batchID, len(ruids))
}

// TestIntegration_SystemTxBuilding tests system transaction construction
func TestIntegration_SystemTxBuilding(t *testing.T) {
	// Create contract address (0x9000)
	contractAddr := common.HexToAddress("0x0000000000000000000000000000000000009000")
	builder := systx.NewBuilder(contractAddr)

	// Create RUIDs and build tree
	ruids := []common.Hash{
		common.HexToHash("0xaaaa"),
		common.HexToHash("0xbbbb"),
	}
	tree, _ := merkle.BuildFromRUIDs(ruids)

	// Create batch metadata
	meta := &types.BatchMeta{
		BatchID:    "test-batch",
		StartBlock: 1000,
		EndBlock:   1100,
		RootHash:   tree.Root(),
		OTSDigest:  tree.OTSDigest(),
		RUIDCount:  2,
		EventRUIDs: ruids,
	}

	// Create candidate batch (simulating BTC confirmation)
	candidate := &types.CandidateBatch{
		BatchMeta:      meta,
		EventRUIDs:     ruids,
		BTCBlockHeight: 800000,
		BTCTxID:        "abcd1234567890abcd1234567890abcd1234567890abcd1234567890abcd1234",
		BTCTimestamp:   1700000000,
	}

	// Build system transaction
	coinbase := common.HexToAddress("0x1234567890123456789012345678901234567890")
	nonce := uint64(5)
	gasLimit := uint64(500000)

	tx, err := builder.BuildSystemTx(candidate, coinbase, nonce, gasLimit)
	if err != nil {
		t.Fatalf("BuildSystemTx failed: %v", err)
	}

	// Verify transaction properties
	if tx.To() == nil || *tx.To() != contractAddr {
		t.Error("wrong transaction recipient")
	}

	if tx.GasPrice().Uint64() != 0 {
		t.Error("system tx should have zero gas price")
	}

	if tx.Nonce() != nonce {
		t.Errorf("expected nonce %d, got %d", nonce, tx.Nonce())
	}

	// Validate the transaction using validator
	validator := systx.NewValidator(contractAddr)
	decoded, err := validator.DecodeCalldata(tx.Data())
	if err != nil {
		t.Fatalf("DecodeCalldata failed: %v", err)
	}

	if decoded.StartBlock != meta.StartBlock {
		t.Errorf("decoded StartBlock mismatch: expected %d, got %d", meta.StartBlock, decoded.StartBlock)
	}

	if decoded.EndBlock != meta.EndBlock {
		t.Errorf("decoded EndBlock mismatch: expected %d, got %d", meta.EndBlock, decoded.EndBlock)
	}

	if decoded.BatchRoot != meta.RootHash {
		t.Error("decoded BatchRoot mismatch")
	}

	if decoded.BTCTimestamp != candidate.BTCTimestamp {
		t.Errorf("decoded BTCTimestamp mismatch: expected %d, got %d", candidate.BTCTimestamp, decoded.BTCTimestamp)
	}

	t.Logf("System tx built successfully: hash=%s, gas=%d", tx.Hash().Hex()[:18], tx.Gas())
}

// TestIntegration_FullFlow tests the complete OTS workflow
func TestIntegration_FullFlow(t *testing.T) {
	ctx := context.Background()

	// Step 1: Create storage
	memdb := memorydb.New()
	db := rawdb.NewDatabase(memdb)
	store := storage.NewStoreWithDB(db)

	// Step 2: Simulate copyright claim events
	// In production, these would come from blockchain logs
	puid := common.HexToHash("0x1234") // Product ID
	auid := common.HexToHash("0x5678") // Asset ID
	claimant := common.HexToAddress("0xabcd")
	blockNumber := uint64(12345)

	// Calculate RUID (keccak256(puid, auid))
	ruidData := append(puid[:], auid[:]...)
	ruid := crypto.Keccak256Hash(ruidData)

	events := []types.EventForMerkle{
		{
			RUID:      ruid,
			SortKey:   types.SortKey{BlockNumber: blockNumber, TxIndex: 0, LogIndex: 0},
			TxHash:    common.HexToHash("0xaaaa"),
			BlockHash: common.HexToHash("0xbbbb"),
		},
	}

	t.Logf("Step 1: Simulated copyright claim - RUID=%s, claimant=%s", ruid.Hex()[:18], claimant.Hex())

	// Step 3: Build Merkle tree
	tree, err := merkle.BuildFromEvents(events)
	if err != nil {
		t.Fatalf("BuildFromEvents failed: %v", err)
	}
	rootHash := tree.Root()
	otsDigest := tree.OTSDigest()

	t.Logf("Step 2: Built Merkle tree - root=%s, leaves=%d", rootHash.Hex()[:18], tree.LeafCount())

	// Step 4: Create and save batch
	batchID := "batch-12345"
	batchMeta := &types.BatchMeta{
		BatchID:     batchID,
		StartBlock:  blockNumber,
		EndBlock:    blockNumber,
		RootHash:    rootHash,
		OTSDigest:   otsDigest,
		RUIDCount:   1,
		EventRUIDs:  []common.Hash{ruid},
		CreatedAt:   time.Now(),
		TriggerType: types.TriggerTypeDaily,
	}

	if err := store.SaveBatchMeta(batchMeta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Step 5: Simulate OTS submission (pending status)
	attempt := &types.Attempt{
		BatchID:       batchID,
		Status:        types.BatchStatusPending,
		AttemptCount:  1,
		LastAttemptAt: time.Now(),
	}
	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatalf("SaveAttempt failed: %v", err)
	}

	t.Logf("Step 3: Batch created and submitted to OTS - batchID=%s", batchID)

	// Step 6: Simulate BTC confirmation
	attempt.Status = types.BatchStatusConfirmed
	attempt.BTCBlockHeight = 800000
	attempt.BTCTxID = "btctx1234567890"
	attempt.BTCTimestamp = 1700000000
	attempt.ConfirmedAt = time.Now()

	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatalf("SaveAttempt update failed: %v", err)
	}

	t.Logf("Step 4: BTC confirmation received - btcBlock=%d", attempt.BTCBlockHeight)

	// Step 7: Build system transaction for anchoring
	contractAddr := common.HexToAddress("0x0000000000000000000000000000000000009000")
	builder := systx.NewBuilder(contractAddr)

	candidate := &types.CandidateBatch{
		BatchMeta:      batchMeta,
		EventRUIDs:     batchMeta.EventRUIDs,
		BTCBlockHeight: attempt.BTCBlockHeight,
		BTCTxID:        attempt.BTCTxID,
		BTCTimestamp:   attempt.BTCTimestamp,
	}

	coinbase := common.HexToAddress("0x1234567890123456789012345678901234567890")
	sysTx, err := builder.BuildSystemTx(candidate, coinbase, 0, 500000)
	if err != nil {
		t.Fatalf("BuildSystemTx failed: %v", err)
	}

	t.Logf("Step 5: System tx built - hash=%s", sysTx.Hash().Hex()[:18])

	// Step 8: Simulate anchoring (update status)
	attempt.Status = types.BatchStatusAnchored
	attempt.AnchorTxHash = sysTx.Hash()
	attempt.AnchorBlock = blockNumber + 100

	if err := store.SaveAttempt(attempt); err != nil {
		t.Fatalf("SaveAttempt anchor update failed: %v", err)
	}

	t.Logf("Step 6: Batch anchored on-chain - anchorTx=%s", sysTx.Hash().Hex()[:18])

	// Step 9: Verify RUID
	// Lookup batch by RUID
	var ruidBytes [32]byte
	copy(ruidBytes[:], ruid[:])

	foundMeta, err := store.GetBatchByRUID(ruidBytes)
	if err != nil {
		t.Fatalf("GetBatchByRUID failed: %v", err)
	}

	if foundMeta.BatchID != batchID {
		t.Fatalf("wrong batch found: expected %s, got %s", batchID, foundMeta.BatchID)
	}

	// Rebuild tree and verify proof
	verifyTree, err := merkle.BuildFromRUIDs(foundMeta.EventRUIDs)
	if err != nil {
		t.Fatalf("rebuild tree failed: %v", err)
	}

	proof, err := verifyTree.GetProof(ruid)
	if err != nil {
		t.Fatalf("GetProof failed: %v", err)
	}

	if !proof.VerifyRUID(ruid) {
		t.Fatal("proof verification failed")
	}

	if verifyTree.Root() != foundMeta.RootHash {
		t.Fatal("root hash mismatch")
	}

	// Get final attempt status
	finalAttempt, err := store.GetAttempt(batchID)
	if err != nil {
		t.Fatalf("GetAttempt failed: %v", err)
	}

	if finalAttempt.Status != types.BatchStatusAnchored {
		t.Errorf("expected status Anchored, got %s", finalAttempt.Status.String())
	}

	t.Logf("Step 7: RUID verified successfully!")
	t.Logf("=== Full Flow Complete ===")
	t.Logf("  RUID: %s", ruid.Hex())
	t.Logf("  Batch: %s", batchID)
	t.Logf("  Merkle Root: %s", rootHash.Hex())
	t.Logf("  BTC Block: %d", finalAttempt.BTCBlockHeight)
	t.Logf("  BTC TxID: %s", finalAttempt.BTCTxID)
	t.Logf("  Anchor Block: %d", finalAttempt.AnchorBlock)
	t.Logf("  Status: %s", finalAttempt.Status.String())

	_ = ctx // silence unused variable warning
}

// TestIntegration_ProofSerializationRoundtrip tests proof serialization
func TestIntegration_ProofSerializationRoundtrip(t *testing.T) {
	// Create tree with multiple leaves
	ruids := []common.Hash{
		common.HexToHash("0x1111"),
		common.HexToHash("0x2222"),
		common.HexToHash("0x3333"),
		common.HexToHash("0x4444"),
		common.HexToHash("0x5555"),
	}

	tree, err := merkle.BuildFromRUIDs(ruids)
	if err != nil {
		t.Fatalf("BuildFromRUIDs failed: %v", err)
	}

	for _, ruid := range ruids {
		// Get proof
		proof, err := tree.GetProof(ruid)
		if err != nil {
			t.Errorf("GetProof failed for %s: %v", ruid.Hex()[:10], err)
			continue
		}

		// Serialize
		serialized := proof.Serialize()

		// Deserialize
		restored, err := merkle.DeserializeProof(serialized)
		if err != nil {
			t.Errorf("DeserializeProof failed for %s: %v", ruid.Hex()[:10], err)
			continue
		}

		// Verify restored proof
		if !restored.VerifyRUID(ruid) {
			t.Errorf("restored proof verification failed for %s", ruid.Hex()[:10])
		}

		// Compare original and restored
		if proof.Leaf != restored.Leaf {
			t.Errorf("leaf mismatch for %s", ruid.Hex()[:10])
		}
		if proof.Root != restored.Root {
			t.Errorf("root mismatch for %s", ruid.Hex()[:10])
		}
		if len(proof.Path) != len(restored.Path) {
			t.Errorf("path length mismatch for %s", ruid.Hex()[:10])
		}
	}

	t.Logf("Proof serialization roundtrip passed for %d proofs", len(ruids))
}

// TestIntegration_BatchStatusTransitions tests valid status transitions
func TestIntegration_BatchStatusTransitions(t *testing.T) {
	memdb := memorydb.New()
	db := rawdb.NewDatabase(memdb)
	store := storage.NewStoreWithDB(db)

	batchID := "status-test-batch"
	meta := &types.BatchMeta{
		BatchID:    batchID,
		StartBlock: 1,
		EndBlock:   100,
		RootHash:   common.HexToHash("0x1234"),
		EventRUIDs: []common.Hash{common.HexToHash("0xaaaa")},
		CreatedAt:  time.Now(),
	}

	if err := store.SaveBatchMeta(meta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Test status transitions: Pending -> Submitted -> Confirmed -> Anchored
	transitions := []types.BatchStatus{
		types.BatchStatusPending,
		types.BatchStatusSubmitted,
		types.BatchStatusConfirmed,
		types.BatchStatusAnchored,
	}

	for i, status := range transitions {
		attempt := &types.Attempt{
			BatchID:       batchID,
			Status:        status,
			AttemptCount:  uint32(i + 1),
			LastAttemptAt: time.Now(),
		}

		if err := store.SaveAttempt(attempt); err != nil {
			t.Fatalf("SaveAttempt failed for status %s: %v", status.String(), err)
		}

		// Verify status was saved
		retrieved, err := store.GetAttempt(batchID)
		if err != nil {
			t.Fatalf("GetAttempt failed: %v", err)
		}

		if retrieved.Status != status {
			t.Errorf("status mismatch: expected %s, got %s", status.String(), retrieved.Status.String())
		}

		t.Logf("Status transition %d: %s", i+1, status.String())
	}
}

// TestIntegration_MultipleRUIDsInBatch tests handling of multiple RUIDs
func TestIntegration_MultipleRUIDsInBatch(t *testing.T) {
	memdb := memorydb.New()
	db := rawdb.NewDatabase(memdb)
	store := storage.NewStoreWithDB(db)

	// Create many RUIDs (simulating a busy day)
	numRUIDs := 100
	ruids := make([]common.Hash, numRUIDs)
	for i := 0; i < numRUIDs; i++ {
		ruids[i] = crypto.Keccak256Hash([]byte{byte(i), byte(i >> 8)})
	}

	// Build tree
	tree, err := merkle.BuildFromRUIDs(ruids)
	if err != nil {
		t.Fatalf("BuildFromRUIDs failed: %v", err)
	}

	// Create batch
	batchID := "large-batch"
	meta := &types.BatchMeta{
		BatchID:    batchID,
		StartBlock: 1,
		EndBlock:   1000,
		RootHash:   tree.Root(),
		OTSDigest:  tree.OTSDigest(),
		RUIDCount:  uint32(numRUIDs),
		EventRUIDs: ruids,
		CreatedAt:  time.Now(),
	}

	if err := store.SaveBatchMeta(meta); err != nil {
		t.Fatalf("SaveBatchMeta failed: %v", err)
	}

	// Verify all RUIDs can be found
	for i, ruid := range ruids {
		var ruidBytes [32]byte
		copy(ruidBytes[:], ruid[:])

		foundMeta, err := store.GetBatchByRUID(ruidBytes)
		if err != nil {
			t.Errorf("GetBatchByRUID failed for RUID %d: %v", i, err)
			continue
		}

		if foundMeta.BatchID != batchID {
			t.Errorf("wrong batch for RUID %d: expected %s, got %s", i, batchID, foundMeta.BatchID)
		}

		// Verify proof
		proof, err := tree.GetProof(ruid)
		if err != nil {
			t.Errorf("GetProof failed for RUID %d: %v", i, err)
			continue
		}

		if !proof.VerifyRUID(ruid) {
			t.Errorf("proof verification failed for RUID %d", i)
		}
	}

	t.Logf("Successfully verified %d RUIDs in batch", numRUIDs)
}
