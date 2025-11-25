// Package memory provides an in-memory implementation of the BlobStore interface
package memory

import (
	"context"
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
		return liblbryerrors.ErrInvalidHash
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

// acquireReadLock acquires a read lock with context cancellation support
func (m *MemoryStore) acquireReadLock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		m.mutex.RLock()
		return nil
	}
}

// acquireWriteLock acquires a write lock with context cancellation support
func (m *MemoryStore) acquireWriteLock(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		m.mutex.Lock()
		return nil
	}
}

// copyWithContext creates a copy of data while respecting context cancellation
func (m *MemoryStore) copyWithContext(ctx context.Context, data []byte) ([]byte, error) {
	// Check context before the copy operation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// For in-memory operations, direct copy is optimal
	// Memory copies complete in microseconds, making chunked copying unnecessary
	dst := make([]byte, len(data))
	copy(dst, data)

	return dst, nil
}

// Has checks if a blob exists in the store.
//
// It checks for regular blobs first, then SD blobs. If the same hash exists in both
// maps, this method returns true (indicating the blob exists) regardless of which
// type contains it. This behavior is consistent with the Get method's priority
// system where regular blobs are prioritized over SD blobs.
// Respects context cancellation during validation and lock acquisition.
func (m *MemoryStore) Has(ctx context.Context, hash string) (bool, error) {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Validate hash format
	if !stream.ValidateHash(hash) {
		return false, liblbryerrors.ErrInvalidHash
	}

	// Acquire read lock with context awareness
	if err := m.acquireReadLock(ctx); err != nil {
		return false, err
	}
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
// Respects context cancellation during lock acquisition and data copying.
func (m *MemoryStore) Get(ctx context.Context, hash string) ([]byte, error) {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate hash format
	if !stream.ValidateHash(hash) {
		return nil, liblbryerrors.ErrInvalidHash
	}

	// Acquire read lock with context awareness
	if err := m.acquireReadLock(ctx); err != nil {
		return nil, err
	}
	defer m.mutex.RUnlock()

	// Check in regular blobs first (priority over SD blobs)
	if data, exists := m.blobs[hash]; exists {
		return m.copyWithContext(ctx, data)
	}

	// Check in SD blobs (lower priority)
	if data, exists := m.sdBlobs[hash]; exists {
		return m.copyWithContext(ctx, data)
	}

	// Blob not found
	return nil, liblbryerrors.Err("blob not found")
}

// Put stores a regular blob in the store
// Respects context cancellation during validation, lock acquisition, and data copying.
func (m *MemoryStore) Put(ctx context.Context, hash string, data []byte) error {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := m.validate(hash, data, ""); err != nil {
		return err
	}

	// Acquire write lock with context awareness
	if err := m.acquireWriteLock(ctx); err != nil {
		return err
	}
	defer m.mutex.Unlock()

	// Create defensive copy of the data with context awareness
	dataCopy, err := m.copyWithContext(ctx, data)
	if err != nil {
		return err
	}

	m.blobs[hash] = dataCopy
	return nil
}

// PutSD stores an SD blob in the store
// Respects context cancellation during validation, lock acquisition, and data copying.
func (m *MemoryStore) PutSD(ctx context.Context, hash string, data []byte) error {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := m.validate(hash, data, "SD"); err != nil {
		return err
	}

	// Acquire write lock with context awareness
	if err := m.acquireWriteLock(ctx); err != nil {
		return err
	}
	defer m.mutex.Unlock()

	// Create defensive copy of the data with context awareness
	dataCopy, err := m.copyWithContext(ctx, data)
	if err != nil {
		return err
	}

	m.sdBlobs[hash] = dataCopy
	return nil
}

// Name returns the name of the store implementation
func (m *MemoryStore) Name() string {
	return "memory"
}

// List returns a list of blob hashes with pagination support
// Respects context cancellation during validation, lock acquisition, and sorting.
func (m *MemoryStore) List(ctx context.Context, offset, limit int) ([]string, error) {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if offset < 0 {
		return nil, liblbryerrors.ErrInvalidOffset
	}
	if limit <= 0 {
		return nil, liblbryerrors.ErrInvalidLimit
	}

	// Acquire read lock with context awareness
	if err := m.acquireReadLock(ctx); err != nil {
		return nil, err
	}
	defer m.mutex.RUnlock()

	// Collect all blob hashes with deduplication
	allHashes := make(map[string]struct{})
	for hash := range m.blobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		allHashes[hash] = struct{}{}
	}
	for hash := range m.sdBlobs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		allHashes[hash] = struct{}{}
	}

	// Convert to slice and sort for deterministic ordering
	hashSlice := make([]string, 0, len(allHashes))
	for hash := range allHashes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		hashSlice = append(hashSlice, hash)
	}

	// Sort for deterministic ordering
	sort.Strings(hashSlice)

	// Final context check before pagination
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Apply pagination
	start := offset
	if start > len(hashSlice) {
		return nil, liblbryerrors.ErrEndOfList
	}

	end := start + limit
	if end > len(hashSlice) {
		end = len(hashSlice)
	}

	return hashSlice[start:end], nil
}

// Delete removes a blob from storage.
// If the blob exists in both regular and SD blob stores, it removes both.
// If the blob is not found, it returns nil (no-op).
// Only returns an error for invalid hash format or other unknown errors.
// Respects context cancellation during validation and lock acquisition.
func (m *MemoryStore) Delete(ctx context.Context, hash string) error {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate hash format
	if !stream.ValidateHash(hash) {
		return liblbryerrors.ErrInvalidHash
	}

	// Acquire write lock with context awareness
	if err := m.acquireWriteLock(ctx); err != nil {
		return err
	}
	defer m.mutex.Unlock()

	// Delete from regular blobs if it exists
	delete(m.blobs, hash)

	// Delete from SD blobs if it exists
	delete(m.sdBlobs, hash)

	// Always return nil as this is a no-op if the blob doesn't exist
	return nil
}
