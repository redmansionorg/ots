// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// This file implements OTS state transition rules.
// State transitions are deterministic and derived from block data.

package consensus

import (
	"bytes"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

const (
	// TriggerHourUTC is the hour (0-23) at which daily OTS batch is triggered
	TriggerHourUTC = 0

	// CopyrightRegistryAddress is the address of the CopyrightRegistry contract
	CopyrightRegistryAddress = "0x0000000000000000000000000000000000009000"
)

var (
	// CopyrightClaimedEventSig is the event signature for CopyrightClaimed
	// event CopyrightClaimed(bytes32 indexed ruid, bytes32 indexed puid, bytes32 indexed auid, address claimant)
	CopyrightClaimedEventSig = crypto.Keccak256Hash([]byte("CopyrightClaimed(bytes32,bytes32,bytes32,address)"))

	// OTS System Transaction selectors
	// otsSubmitted(bytes32 rootHash, bytes32 otsDigest)
	OTSSubmittedSelector = crypto.Keccak256([]byte("otsSubmitted(bytes32,bytes32)"))[:4]

	// otsConfirmed(bytes32 rootHash, uint64 btcBlockHeight, bytes32 btcTxID, uint64 btcTimestamp)
	OTSConfirmedSelector = crypto.Keccak256([]byte("otsConfirmed(bytes32,uint64,bytes32,uint64)"))[:4]

	// anchor(bytes32 rootHash, uint64 startBlock, uint64 endBlock, bytes32[] ruids, uint64 btcBlockHeight, bytes32 btcTxID, uint64 btcTimestamp)
	AnchorSelector = crypto.Keccak256([]byte("anchor(bytes32,uint64,uint64,bytes32[],uint64,bytes32,uint64)"))[:4]

	// Contract address
	copyrightRegistryAddr = common.HexToAddress(CopyrightRegistryAddress)
)

// TransitionEngine processes blocks and updates OTS state
type TransitionEngine struct {
	snapshots  *SnapshotManager
	getReceipts func(hash common.Hash, number uint64) types.Receipts
	getHeader   func(hash common.Hash, number uint64) *types.Header
}

// NewTransitionEngine creates a new transition engine
func NewTransitionEngine(snapshots *SnapshotManager, getReceipts func(common.Hash, uint64) types.Receipts, getHeader func(common.Hash, uint64) *types.Header) *TransitionEngine {
	return &TransitionEngine{
		snapshots:   snapshots,
		getReceipts: getReceipts,
		getHeader:   getHeader,
	}
}

// ProcessBlock applies a block to the OTS state and returns the new snapshot
func (te *TransitionEngine) ProcessBlock(header *types.Header, parentSnap *Snapshot) (*Snapshot, error) {
	// Copy parent state
	newState := parentSnap.State.Copy()

	// Skip if OTS is not enabled
	if !newState.Enabled {
		return NewSnapshot(header.Number.Uint64(), header.Hash(), newState), nil
	}

	// Get block transactions for system tx detection
	receipts := te.getReceipts(header.Hash(), header.Number.Uint64())

	// Apply state transitions based on current state and block content
	te.applyTransitions(newState, header, receipts)

	// Create new snapshot
	newSnap := NewSnapshot(header.Number.Uint64(), header.Hash(), newState)

	// Store snapshot
	if err := te.snapshots.StoreSnapshot(newSnap); err != nil {
		log.Warn("OTS: Failed to store snapshot", "number", header.Number, "err", err)
	}

	return newSnap, nil
}

// applyTransitions applies all applicable state transitions for a block
func (te *TransitionEngine) applyTransitions(state *OTSState, header *types.Header, receipts types.Receipts) {
	blockNumber := header.Number.Uint64()
	coinbase := header.Coinbase

	// Rule 1: Check for trigger condition (no active batch + crossing 00:00 UTC)
	if state.CanTrigger() && te.isTriggerBlock(header) {
		te.handleTrigger(state, header)
	}

	// Rule 2: Check for OTS submission system transaction
	if state.CurrentBatch != nil && state.CurrentBatch.Status == BatchStatusTriggered {
		if submission := te.extractOTSSubmission(header, receipts); submission != nil {
			if err := state.MarkSubmitted(submission.Digest, blockNumber, coinbase); err != nil {
				log.Debug("OTS: Failed to mark submitted", "err", err)
			} else {
				log.Info("OTS: Batch marked as submitted",
					"block", blockNumber,
					"digest", common.Bytes2Hex(submission.Digest[:]),
				)
			}
		}
	}

	// Rule 3: Check for BTC confirmation system transaction
	if state.CurrentBatch != nil && state.CurrentBatch.Status == BatchStatusSubmitted {
		if confirmation := te.extractBTCConfirmation(header, receipts); confirmation != nil {
			if err := state.MarkConfirmed(
				confirmation.BTCBlockHeight,
				confirmation.BTCTxID,
				confirmation.BTCTimestamp,
				blockNumber,
				coinbase,
			); err != nil {
				log.Debug("OTS: Failed to mark confirmed", "err", err)
			} else {
				log.Info("OTS: Batch marked as confirmed",
					"block", blockNumber,
					"btcBlock", confirmation.BTCBlockHeight,
					"btcTxID", confirmation.BTCTxID,
				)
			}
		}
	}

	// Rule 4: Check for anchor system transaction
	if state.CurrentBatch != nil && state.CurrentBatch.Status == BatchStatusConfirmed {
		if te.hasValidAnchorTx(header, receipts, state.CurrentBatch) {
			if err := state.MarkAnchored(blockNumber, coinbase); err != nil {
				log.Debug("OTS: Failed to mark anchored", "err", err)
			} else {
				log.Info("OTS: Batch anchored",
					"block", blockNumber,
					"lastAnchoredBlock", state.LastAnchoredBlock,
				)
			}
		}
	}
}

// isTriggerBlock checks if this block crosses the trigger hour (00:00 UTC)
func (te *TransitionEngine) isTriggerBlock(header *types.Header) bool {
	// Get parent header
	parentHeader := te.getHeader(header.ParentHash, header.Number.Uint64()-1)
	if parentHeader == nil {
		return false
	}

	// Convert timestamps to UTC time
	currentTime := time.Unix(int64(header.Time), 0).UTC()
	parentTime := time.Unix(int64(parentHeader.Time), 0).UTC()

	// Check if we crossed midnight (00:00 UTC)
	// This happens when:
	// 1. Parent was on previous day and current is on new day, OR
	// 2. Parent hour < TriggerHourUTC and current hour >= TriggerHourUTC
	currentDay := currentTime.YearDay()
	parentDay := parentTime.YearDay()
	currentYear := currentTime.Year()
	parentYear := parentTime.Year()

	// Year change or day change
	if currentYear > parentYear || currentDay > parentDay {
		// We crossed midnight
		return currentTime.Hour() >= TriggerHourUTC
	}

	// Same day: check if we crossed the trigger hour
	return parentTime.Hour() < TriggerHourUTC && currentTime.Hour() >= TriggerHourUTC
}

// handleTrigger handles the trigger of a new OTS batch
func (te *TransitionEngine) handleTrigger(state *OTSState, header *types.Header) {
	blockNumber := header.Number.Uint64()

	// Calculate block range: from last anchored + 1 to previous block
	startBlock := state.LastAnchoredBlock + 1
	endBlock := blockNumber - 1

	// Skip if no blocks to process
	if endBlock < startBlock {
		log.Debug("OTS: No blocks to process for trigger", "start", startBlock, "end", endBlock)
		return
	}

	// Calculate root hash from events in the block range
	rootHash := te.calculateRootHash(startBlock, endBlock)

	// Trigger the batch
	if err := state.Trigger(startBlock, endBlock, blockNumber, header.Coinbase, rootHash); err != nil {
		log.Debug("OTS: Failed to trigger batch", "err", err)
		return
	}

	log.Info("OTS: Batch triggered",
		"startBlock", startBlock,
		"endBlock", endBlock,
		"triggerBlock", blockNumber,
		"rootHash", rootHash.Hex(),
	)
}

// calculateRootHash calculates the Merkle root from CopyrightClaimed events
func (te *TransitionEngine) calculateRootHash(startBlock, endBlock uint64) common.Hash {
	var ruids []common.Hash

	// Collect all RUIDs from the block range
	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		// Get block header to get the hash
		// Note: We need to iterate through headers to get receipts
		// In a real implementation, this would use the chain's GetBlockByNumber
		// For now, we'll use a simplified approach
		ruidsFromBlock := te.getRUIDsFromBlock(blockNum)
		ruids = append(ruids, ruidsFromBlock...)
	}

	if len(ruids) == 0 {
		return common.Hash{}
	}

	// Sort RUIDs for deterministic ordering
	sort.Slice(ruids, func(i, j int) bool {
		return bytes.Compare(ruids[i][:], ruids[j][:]) < 0
	})

	// Build Merkle tree
	return buildMerkleRoot(ruids)
}

// getRUIDsFromBlock extracts RUIDs from CopyrightClaimed events in a block
func (te *TransitionEngine) getRUIDsFromBlock(blockNum uint64) []common.Hash {
	// This is a placeholder - in real implementation, we need access to
	// block hash to get receipts. The actual implementation will be
	// provided when integrating with the chain.
	return nil
}

// OTSSubmission represents a parsed OTS submission
type OTSSubmission struct {
	RootHash common.Hash
	Digest   [32]byte
}

// extractOTSSubmission extracts OTS submission info from block transactions
func (te *TransitionEngine) extractOTSSubmission(header *types.Header, receipts types.Receipts) *OTSSubmission {
	// Look for otsSubmitted system transaction in receipts
	for _, receipt := range receipts {
		if receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}
		// Check transaction logs for OTS submission event
		for _, log := range receipt.Logs {
			if log.Address == copyrightRegistryAddr {
				// Parse OTSSubmitted event if present
				submission := te.parseOTSSubmittedLog(log)
				if submission != nil {
					return submission
				}
			}
		}
	}
	return nil
}

// parseOTSSubmittedLog parses an OTSSubmitted event log
func (te *TransitionEngine) parseOTSSubmittedLog(log *types.Log) *OTSSubmission {
	// Event: OTSSubmitted(bytes32 indexed rootHash, bytes32 otsDigest)
	// Topics[0] = event signature
	// Topics[1] = rootHash (indexed)
	// Data = otsDigest

	otsSubmittedSig := crypto.Keccak256Hash([]byte("OTSSubmitted(bytes32,bytes32)"))

	if len(log.Topics) < 2 || log.Topics[0] != otsSubmittedSig {
		return nil
	}

	if len(log.Data) < 32 {
		return nil
	}

	var digest [32]byte
	copy(digest[:], log.Data[:32])

	return &OTSSubmission{
		RootHash: log.Topics[1],
		Digest:   digest,
	}
}

// BTCConfirmation represents a parsed BTC confirmation
type BTCConfirmation struct {
	RootHash       common.Hash
	BTCBlockHeight uint64
	BTCTxID        string
	BTCTimestamp   uint64
}

// extractBTCConfirmation extracts BTC confirmation info from block transactions
func (te *TransitionEngine) extractBTCConfirmation(header *types.Header, receipts types.Receipts) *BTCConfirmation {
	// Look for otsConfirmed system transaction in receipts
	for _, receipt := range receipts {
		if receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}
		for _, log := range receipt.Logs {
			if log.Address == copyrightRegistryAddr {
				confirmation := te.parseOTSConfirmedLog(log)
				if confirmation != nil {
					return confirmation
				}
			}
		}
	}
	return nil
}

// parseOTSConfirmedLog parses an OTSConfirmed event log
func (te *TransitionEngine) parseOTSConfirmedLog(log *types.Log) *BTCConfirmation {
	// Event: OTSConfirmed(bytes32 indexed rootHash, uint64 btcBlockHeight, bytes32 btcTxID, uint64 btcTimestamp)
	// Topics[0] = event signature
	// Topics[1] = rootHash (indexed)
	// Data = btcBlockHeight (32 bytes) + btcTxID (32 bytes) + btcTimestamp (32 bytes)

	otsConfirmedSig := crypto.Keccak256Hash([]byte("OTSConfirmed(bytes32,uint64,bytes32,uint64)"))

	if len(log.Topics) < 2 || log.Topics[0] != otsConfirmedSig {
		return nil
	}

	if len(log.Data) < 96 {
		return nil
	}

	// Parse data (ABI encoded)
	btcBlockHeight := common.BytesToHash(log.Data[0:32]).Big().Uint64()
	btcTxID := common.Bytes2Hex(log.Data[32:64])
	btcTimestamp := common.BytesToHash(log.Data[64:96]).Big().Uint64()

	return &BTCConfirmation{
		RootHash:       log.Topics[1],
		BTCBlockHeight: btcBlockHeight,
		BTCTxID:        btcTxID,
		BTCTimestamp:   btcTimestamp,
	}
}

// hasValidAnchorTx checks if the block contains a valid anchor transaction
func (te *TransitionEngine) hasValidAnchorTx(header *types.Header, receipts types.Receipts, batch *BatchState) bool {
	// Look for anchor system transaction that matches current batch
	for _, receipt := range receipts {
		if receipt.Status != types.ReceiptStatusSuccessful {
			continue
		}
		for _, log := range receipt.Logs {
			if log.Address == copyrightRegistryAddr {
				if te.isValidAnchorLog(log, batch) {
					return true
				}
			}
		}
	}
	return false
}

// isValidAnchorLog checks if a log is a valid Anchored event for the batch
func (te *TransitionEngine) isValidAnchorLog(log *types.Log, batch *BatchState) bool {
	// Event: Anchored(bytes32 indexed rootHash, uint64 startBlock, uint64 endBlock, uint64 btcBlockHeight)
	// Topics[0] = event signature
	// Topics[1] = rootHash (indexed)

	anchoredSig := crypto.Keccak256Hash([]byte("Anchored(bytes32,uint64,uint64,uint64)"))

	if len(log.Topics) < 2 || log.Topics[0] != anchoredSig {
		return false
	}

	// Verify rootHash matches
	if log.Topics[1] != batch.RootHash {
		return false
	}

	return true
}

// buildMerkleRoot constructs a Merkle root from a list of RUIDs
// Uses Bitcoin-style duplication for odd number of nodes
func buildMerkleRoot(ruids []common.Hash) common.Hash {
	if len(ruids) == 0 {
		return common.Hash{}
	}

	// Build leaf hashes: leafHash = keccak256(ruid)
	leaves := make([]common.Hash, len(ruids))
	for i, ruid := range ruids {
		leaves[i] = crypto.Keccak256Hash(ruid[:])
	}

	// Build tree layers (Bitcoin-style: duplicate last node if odd count)
	currentLayer := leaves
	for len(currentLayer) > 1 {
		// If odd number of nodes, duplicate the last one
		if len(currentLayer)%2 == 1 {
			currentLayer = append(currentLayer, currentLayer[len(currentLayer)-1])
		}

		nextLayer := make([]common.Hash, len(currentLayer)/2)
		for i := 0; i < len(currentLayer); i += 2 {
			// Combine two nodes: sort them first for deterministic ordering
			left, right := currentLayer[i], currentLayer[i+1]
			if bytes.Compare(left[:], right[:]) > 0 {
				left, right = right, left
			}
			combined := append(left[:], right[:]...)
			nextLayer[i/2] = crypto.Keccak256Hash(combined)
		}
		currentLayer = nextLayer
	}

	return currentLayer[0]
}

// RebuildState rebuilds OTS state from chain data starting from a snapshot
func (te *TransitionEngine) RebuildState(fromSnap *Snapshot, targetNumber uint64, getHeader func(uint64) *types.Header) (*Snapshot, error) {
	currentSnap := fromSnap.Copy()

	for blockNum := fromSnap.Number + 1; blockNum <= targetNumber; blockNum++ {
		header := getHeader(blockNum)
		if header == nil {
			return nil, ErrSnapshotNotFound
		}

		newSnap, err := te.ProcessBlock(header, currentSnap)
		if err != nil {
			return nil, err
		}
		currentSnap = newSnap
	}

	return currentSnap, nil
}
