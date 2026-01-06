// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// Package metrics provides Prometheus metrics for the OTS module.

package metrics

import (
	"github.com/ethereum/go-ethereum/metrics"
)

// Namespace prefix for all OTS metrics
const namespace = "ots/"

// Batch lifecycle metrics
var (
	// BatchesCreatedCounter counts total batches created
	BatchesCreatedCounter = metrics.NewRegisteredCounter(namespace+"batches/created", nil)

	// BatchesSubmittedCounter counts batches submitted to OTS calendar
	BatchesSubmittedCounter = metrics.NewRegisteredCounter(namespace+"batches/submitted", nil)

	// BatchesConfirmedCounter counts batches confirmed on Bitcoin
	BatchesConfirmedCounter = metrics.NewRegisteredCounter(namespace+"batches/confirmed", nil)

	// BatchesAnchoredCounter counts batches anchored on-chain
	BatchesAnchoredCounter = metrics.NewRegisteredCounter(namespace+"batches/anchored", nil)

	// BatchesFailedCounter counts batches that failed processing
	BatchesFailedCounter = metrics.NewRegisteredCounter(namespace+"batches/failed", nil)
)

// Event metrics
var (
	// EventsCollectedCounter counts total copyright events collected
	EventsCollectedCounter = metrics.NewRegisteredCounter(namespace+"events/collected", nil)

	// EventsProcessedMeter measures events processed per second
	EventsProcessedMeter = metrics.NewRegisteredMeter(namespace+"events/processed", nil)
)

// RUID metrics
var (
	// RUIDsInBatchesCounter counts total RUIDs included in batches
	RUIDsInBatchesCounter = metrics.NewRegisteredCounter(namespace+"ruids/batched", nil)

	// RUIDVerificationsCounter counts RUID verification requests
	RUIDVerificationsCounter = metrics.NewRegisteredCounter(namespace+"ruids/verifications", nil)

	// RUIDVerificationsSuccessCounter counts successful RUID verifications
	RUIDVerificationsSuccessCounter = metrics.NewRegisteredCounter(namespace+"ruids/verifications/success", nil)
)

// State gauges (current state indicators)
var (
	// PendingBatchesGauge shows current number of pending batches
	PendingBatchesGauge = metrics.NewRegisteredGauge(namespace+"batches/pending", nil)

	// SubmittedBatchesGauge shows current number of submitted (awaiting BTC confirmation) batches
	SubmittedBatchesGauge = metrics.NewRegisteredGauge(namespace+"batches/submitted/current", nil)

	// ConfirmedBatchesGauge shows current number of confirmed (awaiting anchoring) batches
	ConfirmedBatchesGauge = metrics.NewRegisteredGauge(namespace+"batches/confirmed/current", nil)

	// LastProcessedBlockGauge shows the last processed block number
	LastProcessedBlockGauge = metrics.NewRegisteredGauge(namespace+"blocks/lastprocessed", nil)

	// ModuleStateGauge shows the module state (0=uninitialized, 1=starting, 2=running, 3=stopping, 4=stopped)
	ModuleStateGauge = metrics.NewRegisteredGauge(namespace+"module/state", nil)
)

// Timing metrics
var (
	// BatchProcessingTimer measures batch creation and processing time
	BatchProcessingTimer = metrics.NewRegisteredTimer(namespace+"batch/processing", nil)

	// MerkleTreeBuildTimer measures Merkle tree construction time
	MerkleTreeBuildTimer = metrics.NewRegisteredTimer(namespace+"merkle/build", nil)

	// CalendarSubmitTimer measures OTS calendar submission time
	CalendarSubmitTimer = metrics.NewRegisteredTimer(namespace+"calendar/submit", nil)

	// SystemTxBuildTimer measures system transaction build time
	SystemTxBuildTimer = metrics.NewRegisteredTimer(namespace+"systx/build", nil)

	// VerificationTimer measures RUID verification time
	VerificationTimer = metrics.NewRegisteredTimer(namespace+"verification/duration", nil)
)

// Storage metrics
var (
	// StorageWritesMeter measures storage write operations per second
	StorageWritesMeter = metrics.NewRegisteredMeter(namespace+"storage/writes", nil)

	// StorageReadsMeter measures storage read operations per second
	StorageReadsMeter = metrics.NewRegisteredMeter(namespace+"storage/reads", nil)

	// StorageSizeGauge shows approximate storage size in bytes
	StorageSizeGauge = metrics.NewRegisteredGauge(namespace+"storage/size", nil)
)

// Bitcoin/OTS metrics
var (
	// BTCConfirmationTimeGauge shows average time to Bitcoin confirmation in seconds
	BTCConfirmationTimeGauge = metrics.NewRegisteredGauge(namespace+"btc/confirmation/time", nil)

	// BTCBlockHeightGauge shows the latest confirmed Bitcoin block height
	BTCBlockHeightGauge = metrics.NewRegisteredGauge(namespace+"btc/blockheight", nil)

	// CalendarServerHealthGauge shows calendar server health (0=down, 1=up)
	CalendarServerHealthGauge = metrics.NewRegisteredGauge(namespace+"calendar/health", nil)
)

// Error metrics
var (
	// CollectorErrorsCounter counts event collection errors
	CollectorErrorsCounter = metrics.NewRegisteredCounter(namespace+"errors/collector", nil)

	// CalendarErrorsCounter counts calendar submission errors
	CalendarErrorsCounter = metrics.NewRegisteredCounter(namespace+"errors/calendar", nil)

	// StorageErrorsCounter counts storage operation errors
	StorageErrorsCounter = metrics.NewRegisteredCounter(namespace+"errors/storage", nil)

	// SystemTxErrorsCounter counts system transaction errors
	SystemTxErrorsCounter = metrics.NewRegisteredCounter(namespace+"errors/systx", nil)
)

// Helper functions to update metrics

// IncBatchCreated increments the batch created counter and updates pending gauge
func IncBatchCreated(ruidCount int) {
	BatchesCreatedCounter.Inc(1)
	RUIDsInBatchesCounter.Inc(int64(ruidCount))
}

// IncBatchSubmitted increments the batch submitted counter
func IncBatchSubmitted() {
	BatchesSubmittedCounter.Inc(1)
}

// IncBatchConfirmed increments the batch confirmed counter
func IncBatchConfirmed() {
	BatchesConfirmedCounter.Inc(1)
}

// IncBatchAnchored increments the batch anchored counter
func IncBatchAnchored() {
	BatchesAnchoredCounter.Inc(1)
}

// IncBatchFailed increments the batch failed counter
func IncBatchFailed() {
	BatchesFailedCounter.Inc(1)
}

// UpdatePendingBatches updates the pending batches gauge
func UpdatePendingBatches(count int) {
	PendingBatchesGauge.Update(int64(count))
}

// UpdateSubmittedBatches updates the submitted batches gauge
func UpdateSubmittedBatches(count int) {
	SubmittedBatchesGauge.Update(int64(count))
}

// UpdateConfirmedBatches updates the confirmed batches gauge
func UpdateConfirmedBatches(count int) {
	ConfirmedBatchesGauge.Update(int64(count))
}

// UpdateLastProcessedBlock updates the last processed block gauge
func UpdateLastProcessedBlock(blockNum uint64) {
	LastProcessedBlockGauge.Update(int64(blockNum))
}

// UpdateModuleState updates the module state gauge
func UpdateModuleState(state int) {
	ModuleStateGauge.Update(int64(state))
}

// UpdateBTCBlockHeight updates the Bitcoin block height gauge
func UpdateBTCBlockHeight(height uint64) {
	BTCBlockHeightGauge.Update(int64(height))
}

// MarkEventsCollected records events collected
func MarkEventsCollected(count int) {
	EventsCollectedCounter.Inc(int64(count))
	EventsProcessedMeter.Mark(int64(count))
}

// IncVerification records a verification attempt
func IncVerification(success bool) {
	RUIDVerificationsCounter.Inc(1)
	if success {
		RUIDVerificationsSuccessCounter.Inc(1)
	}
}

// IncCollectorError records a collector error
func IncCollectorError() {
	CollectorErrorsCounter.Inc(1)
}

// IncCalendarError records a calendar error
func IncCalendarError() {
	CalendarErrorsCounter.Inc(1)
}

// IncStorageError records a storage error
func IncStorageError() {
	StorageErrorsCounter.Inc(1)
}

// IncSystemTxError records a system transaction error
func IncSystemTxError() {
	SystemTxErrorsCounter.Inc(1)
}
