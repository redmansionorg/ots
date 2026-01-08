// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// Package consensus implements the OTS consensus state management.
// OTS state is maintained as part of the blockchain consensus, allowing
// all nodes to independently verify and track OTS batch progress.

package consensus

import (
	"encoding/json"
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

var (
	ErrInvalidState      = errors.New("invalid OTS state")
	ErrBatchNotFound     = errors.New("batch not found in OTS state")
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrAlreadyTriggered  = errors.New("batch already triggered")
	ErrNotTriggered      = errors.New("batch not yet triggered")
	ErrNotSubmitted      = errors.New("batch not yet submitted")
	ErrNotConfirmed      = errors.New("batch not yet confirmed")
	ErrAlreadyAnchored   = errors.New("batch already anchored")
)

// BatchStatus represents the status of an OTS batch in consensus
type BatchStatus uint8

const (
	// BatchStatusNone indicates no active batch
	BatchStatusNone BatchStatus = 0
	// BatchStatusTriggered indicates batch is triggered, waiting for OTS submission
	BatchStatusTriggered BatchStatus = 1
	// BatchStatusSubmitted indicates batch is submitted to OTS calendar, waiting for BTC confirmation
	BatchStatusSubmitted BatchStatus = 2
	// BatchStatusConfirmed indicates batch is confirmed on Bitcoin, waiting for on-chain anchor
	BatchStatusConfirmed BatchStatus = 3
	// BatchStatusAnchored indicates batch is anchored on-chain (terminal state)
	BatchStatusAnchored BatchStatus = 4
)

func (s BatchStatus) String() string {
	switch s {
	case BatchStatusNone:
		return "none"
	case BatchStatusTriggered:
		return "triggered"
	case BatchStatusSubmitted:
		return "submitted"
	case BatchStatusConfirmed:
		return "confirmed"
	case BatchStatusAnchored:
		return "anchored"
	default:
		return "unknown"
	}
}

// CanTransitionTo checks if a transition from current status to target status is valid
func (s BatchStatus) CanTransitionTo(target BatchStatus) bool {
	switch s {
	case BatchStatusNone:
		return target == BatchStatusTriggered
	case BatchStatusTriggered:
		return target == BatchStatusSubmitted
	case BatchStatusSubmitted:
		return target == BatchStatusConfirmed
	case BatchStatusConfirmed:
		return target == BatchStatusAnchored || target == BatchStatusNone
	case BatchStatusAnchored:
		return target == BatchStatusNone
	default:
		return false
	}
}

// OTSState represents the OTS consensus state
// This state is deterministically derived from chain events and system transactions
type OTSState struct {
	// Enabled indicates if OTS is enabled for this chain
	Enabled bool `json:"enabled"`

	// LastAnchoredBlock is the end block of the last successfully anchored batch
	LastAnchoredBlock uint64 `json:"lastAnchoredBlock"`

	// CurrentBatch is the batch currently being processed (nil if none)
	CurrentBatch *BatchState `json:"currentBatch,omitempty"`
}

// BatchState represents the state of a single OTS batch
type BatchState struct {
	// Batch identification and range
	StartBlock uint64      `json:"startBlock"`
	EndBlock   uint64      `json:"endBlock"`
	RootHash   common.Hash `json:"rootHash"`

	// Current status
	Status BatchStatus `json:"status"`

	// Trigger information (set when triggered)
	TriggerBlock uint64         `json:"triggerBlock"`
	TriggerNode  common.Address `json:"triggerNode"`

	// OTS submission information (set when submitted)
	OTSDigest   [32]byte `json:"otsDigest,omitempty"`
	SubmittedAt uint64   `json:"submittedAt,omitempty"`
	SubmittedBy common.Address `json:"submittedBy,omitempty"`

	// BTC confirmation information (set when confirmed)
	BTCBlockHeight uint64 `json:"btcBlockHeight,omitempty"`
	BTCTxID        string `json:"btcTxId,omitempty"`
	BTCTimestamp   uint64 `json:"btcTimestamp,omitempty"`
	ConfirmedAt    uint64 `json:"confirmedAt,omitempty"`
	ConfirmedBy    common.Address `json:"confirmedBy,omitempty"`

	// Anchor information (set when anchored)
	AnchoredAt uint64 `json:"anchoredAt,omitempty"`
	AnchoredBy common.Address `json:"anchoredBy,omitempty"`
}

// NewOTSState creates a new OTS state with default values
func NewOTSState(enabled bool) *OTSState {
	return &OTSState{
		Enabled:           enabled,
		LastAnchoredBlock: 0,
		CurrentBatch:      nil,
	}
}

// Copy creates a deep copy of the OTS state
func (s *OTSState) Copy() *OTSState {
	cpy := &OTSState{
		Enabled:           s.Enabled,
		LastAnchoredBlock: s.LastAnchoredBlock,
	}
	if s.CurrentBatch != nil {
		cpy.CurrentBatch = s.CurrentBatch.Copy()
	}
	return cpy
}

// Copy creates a deep copy of the batch state
func (b *BatchState) Copy() *BatchState {
	cpy := *b
	return &cpy
}

// HasActiveBatch returns true if there's an active batch being processed
func (s *OTSState) HasActiveBatch() bool {
	return s.CurrentBatch != nil && s.CurrentBatch.Status != BatchStatusNone
}

// CanTrigger returns true if a new batch can be triggered
func (s *OTSState) CanTrigger() bool {
	return s.Enabled && !s.HasActiveBatch()
}

// Trigger starts a new batch
func (s *OTSState) Trigger(startBlock, endBlock, triggerBlock uint64, triggerNode common.Address, rootHash common.Hash) error {
	if !s.CanTrigger() {
		return ErrAlreadyTriggered
	}

	s.CurrentBatch = &BatchState{
		StartBlock:   startBlock,
		EndBlock:     endBlock,
		RootHash:     rootHash,
		Status:       BatchStatusTriggered,
		TriggerBlock: triggerBlock,
		TriggerNode:  triggerNode,
	}
	return nil
}

// MarkSubmitted marks the current batch as submitted to OTS calendar
func (s *OTSState) MarkSubmitted(digest [32]byte, blockNumber uint64, submitter common.Address) error {
	if s.CurrentBatch == nil || s.CurrentBatch.Status != BatchStatusTriggered {
		return ErrNotTriggered
	}
	if !s.CurrentBatch.Status.CanTransitionTo(BatchStatusSubmitted) {
		return ErrInvalidTransition
	}

	s.CurrentBatch.OTSDigest = digest
	s.CurrentBatch.SubmittedAt = blockNumber
	s.CurrentBatch.SubmittedBy = submitter
	s.CurrentBatch.Status = BatchStatusSubmitted
	return nil
}

// MarkConfirmed marks the current batch as confirmed on Bitcoin
func (s *OTSState) MarkConfirmed(btcBlockHeight uint64, btcTxID string, btcTimestamp uint64, blockNumber uint64, confirmer common.Address) error {
	if s.CurrentBatch == nil || s.CurrentBatch.Status != BatchStatusSubmitted {
		return ErrNotSubmitted
	}
	if !s.CurrentBatch.Status.CanTransitionTo(BatchStatusConfirmed) {
		return ErrInvalidTransition
	}

	s.CurrentBatch.BTCBlockHeight = btcBlockHeight
	s.CurrentBatch.BTCTxID = btcTxID
	s.CurrentBatch.BTCTimestamp = btcTimestamp
	s.CurrentBatch.ConfirmedAt = blockNumber
	s.CurrentBatch.ConfirmedBy = confirmer
	s.CurrentBatch.Status = BatchStatusConfirmed
	return nil
}

// MarkAnchored marks the current batch as anchored on-chain and clears it
func (s *OTSState) MarkAnchored(blockNumber uint64, anchorer common.Address) error {
	if s.CurrentBatch == nil || s.CurrentBatch.Status != BatchStatusConfirmed {
		return ErrNotConfirmed
	}
	if !s.CurrentBatch.Status.CanTransitionTo(BatchStatusAnchored) {
		return ErrInvalidTransition
	}

	// Update last anchored block
	s.LastAnchoredBlock = s.CurrentBatch.EndBlock

	// Mark as anchored (for historical reference)
	s.CurrentBatch.AnchoredAt = blockNumber
	s.CurrentBatch.AnchoredBy = anchorer
	s.CurrentBatch.Status = BatchStatusAnchored

	// Clear current batch (ready for next trigger)
	s.CurrentBatch = nil
	return nil
}

// Encode serializes the OTS state to JSON
func (s *OTSState) Encode() ([]byte, error) {
	return json.Marshal(s)
}

// DecodeOTSState deserializes the OTS state from JSON
func DecodeOTSState(data []byte) (*OTSState, error) {
	var state OTSState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Hash returns a hash of the OTS state for integrity verification
func (s *OTSState) Hash() common.Hash {
	data, _ := s.Encode()
	return common.BytesToHash(crypto.Keccak256(data))
}
