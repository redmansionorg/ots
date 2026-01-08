// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// This file provides integration between OTS consensus and Parlia consensus engine.
// It manages OTS state transitions within the Parlia block processing flow.

package consensus

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/ots/systx"
)

// OTSConsensusManager manages OTS consensus state integration with Parlia
type OTSConsensusManager struct {
	mu sync.RWMutex

	// Core components
	db          ethdb.Database
	snapshots   *SnapshotManager
	engine      *TransitionEngine
	txBuilder   *systx.Builder

	// Configuration
	enabled         bool
	contractAddress common.Address
	systemTxGasLimit uint64

	// Chain access functions (set during initialization)
	getReceipts func(hash common.Hash, number uint64) types.Receipts
	getHeader   func(hash common.Hash, number uint64) *types.Header
	getHeaderByNumber func(number uint64) *types.Header

	// OTS client for background operations (optional)
	otsClient OTSClientInterface
}

// OTSClientInterface defines the interface for OTS client operations
type OTSClientInterface interface {
	// Stamp submits a hash to OTS calendar and returns the proof
	Stamp(digest common.Hash) ([]byte, [32]byte, error)
	// CheckConfirmation checks if a proof has BTC confirmation
	CheckConfirmation(digest [32]byte) (*BTCConfirmationResult, error)
}

// BTCConfirmationResult contains BTC confirmation information
type BTCConfirmationResult struct {
	Confirmed      bool
	BTCBlockHeight uint64
	BTCTxID        string
	BTCTimestamp   uint64
}

// OTSManagerConfig contains configuration for OTS consensus manager
type OTSManagerConfig struct {
	Enabled          bool
	ContractAddress  common.Address
	SystemTxGasLimit uint64
	DataDir          string
}

// NewOTSConsensusManager creates a new OTS consensus manager
func NewOTSConsensusManager(db ethdb.Database, config *OTSManagerConfig) (*OTSConsensusManager, error) {
	snapshots, err := NewSnapshotManager(db, config.Enabled)
	if err != nil {
		return nil, err
	}

	manager := &OTSConsensusManager{
		db:               db,
		snapshots:        snapshots,
		enabled:          config.Enabled,
		contractAddress:  config.ContractAddress,
		systemTxGasLimit: config.SystemTxGasLimit,
		txBuilder:        systx.NewBuilder(config.ContractAddress),
	}

	return manager, nil
}

// SetChainAccessors sets the chain access functions
func (m *OTSConsensusManager) SetChainAccessors(
	getReceipts func(hash common.Hash, number uint64) types.Receipts,
	getHeader func(hash common.Hash, number uint64) *types.Header,
	getHeaderByNumber func(number uint64) *types.Header,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.getReceipts = getReceipts
	m.getHeader = getHeader
	m.getHeaderByNumber = getHeaderByNumber

	// Create transition engine with chain accessors
	m.engine = NewTransitionEngine(m.snapshots, getReceipts, getHeader)
}

// SetOTSClient sets the OTS client for background operations
func (m *OTSConsensusManager) SetOTSClient(client OTSClientInterface) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.otsClient = client
}

// IsEnabled returns whether OTS is enabled
func (m *OTSConsensusManager) IsEnabled() bool {
	return m.enabled
}

// GetSnapshot returns the OTS snapshot for a given block
func (m *OTSConsensusManager) GetSnapshot(hash common.Hash) (*Snapshot, error) {
	return m.snapshots.GetSnapshot(hash)
}

// GetCurrentState returns the current OTS state for a block
func (m *OTSConsensusManager) GetCurrentState(hash common.Hash) (*OTSState, error) {
	snap, err := m.snapshots.GetSnapshot(hash)
	if err != nil {
		return nil, err
	}
	return snap.State, nil
}

// ProcessBlock processes a block and updates OTS state
// This should be called during block finalization
func (m *OTSConsensusManager) ProcessBlock(header *types.Header, parentHash common.Hash) (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.enabled || m.engine == nil {
		return nil, nil
	}

	// Get parent snapshot
	parentSnap, err := m.snapshots.GetSnapshot(parentHash)
	if err != nil {
		// If no parent snapshot, try to get genesis
		if header.Number.Uint64() == 1 {
			parentSnap = m.snapshots.GetGenesisSnapshot(parentHash)
		} else {
			return nil, err
		}
	}

	// Process the block
	return m.engine.ProcessBlock(header, parentSnap)
}

// GetSystemTransactions returns OTS system transactions to be included in a block
// This is called during block assembly by the block producer
func (m *OTSConsensusManager) GetSystemTransactions(
	header *types.Header,
	parentHash common.Hash,
	coinbase common.Address,
	getNonce func(addr common.Address) uint64,
) ([]*types.Transaction, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled {
		return nil, nil
	}

	// Get current OTS state
	snap, err := m.snapshots.GetSnapshot(parentHash)
	if err != nil {
		log.Debug("OTS: No snapshot for parent", "parentHash", parentHash, "err", err)
		return nil, nil
	}

	state := snap.State
	if state == nil || state.CurrentBatch == nil {
		return nil, nil
	}

	var txs []*types.Transaction
	nonce := getNonce(coinbase)

	switch state.CurrentBatch.Status {
	case BatchStatusTriggered:
		// Any validator with otsClient can submit to OTS calendar
		// The first one to successfully submit and include OTSSubmitted tx wins
		// Duplicate OTS submissions are harmless (same digest goes to same calendar entry)
		// Consensus layer validates that rootHash matches, regardless of who submitted
		if m.otsClient != nil {
			tx, err := m.tryBuildOTSSubmittedTx(state, coinbase, nonce)
			if err != nil {
				log.Debug("OTS: Failed to build otsSubmitted tx", "err", err)
			} else if tx != nil {
				txs = append(txs, tx)
			}
		}

	case BatchStatusSubmitted:
		// We need to check for BTC confirmation
		if m.otsClient != nil {
			tx, err := m.tryBuildOTSConfirmedTx(state, coinbase, nonce)
			if err != nil {
				log.Debug("OTS: Failed to build otsConfirmed tx", "err", err)
			} else if tx != nil {
				txs = append(txs, tx)
			}
		}

	case BatchStatusConfirmed:
		// We need to anchor on-chain
		tx, err := m.buildAnchorTx(state, coinbase, nonce)
		if err != nil {
			log.Debug("OTS: Failed to build anchor tx", "err", err)
		} else if tx != nil {
			txs = append(txs, tx)
		}
	}

	return txs, nil
}

// tryBuildOTSSubmittedTx attempts to submit to OTS and build the submission tx
func (m *OTSConsensusManager) tryBuildOTSSubmittedTx(state *OTSState, coinbase common.Address, nonce uint64) (*types.Transaction, error) {
	if state.CurrentBatch == nil || state.CurrentBatch.Status != BatchStatusTriggered {
		return nil, nil
	}

	// Submit to OTS calendar
	_, digest, err := m.otsClient.Stamp(state.CurrentBatch.RootHash)
	if err != nil {
		return nil, err
	}

	// Build otsSubmitted transaction
	params := &systx.OTSSubmittedParams{
		RootHash:  state.CurrentBatch.RootHash,
		OTSDigest: digest,
	}

	return m.txBuilder.BuildOTSSubmittedTx(params, coinbase, nonce, m.systemTxGasLimit)
}

// tryBuildOTSConfirmedTx checks for BTC confirmation and builds the confirmation tx
func (m *OTSConsensusManager) tryBuildOTSConfirmedTx(state *OTSState, coinbase common.Address, nonce uint64) (*types.Transaction, error) {
	if state.CurrentBatch == nil || state.CurrentBatch.Status != BatchStatusSubmitted {
		return nil, nil
	}

	// Check for BTC confirmation
	result, err := m.otsClient.CheckConfirmation(state.CurrentBatch.OTSDigest)
	if err != nil || !result.Confirmed {
		return nil, err
	}

	// Convert BTCTxID to bytes32
	var btcTxID [32]byte
	copy(btcTxID[:], []byte(result.BTCTxID))

	// Build otsConfirmed transaction
	params := &systx.OTSConfirmedParams{
		RootHash:       state.CurrentBatch.RootHash,
		BTCBlockHeight: result.BTCBlockHeight,
		BTCTxID:        btcTxID,
		BTCTimestamp:   result.BTCTimestamp,
	}

	return m.txBuilder.BuildOTSConfirmedTx(params, coinbase, nonce, m.systemTxGasLimit)
}

// buildAnchorTx builds the final anchor transaction
func (m *OTSConsensusManager) buildAnchorTx(state *OTSState, coinbase common.Address, nonce uint64) (*types.Transaction, error) {
	if state.CurrentBatch == nil || state.CurrentBatch.Status != BatchStatusConfirmed {
		return nil, nil
	}

	batch := state.CurrentBatch

	// Get RUIDs for the batch (we need to collect them from chain)
	ruids := m.collectRUIDsForBatch(batch.StartBlock, batch.EndBlock)

	// Build candidate batch for anchor
	candidate := &systx.CandidateBatch{
		RootHash:       batch.RootHash,
		StartBlock:     batch.StartBlock,
		EndBlock:       batch.EndBlock,
		EventRUIDs:     ruids,
		BTCBlockHeight: batch.BTCBlockHeight,
		BTCTxID:        batch.BTCTxID,
		BTCTimestamp:   batch.BTCTimestamp,
	}

	return m.txBuilder.BuildAnchorTx(candidate, coinbase, nonce, m.systemTxGasLimit)
}

// collectRUIDsForBatch collects RUIDs from chain events
func (m *OTSConsensusManager) collectRUIDsForBatch(startBlock, endBlock uint64) []common.Hash {
	var ruids []common.Hash

	for blockNum := startBlock; blockNum <= endBlock; blockNum++ {
		header := m.getHeaderByNumber(blockNum)
		if header == nil {
			continue
		}

		receipts := m.getReceipts(header.Hash(), blockNum)
		if receipts == nil {
			continue
		}

		// Extract RUIDs from CopyrightClaimed events
		for _, receipt := range receipts {
			for _, log := range receipt.Logs {
				if log.Address == common.HexToAddress(CopyrightRegistryAddress) {
					if len(log.Topics) >= 2 && log.Topics[0] == CopyrightClaimedEventSig {
						ruids = append(ruids, log.Topics[1]) // RUID is indexed
					}
				}
			}
		}
	}

	return ruids
}

// ValidateOTSSystemTx validates an OTS system transaction
func (m *OTSConsensusManager) ValidateOTSSystemTx(tx *types.Transaction, parentHash common.Hash) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if !m.enabled {
		return nil
	}

	// Get current state
	snap, err := m.snapshots.GetSnapshot(parentHash)
	if err != nil {
		return err
	}

	state := snap.State
	if state == nil {
		return ErrInvalidState
	}

	// Validate based on transaction type
	if systx.IsOTSSubmittedTx(tx) {
		return m.validateOTSSubmittedTx(tx, state)
	}

	if systx.IsOTSConfirmedTx(tx) {
		return m.validateOTSConfirmedTx(tx, state)
	}

	if systx.IsAnchorTx(tx) {
		return m.validateAnchorTx(tx, state)
	}

	return nil
}

// validateOTSSubmittedTx validates an otsSubmitted transaction
func (m *OTSConsensusManager) validateOTSSubmittedTx(tx *types.Transaction, state *OTSState) error {
	// Must have active batch in Triggered status
	if state.CurrentBatch == nil || state.CurrentBatch.Status != BatchStatusTriggered {
		return ErrInvalidTransition
	}

	// Decode and verify rootHash matches
	params, err := systx.DecodeOTSSubmittedTx(tx)
	if err != nil {
		return err
	}

	if params.RootHash != state.CurrentBatch.RootHash {
		return ErrInvalidState
	}

	return nil
}

// validateOTSConfirmedTx validates an otsConfirmed transaction
func (m *OTSConsensusManager) validateOTSConfirmedTx(tx *types.Transaction, state *OTSState) error {
	// Must have active batch in Submitted status
	if state.CurrentBatch == nil || state.CurrentBatch.Status != BatchStatusSubmitted {
		return ErrInvalidTransition
	}

	// Decode and verify rootHash matches
	params, err := systx.DecodeOTSConfirmedTx(tx)
	if err != nil {
		return err
	}

	if params.RootHash != state.CurrentBatch.RootHash {
		return ErrInvalidState
	}

	return nil
}

// validateAnchorTx validates an anchor transaction
func (m *OTSConsensusManager) validateAnchorTx(tx *types.Transaction, state *OTSState) error {
	// Must have active batch in Confirmed status
	if state.CurrentBatch == nil || state.CurrentBatch.Status != BatchStatusConfirmed {
		return ErrInvalidTransition
	}

	// Decode and verify rootHash matches
	decoded, err := systx.DecodeCalldata(tx.Data())
	if err != nil {
		return err
	}

	// Verify rootHash
	if decoded.RootHash != state.CurrentBatch.RootHash {
		return ErrInvalidState
	}

	// Verify block range
	if decoded.StartBlock != state.CurrentBatch.StartBlock || decoded.EndBlock != state.CurrentBatch.EndBlock {
		return ErrInvalidState
	}

	// Note: BTCBlockHeight is not included in anchor calldata, it's stored in consensus state
	// from the otsConfirmed transaction

	return nil
}

// RebuildFromChain rebuilds OTS state from chain data
func (m *OTSConsensusManager) RebuildFromChain(fromBlock, toBlock uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.engine == nil || m.getHeaderByNumber == nil {
		return ErrInvalidState
	}

	// Find nearest snapshot
	fromHash := common.Hash{}
	if fromBlock > 0 {
		header := m.getHeaderByNumber(fromBlock)
		if header != nil {
			fromHash = header.Hash()
		}
	}

	snap, err := m.snapshots.FindNearestSnapshot(fromBlock, func(num uint64) common.Hash {
		header := m.getHeaderByNumber(num)
		if header != nil {
			return header.Hash()
		}
		return common.Hash{}
	})
	if err != nil {
		// Start from genesis
		snap = m.snapshots.GetGenesisSnapshot(fromHash)
	}

	// Rebuild state
	newSnap, err := m.engine.RebuildState(snap, toBlock, m.getHeaderByNumber)
	if err != nil {
		return err
	}

	// Force store the final snapshot
	return m.snapshots.ForceStore(newSnap)
}

// GetBatchState returns the current batch state for RPC queries
func (m *OTSConsensusManager) GetBatchState(blockHash common.Hash) *BatchState {
	m.mu.RLock()
	defer m.mu.RUnlock()

	snap, err := m.snapshots.GetSnapshot(blockHash)
	if err != nil || snap.State == nil {
		return nil
	}
	return snap.State.CurrentBatch
}

// GetStats returns OTS consensus statistics
func (m *OTSConsensusManager) GetStats(blockHash common.Hash) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]interface{})
	stats["enabled"] = m.enabled

	snap, err := m.snapshots.GetSnapshot(blockHash)
	if err == nil && snap.State != nil {
		stats["lastAnchoredBlock"] = snap.State.LastAnchoredBlock
		if snap.State.CurrentBatch != nil {
			stats["currentBatch"] = map[string]interface{}{
				"startBlock":   snap.State.CurrentBatch.StartBlock,
				"endBlock":     snap.State.CurrentBatch.EndBlock,
				"status":       snap.State.CurrentBatch.Status.String(),
				"rootHash":     snap.State.CurrentBatch.RootHash.Hex(),
				"triggerBlock": snap.State.CurrentBatch.TriggerBlock,
			}
		}
	}

	cacheSize, cacheCapacity := m.snapshots.CacheStats()
	stats["cacheSize"] = cacheSize
	stats["cacheCapacity"] = cacheCapacity

	return stats
}
