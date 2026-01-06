// Copyright 2024 The RMC Authors
// This file is part of the RMC library.

package event

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	otstypes "github.com/ethereum/go-ethereum/ots/types"
)

func TestCopyrightClaimedEventSig(t *testing.T) {
	// Verify the event signature matches the new contract:
	// event CopyrightClaimed(bytes32 indexed ruid, address indexed claimant, uint64 submitBlock);
	expectedSig := crypto.Keccak256Hash([]byte("CopyrightClaimed(bytes32,address,uint64)"))

	if CopyrightClaimedEventSig != expectedSig {
		t.Errorf("CopyrightClaimedEventSig = %s, want %s",
			CopyrightClaimedEventSig.Hex(), expectedSig.Hex())
	}
}

func TestParseLog(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")

	// Create mock collector (we only test parseLog)
	c := &Collector{
		contractAddress: contractAddr,
	}

	// Create a mock log entry
	// Topics:
	//   [0] = event signature
	//   [1] = ruid (indexed bytes32)
	//   [2] = claimant (indexed address, padded to 32 bytes)
	// Data:
	//   submitBlock (uint64, padded to 32 bytes)

	ruid := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	claimant := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")

	// Pad address to 32 bytes for topic
	var claimantTopic common.Hash
	copy(claimantTopic[12:], claimant.Bytes())

	logEntry := &types.Log{
		Address:     contractAddr,
		Topics:      []common.Hash{CopyrightClaimedEventSig, ruid, claimantTopic},
		Data:        make([]byte, 32), // submitBlock = 0 for simplicity
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
		TxIndex:     5,
		Index:       3,
		BlockHash:   common.HexToHash("0xdef"),
	}

	event, err := c.parseLog(logEntry)
	if err != nil {
		t.Fatalf("parseLog failed: %v", err)
	}

	// Verify parsed event
	if event.RUID != ruid {
		t.Errorf("RUID = %s, want %s", event.RUID.Hex(), ruid.Hex())
	}

	if event.SortKey.BlockNumber != 100 {
		t.Errorf("BlockNumber = %d, want 100", event.SortKey.BlockNumber)
	}

	if event.SortKey.TxIndex != 5 {
		t.Errorf("TxIndex = %d, want 5", event.SortKey.TxIndex)
	}

	if event.SortKey.LogIndex != 3 {
		t.Errorf("LogIndex = %d, want 3", event.SortKey.LogIndex)
	}

	if event.TxHash != common.HexToHash("0xabc") {
		t.Errorf("TxHash mismatch")
	}

	if event.BlockHash != common.HexToHash("0xdef") {
		t.Errorf("BlockHash mismatch")
	}
}

func TestParseLog_InsufficientTopics(t *testing.T) {
	c := &Collector{}

	// Log with only 2 topics (need at least 3)
	logEntry := &types.Log{
		Topics: []common.Hash{CopyrightClaimedEventSig, common.HexToHash("0x1")},
	}

	_, err := c.parseLog(logEntry)
	if err == nil {
		t.Error("expected error for insufficient topics")
	}
}

func TestParseFullEvent(t *testing.T) {
	contractAddr := common.HexToAddress("0x9000")
	c := &Collector{
		contractAddress: contractAddr,
	}

	ruid := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	claimant := common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12")

	// Pad address to 32 bytes for topic
	var claimantTopic common.Hash
	copy(claimantTopic[12:], claimant.Bytes())

	// submitBlock = 12345 as uint64 in 32 bytes
	submitBlockData := make([]byte, 32)
	submitBlockData[31] = 0x39 // 12345 = 0x3039
	submitBlockData[30] = 0x30

	logEntry := &types.Log{
		Address:     contractAddr,
		Topics:      []common.Hash{CopyrightClaimedEventSig, ruid, claimantTopic},
		Data:        submitBlockData,
		BlockNumber: 100,
		TxHash:      common.HexToHash("0xabc"),
		TxIndex:     5,
		Index:       3,
		BlockHash:   common.HexToHash("0xdef"),
	}

	event, err := c.ParseFullEvent(logEntry)
	if err != nil {
		t.Fatalf("ParseFullEvent failed: %v", err)
	}

	if event.RUID != ruid {
		t.Errorf("RUID mismatch")
	}

	if event.Claimant != claimant {
		t.Errorf("Claimant = %s, want %s", event.Claimant.Hex(), claimant.Hex())
	}

	if event.BlockNumber != 100 {
		t.Errorf("BlockNumber = %d, want 100", event.BlockNumber)
	}

	if event.TxIndex != 5 {
		t.Errorf("TxIndex = %d, want 5", event.TxIndex)
	}

	if event.LogIndex != 3 {
		t.Errorf("LogIndex = %d, want 3", event.LogIndex)
	}

	// PUID and AUID should be zero (not available in claim event)
	if event.PUID != (common.Hash{}) {
		t.Error("PUID should be zero")
	}

	if event.AUID != (common.Hash{}) {
		t.Error("AUID should be zero")
	}

	// Timestamp should be submitBlock value
	if event.Timestamp != 12345 {
		t.Errorf("Timestamp = %d, want 12345", event.Timestamp)
	}
}

func TestSortEventsByKey(t *testing.T) {
	// Create unsorted events
	events := []struct {
		block    uint64
		txIndex  uint32
		logIndex uint32
	}{
		{100, 5, 2},
		{100, 3, 1},
		{99, 10, 5},
		{100, 3, 0},
		{101, 0, 0},
	}

	var eventList []otstypes.EventForMerkle
	for _, e := range events {
		eventList = append(eventList, otstypes.EventForMerkle{
			RUID: common.Hash{},
			SortKey: otstypes.SortKey{
				BlockNumber: e.block,
				TxIndex:     e.txIndex,
				LogIndex:    e.logIndex,
			},
		})
	}

	// Sort
	sortEventsByKey(eventList)

	// Verify order: (99,10,5), (100,3,0), (100,3,1), (100,5,2), (101,0,0)
	expected := []struct {
		block    uint64
		txIndex  uint32
		logIndex uint32
	}{
		{99, 10, 5},
		{100, 3, 0},
		{100, 3, 1},
		{100, 5, 2},
		{101, 0, 0},
	}

	for i, e := range expected {
		if eventList[i].SortKey.BlockNumber != e.block ||
			eventList[i].SortKey.TxIndex != e.txIndex ||
			eventList[i].SortKey.LogIndex != e.logIndex {
			t.Errorf("event[%d] = (%d,%d,%d), want (%d,%d,%d)",
				i,
				eventList[i].SortKey.BlockNumber,
				eventList[i].SortKey.TxIndex,
				eventList[i].SortKey.LogIndex,
				e.block, e.txIndex, e.logIndex)
		}
	}
}
