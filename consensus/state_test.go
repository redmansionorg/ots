// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package consensus

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestNewOTSState(t *testing.T) {
	state := NewOTSState(true)
	if !state.Enabled {
		t.Error("Expected OTS to be enabled")
	}
	if state.LastAnchoredBlock != 0 {
		t.Error("Expected LastAnchoredBlock to be 0")
	}
	if state.CurrentBatch != nil {
		t.Error("Expected CurrentBatch to be nil")
	}
}

func TestOTSState_Copy(t *testing.T) {
	state := NewOTSState(true)
	state.LastAnchoredBlock = 100

	cpy := state.Copy()
	if cpy.LastAnchoredBlock != 100 {
		t.Error("Copy should have same LastAnchoredBlock")
	}

	// Modify original
	state.LastAnchoredBlock = 200
	if cpy.LastAnchoredBlock != 100 {
		t.Error("Copy should be independent from original")
	}
}

func TestOTSState_CanTrigger(t *testing.T) {
	// Disabled state cannot trigger
	state := NewOTSState(false)
	if state.CanTrigger() {
		t.Error("Disabled state should not be able to trigger")
	}

	// Enabled state with no batch can trigger
	state = NewOTSState(true)
	if !state.CanTrigger() {
		t.Error("Enabled state with no batch should be able to trigger")
	}

	// State with active batch cannot trigger
	state.CurrentBatch = &BatchState{Status: BatchStatusTriggered}
	if state.CanTrigger() {
		t.Error("State with active batch should not be able to trigger")
	}
}

func TestOTSState_Trigger(t *testing.T) {
	state := NewOTSState(true)
	triggerNode := common.HexToAddress("0x1234567890123456789012345678901234567890")
	rootHash := common.HexToHash("0xabcd")

	err := state.Trigger(1, 100, 101, triggerNode, rootHash)
	if err != nil {
		t.Fatalf("Trigger failed: %v", err)
	}

	if state.CurrentBatch == nil {
		t.Fatal("CurrentBatch should not be nil after trigger")
	}
	if state.CurrentBatch.Status != BatchStatusTriggered {
		t.Errorf("Expected status Triggered, got %v", state.CurrentBatch.Status)
	}
	if state.CurrentBatch.StartBlock != 1 {
		t.Errorf("Expected StartBlock 1, got %d", state.CurrentBatch.StartBlock)
	}
	if state.CurrentBatch.EndBlock != 100 {
		t.Errorf("Expected EndBlock 100, got %d", state.CurrentBatch.EndBlock)
	}
	if state.CurrentBatch.TriggerBlock != 101 {
		t.Errorf("Expected TriggerBlock 101, got %d", state.CurrentBatch.TriggerBlock)
	}
	if state.CurrentBatch.TriggerNode != triggerNode {
		t.Errorf("TriggerNode mismatch")
	}
	if state.CurrentBatch.RootHash != rootHash {
		t.Errorf("RootHash mismatch")
	}

	// Cannot trigger again
	err = state.Trigger(101, 200, 201, triggerNode, rootHash)
	if err != ErrAlreadyTriggered {
		t.Errorf("Expected ErrAlreadyTriggered, got %v", err)
	}
}

func TestOTSState_MarkSubmitted(t *testing.T) {
	state := NewOTSState(true)
	triggerNode := common.HexToAddress("0x1234")
	submitter := common.HexToAddress("0x5678")
	rootHash := common.HexToHash("0xabcd")
	digest := [32]byte{1, 2, 3, 4}

	// Cannot mark submitted without triggering first
	err := state.MarkSubmitted(digest, 102, submitter)
	if err != ErrNotTriggered {
		t.Errorf("Expected ErrNotTriggered, got %v", err)
	}

	// Trigger first
	_ = state.Trigger(1, 100, 101, triggerNode, rootHash)

	// Now mark submitted
	err = state.MarkSubmitted(digest, 102, submitter)
	if err != nil {
		t.Fatalf("MarkSubmitted failed: %v", err)
	}

	if state.CurrentBatch.Status != BatchStatusSubmitted {
		t.Errorf("Expected status Submitted, got %v", state.CurrentBatch.Status)
	}
	if state.CurrentBatch.OTSDigest != digest {
		t.Error("OTSDigest mismatch")
	}
	if state.CurrentBatch.SubmittedAt != 102 {
		t.Errorf("Expected SubmittedAt 102, got %d", state.CurrentBatch.SubmittedAt)
	}
	if state.CurrentBatch.SubmittedBy != submitter {
		t.Error("SubmittedBy mismatch")
	}
}

func TestOTSState_MarkConfirmed(t *testing.T) {
	state := NewOTSState(true)
	triggerNode := common.HexToAddress("0x1234")
	submitter := common.HexToAddress("0x5678")
	confirmer := common.HexToAddress("0x9abc")
	rootHash := common.HexToHash("0xabcd")
	digest := [32]byte{1, 2, 3, 4}

	// Cannot confirm without submitting first
	err := state.MarkConfirmed(800000, "btctx123", 1234567890, 103, confirmer)
	if err != ErrNotSubmitted {
		t.Errorf("Expected ErrNotSubmitted, got %v", err)
	}

	// Setup: trigger and submit
	_ = state.Trigger(1, 100, 101, triggerNode, rootHash)
	_ = state.MarkSubmitted(digest, 102, submitter)

	// Now confirm
	err = state.MarkConfirmed(800000, "btctx123", 1234567890, 103, confirmer)
	if err != nil {
		t.Fatalf("MarkConfirmed failed: %v", err)
	}

	if state.CurrentBatch.Status != BatchStatusConfirmed {
		t.Errorf("Expected status Confirmed, got %v", state.CurrentBatch.Status)
	}
	if state.CurrentBatch.BTCBlockHeight != 800000 {
		t.Errorf("Expected BTCBlockHeight 800000, got %d", state.CurrentBatch.BTCBlockHeight)
	}
	if state.CurrentBatch.BTCTxID != "btctx123" {
		t.Errorf("Expected BTCTxID 'btctx123', got %s", state.CurrentBatch.BTCTxID)
	}
	if state.CurrentBatch.BTCTimestamp != 1234567890 {
		t.Errorf("Expected BTCTimestamp 1234567890, got %d", state.CurrentBatch.BTCTimestamp)
	}
	if state.CurrentBatch.ConfirmedAt != 103 {
		t.Errorf("Expected ConfirmedAt 103, got %d", state.CurrentBatch.ConfirmedAt)
	}
}

func TestOTSState_MarkAnchored(t *testing.T) {
	state := NewOTSState(true)
	triggerNode := common.HexToAddress("0x1234")
	submitter := common.HexToAddress("0x5678")
	confirmer := common.HexToAddress("0x9abc")
	anchorer := common.HexToAddress("0xdef0")
	rootHash := common.HexToHash("0xabcd")
	digest := [32]byte{1, 2, 3, 4}

	// Cannot anchor without confirming first
	err := state.MarkAnchored(104, anchorer)
	if err != ErrNotConfirmed {
		t.Errorf("Expected ErrNotConfirmed, got %v", err)
	}

	// Setup: trigger, submit, confirm
	_ = state.Trigger(1, 100, 101, triggerNode, rootHash)
	_ = state.MarkSubmitted(digest, 102, submitter)
	_ = state.MarkConfirmed(800000, "btctx123", 1234567890, 103, confirmer)

	// Now anchor
	err = state.MarkAnchored(104, anchorer)
	if err != nil {
		t.Fatalf("MarkAnchored failed: %v", err)
	}

	// After anchoring, CurrentBatch should be nil
	if state.CurrentBatch != nil {
		t.Error("CurrentBatch should be nil after anchoring")
	}
	// LastAnchoredBlock should be updated
	if state.LastAnchoredBlock != 100 {
		t.Errorf("Expected LastAnchoredBlock 100, got %d", state.LastAnchoredBlock)
	}
}

func TestBatchStatus_String(t *testing.T) {
	tests := []struct {
		status   BatchStatus
		expected string
	}{
		{BatchStatusNone, "none"},
		{BatchStatusTriggered, "triggered"},
		{BatchStatusSubmitted, "submitted"},
		{BatchStatusConfirmed, "confirmed"},
		{BatchStatusAnchored, "anchored"},
		{BatchStatus(99), "unknown"},
	}

	for _, tt := range tests {
		if got := tt.status.String(); got != tt.expected {
			t.Errorf("BatchStatus(%d).String() = %s, want %s", tt.status, got, tt.expected)
		}
	}
}

func TestBatchStatus_CanTransitionTo(t *testing.T) {
	tests := []struct {
		from     BatchStatus
		to       BatchStatus
		expected bool
	}{
		{BatchStatusNone, BatchStatusTriggered, true},
		{BatchStatusNone, BatchStatusSubmitted, false},
		{BatchStatusTriggered, BatchStatusSubmitted, true},
		{BatchStatusTriggered, BatchStatusConfirmed, false},
		{BatchStatusSubmitted, BatchStatusConfirmed, true},
		{BatchStatusSubmitted, BatchStatusAnchored, false},
		{BatchStatusConfirmed, BatchStatusAnchored, true},
		{BatchStatusConfirmed, BatchStatusNone, true}, // timeout/reset case
		{BatchStatusAnchored, BatchStatusNone, true},
		{BatchStatusAnchored, BatchStatusTriggered, false},
	}

	for _, tt := range tests {
		if got := tt.from.CanTransitionTo(tt.to); got != tt.expected {
			t.Errorf("%s.CanTransitionTo(%s) = %v, want %v", tt.from, tt.to, got, tt.expected)
		}
	}
}

func TestOTSState_EncodeDecode(t *testing.T) {
	state := NewOTSState(true)
	state.LastAnchoredBlock = 12345
	triggerNode := common.HexToAddress("0x1234567890123456789012345678901234567890")
	rootHash := common.HexToHash("0xabcd")
	_ = state.Trigger(1, 100, 101, triggerNode, rootHash)

	// Encode
	data, err := state.Encode()
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Decode
	decoded, err := DecodeOTSState(data)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	// Verify
	if decoded.Enabled != state.Enabled {
		t.Error("Enabled mismatch")
	}
	if decoded.LastAnchoredBlock != state.LastAnchoredBlock {
		t.Error("LastAnchoredBlock mismatch")
	}
	if decoded.CurrentBatch == nil {
		t.Fatal("CurrentBatch should not be nil")
	}
	if decoded.CurrentBatch.StartBlock != state.CurrentBatch.StartBlock {
		t.Error("StartBlock mismatch")
	}
	if decoded.CurrentBatch.RootHash != state.CurrentBatch.RootHash {
		t.Error("RootHash mismatch")
	}
}

func TestOTSState_Hash(t *testing.T) {
	state1 := NewOTSState(true)
	state1.LastAnchoredBlock = 100

	state2 := NewOTSState(true)
	state2.LastAnchoredBlock = 100

	state3 := NewOTSState(true)
	state3.LastAnchoredBlock = 200

	// Same state should have same hash
	if state1.Hash() != state2.Hash() {
		t.Error("Same state should have same hash")
	}

	// Different state should have different hash
	if state1.Hash() == state3.Hash() {
		t.Error("Different state should have different hash")
	}
}

func TestFullStateTransitionCycle(t *testing.T) {
	state := NewOTSState(true)
	triggerNode := common.HexToAddress("0x1111")
	submitter := common.HexToAddress("0x2222")
	confirmer := common.HexToAddress("0x3333")
	anchorer := common.HexToAddress("0x4444")
	rootHash := common.HexToHash("0xdeadbeef")
	digest := [32]byte{0xaa, 0xbb, 0xcc, 0xdd}

	// Initial state
	if state.HasActiveBatch() {
		t.Error("Should not have active batch initially")
	}

	// Cycle 1: Trigger -> Submit -> Confirm -> Anchor
	if err := state.Trigger(1, 1000, 1001, triggerNode, rootHash); err != nil {
		t.Fatalf("Trigger failed: %v", err)
	}
	if !state.HasActiveBatch() {
		t.Error("Should have active batch after trigger")
	}

	if err := state.MarkSubmitted(digest, 1002, submitter); err != nil {
		t.Fatalf("MarkSubmitted failed: %v", err)
	}

	if err := state.MarkConfirmed(800001, "tx1", 1700000000, 1003, confirmer); err != nil {
		t.Fatalf("MarkConfirmed failed: %v", err)
	}

	if err := state.MarkAnchored(1004, anchorer); err != nil {
		t.Fatalf("MarkAnchored failed: %v", err)
	}

	// After first cycle
	if state.LastAnchoredBlock != 1000 {
		t.Errorf("Expected LastAnchoredBlock 1000, got %d", state.LastAnchoredBlock)
	}
	if state.HasActiveBatch() {
		t.Error("Should not have active batch after anchoring")
	}
	if !state.CanTrigger() {
		t.Error("Should be able to trigger again")
	}

	// Cycle 2: Start next batch
	rootHash2 := common.HexToHash("0xcafebabe")
	if err := state.Trigger(1001, 2000, 2001, triggerNode, rootHash2); err != nil {
		t.Fatalf("Second trigger failed: %v", err)
	}

	if state.CurrentBatch.StartBlock != 1001 {
		t.Errorf("Second batch StartBlock should be 1001, got %d", state.CurrentBatch.StartBlock)
	}
}
