package liblbry

import (
	"context"
	"errors"
	"go.lumeweb.com/liblbry/blob/transfer"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage"
)

// BlobAcquirer defines the interface for blob acquisition with fallback mechanisms
type BlobAcquirer interface {
	// Acquire attempts to acquire a blob using available transfer methods
	Acquire(ctx context.Context, hash string) ([]byte, error)
}

// DefaultBlobAcquirer implements the BlobAcquirer interface with fallback mechanisms
type DefaultBlobAcquirer struct {
	transfers []transfer.Transfer
	store     storage.BlobStore
}

// NewBlobAcquirer creates a new BlobAcquirer with the given transfer methods and storage
func NewBlobAcquirer(transfers []transfer.Transfer, store storage.BlobStore) (BlobAcquirer, error) {
	if store == nil {
		return nil, errors.New("store cannot be nil")
	}
	return &DefaultBlobAcquirer{
		transfers: transfers,
		store:     store,
	}, nil
}

// Acquire attempts to acquire a blob using the transfer methods in order
func (ba *DefaultBlobAcquirer) Acquire(ctx context.Context, hash string) ([]byte, error) {
	if ba.store == nil {
		return nil, liblbryerrors.ErrAcquisitionFailed
	}

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
		if transferMethod == nil {
			continue
		}
		data, err := transferMethod.Get(ctx, hash)
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

	return nil, liblbryerrors.ErrAcquisitionFailed
}
