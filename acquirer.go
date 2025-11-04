package liblbry

import (
	"go.lumeweb.com/liblbry/blob/transfer"
	lbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage"
)

// BlobAcquirer defines the interface for blob acquisition with fallback mechanisms
type BlobAcquirer interface {
	// Acquire attempts to acquire a blob using available transfer methods
	Acquire(hash string) ([]byte, error)
}

// DefaultBlobAcquirer implements the BlobAcquirer interface with fallback mechanisms
type DefaultBlobAcquirer struct {
	transfers []transfer.Transfer
	store     storage.BlobStore
}

// NewBlobAcquirer creates a new BlobAcquirer with the given transfer methods and storage
func NewBlobAcquirer(transfers []transfer.Transfer, store storage.BlobStore) BlobAcquirer {
	return &DefaultBlobAcquirer{
		transfers: transfers,
		store:     store,
	}
}

// Acquire attempts to acquire a blob using the transfer methods in order
func (ba *DefaultBlobAcquirer) Acquire(hash string) ([]byte, error) {
	// First check if blob already exists in storage
	has, err := ba.store.Has(hash)
	if err != nil {
		return nil, err
	}

	if has {
		// Blob exists in storage, retrieve it
		data, err := ba.store.Get(hash)
		if err != nil {
			return nil, err
		}
		return data, nil
	}

	// Try each transfer method in order
	for _, transferMethod := range ba.transfers {
		data, err := transferMethod.Get(hash)
		if err == nil {
			// Successfully acquired blob, store it
			err = ba.store.Put(hash, data)
			if err != nil {
				return nil, err
			}
			return data, nil
		}
		// If transfer failed, continue to next one
	}

	return nil, lbryerrors.ErrAcquisitionFailed
}
