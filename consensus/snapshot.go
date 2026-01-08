// Copyright 2024 The RMC Authors
// This file is part of the RMC library.
//
// This file implements OTS snapshot management, similar to Parlia's validator snapshots.
// Snapshots are stored locally and can be rebuilt from chain data.

package consensus

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	lru "github.com/hashicorp/golang-lru"
)

const (
	// snapshotCacheSize is the number of snapshots to keep in memory
	snapshotCacheSize = 128

	// snapshotPersistInterval is the block interval for persisting snapshots
	snapshotPersistInterval = 1024
)

var (
	ErrSnapshotNotFound = errors.New("OTS snapshot not found")
	ErrInvalidSnapshot  = errors.New("invalid OTS snapshot")

	// Database key prefixes
	snapshotPrefix = []byte("ots-snapshot-")
)

// Snapshot represents an OTS state snapshot at a specific block
type Snapshot struct {
	Number uint64      `json:"number"` // Block number
	Hash   common.Hash `json:"hash"`   // Block hash
	State  *OTSState   `json:"state"`  // OTS state at this block
}

// NewSnapshot creates a new snapshot
func NewSnapshot(number uint64, hash common.Hash, state *OTSState) *Snapshot {
	return &Snapshot{
		Number: number,
		Hash:   hash,
		State:  state.Copy(),
	}
}

// Copy creates a deep copy of the snapshot
func (s *Snapshot) Copy() *Snapshot {
	return &Snapshot{
		Number: s.Number,
		Hash:   s.Hash,
		State:  s.State.Copy(),
	}
}

// Encode serializes the snapshot to JSON
func (s *Snapshot) Encode() ([]byte, error) {
	return json.Marshal(s)
}

// DecodeSnapshot deserializes a snapshot from JSON
func DecodeSnapshot(data []byte) (*Snapshot, error) {
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// SnapshotManager manages OTS snapshots with caching and persistence
type SnapshotManager struct {
	db    ethdb.Database
	cache *lru.ARCCache
	mu    sync.RWMutex

	// Configuration
	otsEnabled bool
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(db ethdb.Database, otsEnabled bool) (*SnapshotManager, error) {
	cache, err := lru.NewARC(snapshotCacheSize)
	if err != nil {
		return nil, err
	}

	return &SnapshotManager{
		db:         db,
		cache:      cache,
		otsEnabled: otsEnabled,
	}, nil
}

// GetSnapshot retrieves a snapshot for the given block hash
// Returns cached version if available, otherwise loads from database
func (sm *SnapshotManager) GetSnapshot(hash common.Hash) (*Snapshot, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Check cache first
	if snap, ok := sm.cache.Get(hash); ok {
		return snap.(*Snapshot).Copy(), nil
	}

	// Load from database
	snap, err := sm.loadFromDB(hash)
	if err != nil {
		return nil, err
	}

	// Add to cache
	sm.cache.Add(hash, snap)
	return snap.Copy(), nil
}

// GetSnapshotByNumber retrieves a snapshot for the given block number
// This requires knowing the block hash, so it's less efficient
func (sm *SnapshotManager) GetSnapshotByNumber(number uint64, hash common.Hash) (*Snapshot, error) {
	return sm.GetSnapshot(hash)
}

// StoreSnapshot stores a snapshot both in cache and database
func (sm *SnapshotManager) StoreSnapshot(snap *Snapshot) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Add to cache
	sm.cache.Add(snap.Hash, snap.Copy())

	// Persist to database at intervals
	if snap.Number%snapshotPersistInterval == 0 {
		return sm.saveToDB(snap)
	}

	return nil
}

// ForceStore forces storage of a snapshot to database regardless of interval
func (sm *SnapshotManager) ForceStore(snap *Snapshot) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.cache.Add(snap.Hash, snap.Copy())
	return sm.saveToDB(snap)
}

// loadFromDB loads a snapshot from the database
func (sm *SnapshotManager) loadFromDB(hash common.Hash) (*Snapshot, error) {
	key := append(snapshotPrefix, hash.Bytes()...)
	data, err := sm.db.Get(key)
	if err != nil {
		return nil, ErrSnapshotNotFound
	}
	return DecodeSnapshot(data)
}

// saveToDB saves a snapshot to the database
func (sm *SnapshotManager) saveToDB(snap *Snapshot) error {
	key := append(snapshotPrefix, snap.Hash.Bytes()...)
	data, err := snap.Encode()
	if err != nil {
		return err
	}
	return sm.db.Put(key, data)
}

// DeleteSnapshot removes a snapshot from cache and database
func (sm *SnapshotManager) DeleteSnapshot(hash common.Hash) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.cache.Remove(hash)
	key := append(snapshotPrefix, hash.Bytes()...)
	return sm.db.Delete(key)
}

// GetGenesisSnapshot returns the genesis OTS snapshot
func (sm *SnapshotManager) GetGenesisSnapshot(genesisHash common.Hash) *Snapshot {
	return NewSnapshot(0, genesisHash, NewOTSState(sm.otsEnabled))
}

// HasSnapshot checks if a snapshot exists for the given hash
func (sm *SnapshotManager) HasSnapshot(hash common.Hash) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if sm.cache.Contains(hash) {
		return true
	}

	key := append(snapshotPrefix, hash.Bytes()...)
	has, _ := sm.db.Has(key)
	return has
}

// CacheStats returns cache statistics for monitoring
func (sm *SnapshotManager) CacheStats() (size int, capacity int) {
	return sm.cache.Len(), snapshotCacheSize
}

// Clear clears the snapshot cache (useful for testing or chain reorganization)
func (sm *SnapshotManager) Clear() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cache.Purge()
}

// FindNearestSnapshot finds the nearest stored snapshot before the given block number
// This is used when rebuilding state from chain data
func (sm *SnapshotManager) FindNearestSnapshot(targetNumber uint64, getHash func(uint64) common.Hash) (*Snapshot, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	// Search backwards from target in steps of persist interval
	for num := (targetNumber / snapshotPersistInterval) * snapshotPersistInterval; num > 0; num -= snapshotPersistInterval {
		hash := getHash(num)
		if hash == (common.Hash{}) {
			continue
		}

		// Check cache
		if snap, ok := sm.cache.Get(hash); ok {
			return snap.(*Snapshot).Copy(), nil
		}

		// Check database
		snap, err := sm.loadFromDB(hash)
		if err == nil {
			sm.cache.Add(hash, snap)
			return snap.Copy(), nil
		}
	}

	// If no snapshot found, return genesis
	genesisHash := getHash(0)
	return sm.GetGenesisSnapshot(genesisHash), nil
}
