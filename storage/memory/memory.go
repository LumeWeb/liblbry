// Package memory provides an in-memory implementation of the BlobStore interface
package memory

import (
	"sync"

	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/stream"
)

// MemoryStore implements the BlobStore interface using in-memory storage
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

// Has checks if a blob exists in the store
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

// Get retrieves a blob from the store
func (m *MemoryStore) Get(hash string) ([]byte, error) {
	// Validate hash format
	if !stream.ValidateHash(hash) {
		return nil, liblbryerrors.Err("invalid hash format")
	}

	m.mutex.RLock()
	defer m.mutex.RUnlock()

	// Check in regular blobs first
	if data, exists := m.blobs[hash]; exists {
		return data, nil
	}

	// Check in SD blobs
	if data, exists := m.sdBlobs[hash]; exists {
		return data, nil
	}

	// Blob not found
	return nil, liblbryerrors.Err("blob not found")
}

// Put stores a regular blob in the store
func (m *MemoryStore) Put(hash string, data []byte) error {
	// Validate hash format
	if !stream.ValidateHash(hash) {
		return liblbryerrors.Err("invalid hash format")
	}

	// Validate blob data
	b := blob.Blob(data)
	if err := b.ValidForSend(); err != nil {
		return liblbryerrors.Err("invalid blob data: %w", err)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.blobs[hash] = data
	return nil
}

// PutSD stores an SD blob in the store
func (m *MemoryStore) PutSD(hash string, data []byte) error {
	// Validate hash format
	if !stream.ValidateHash(hash) {
		return liblbryerrors.Err("invalid hash format")
	}

	// Validate blob data
	b := blob.Blob(data)
	if err := b.ValidForSend(); err != nil {
		return liblbryerrors.Err("invalid SD blob data: %w", err)
	}

	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.sdBlobs[hash] = data
	return nil
}

// Name returns the name of the store implementation
func (m *MemoryStore) Name() string {
	return "memory"
}
