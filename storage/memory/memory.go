// Package memory provides an in-memory implementation of the BlobStore interface
package memory

import (
	"sort"
	"sync"

	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/stream"
)

// MemoryStore implements the BlobStore interface using in-memory storage.
// It provides copy-in/copy-out semantics: all data passed to Put/PutSD is copied
// before storage, and all data returned from Get is copied before return.
// This ensures that returned slices are safe to use and mutate by callers
// without affecting the stored data or causing race conditions.
type MemoryStore struct {
	blobs   map[string][]byte
	sdBlobs map[string][]byte
	mutex   sync.RWMutex
}

// NewMemoryStore creates a new instance of MemoryStore
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		blobs:   make(map[string][]byte),
		sdBlobs: make(map[string][]byte),
	}
}

// validate performs common validation for hash format and blob data
func (m *MemoryStore) validate(hash string, data []byte, kind string) error {
	if !stream.ValidateHash(hash) {
		return liblbryerrors.Err("invalid hash format")
	}
	b := blob.Blob(data)
	if err := b.ValidForSend(); err != nil {
		if kind != "" {
			return liblbryerrors.Err("invalid %s blob data: %w", kind, err)
		}
		return liblbryerrors.Err("invalid blob data: %w", err)
	}
	return nil
}

// Has checks if a blob exists in the store.
//
// It checks for regular blobs first, then SD blobs. If the same hash exists in both
// maps, this method returns true (indicating the blob exists) regardless of which
// type contains it. This behavior is consistent with the Get method's priority
// system where regular blobs are prioritized over SD blobs.
func (m *MemoryStore) Has(hash string) (bool, error) {
	// Validate hash format
	if !stream.ValidateHash(hash) {
		return false, liblbryerrors.Err("invalid hash format")
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check in both regular blobs and SD blobs
	if _, exists := m.blobs[hash]; exists {
		return true, nil
	}
	if _, exists := m.sdBlobs[hash]; exists {
		return true, nil
	}

	return false, nil
}

// Get retrieves a blob from the store.
//
// It prioritizes regular blobs over SD blobs when both exist with the same hash.
// This priority system ensures that regular content takes precedence over metadata
// (SD blobs) in hash collision scenarios. The returned data is a defensive copy
// to prevent callers from mutating the stored data.
func (m *MemoryStore) Get(hash string) ([]byte, error) {
	// Validate hash format
	if !stream.ValidateHash(hash) {
		return nil, liblbryerrors.Err("invalid hash format")
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check in regular blobs first (priority over SD blobs)
	if data, exists := m.blobs[hash]; exists {
		dst := make([]byte, len(data))
		copy(dst, data)
		return dst, nil
	}

	// Check in SD blobs (lower priority)
	if data, exists := m.sdBlobs[hash]; exists {
		dst := make([]byte, len(data))
		copy(dst, data)
		return dst, nil
	}

	// Blob not found
	return nil, liblbryerrors.Err("blob not found")
}

// Put stores a regular blob in the store
func (m *MemoryStore) Put(hash string, data []byte) error {
	if err := m.validate(hash, data, ""); err != nil {
		return err
	}

	// Create defensive copy of the data
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.blobs[hash] = dataCopy
	return nil
}

// PutSD stores an SD blob in the store
func (m *MemoryStore) PutSD(hash string, data []byte) error {
	if err := m.validate(hash, data, "SD"); err != nil {
		return err
	}

	// Create defensive copy of the data
	dataCopy := make([]byte, len(data))
	copy(dataCopy, data)

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.sdBlobs[hash] = dataCopy
	return nil
}

// Name returns the name of the store implementation
func (m *MemoryStore) Name() string {
	return "memory"
}

// List returns a list of blob hashes with pagination support
func (m *MemoryStore) List(offset, limit int) ([]string, error) {
	if offset < 0 {
		return nil, liblbryerrors.ErrInvalidOffset
	}
	if limit <= 0 {
		return nil, liblbryerrors.ErrInvalidLimit
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Collect all blob hashes with deduplication
	allHashes := make(map[string]struct{})
	for hash := range m.blobs {
		allHashes[hash] = struct{}{}
	}
	for hash := range m.sdBlobs {
		allHashes[hash] = struct{}{}
	}

	// Convert to slice and sort for deterministic ordering
	hashSlice := make([]string, 0, len(allHashes))
	for hash := range allHashes {
		hashSlice = append(hashSlice, hash)
	}
	sort.Strings(hashSlice)

	// Apply pagination
	start := offset
	if start >= len(hashSlice) {
		return []string{}, nil
	}

	end := start + limit
	if end > len(hashSlice) {
		end = len(hashSlice)
	}

	return hashSlice[start:end], nil
}
