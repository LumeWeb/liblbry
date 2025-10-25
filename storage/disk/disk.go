// Package liblbry implements a disk-based blob storage backend
package disk

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/knadh/koanf/v2"
	"go.lumeweb.com/liblbry"
	"go.uber.org/zap"
)

// DiskStore implements the BlobStore interface using the file system
type DiskStore struct {
	path   string
	logger *zap.Logger
}

// DiskStoreFactory implements the StoreFactory interface for creating DiskStore instances
type DiskStoreFactory struct {
	logger *zap.Logger
}

// CreateStore creates a new DiskStore instance from the provided configuration
func (f *DiskStoreFactory) CreateStore(config *koanf.Koanf) (liblbry.BlobStore, error) {
	if config == nil {
		err := fmt.Errorf("configuration cannot be nil")
		if f.logger != nil {
			f.logger.Error("failed to create disk store", zap.Error(err))
		}
		return nil, err
	}

	path := config.String("path")
	if path == "" {
		err := fmt.Errorf("missing or invalid 'path' configuration parameter")
		if f.logger != nil {
			f.logger.Error("failed to create disk store", zap.Error(err))
		}
		return nil, err
	}

	// Create the directory if it doesn't exist
	if err := os.MkdirAll(path, 0755); err != nil {
		err = fmt.Errorf("failed to create storage directory: %w", err)
		if f.logger != nil {
			f.logger.Error("failed to create disk store", zap.Error(err))
		}
		return nil, err
	}

	if f.logger != nil {
		f.logger.Debug("created disk store", zap.String("path", path))
	}

	return &DiskStore{path: path, logger: f.logger}, nil
}

// Name returns the name of the factory
func (f *DiskStoreFactory) Name() string {
	if f.logger != nil {
		f.logger.Debug("returning disk store factory name")
	}
	return "disk"
}

// SetLogger configures the factory with a logger
func (f *DiskStoreFactory) SetLogger(logger *zap.Logger) {
	f.logger = logger
}

// Has checks if a blob exists in the disk store
func (d *DiskStore) Has(hash string) (bool, error) {
	if len(hash) < 2 {
		if d.logger != nil {
			d.logger.Debug("invalid hash length for Has operation", zap.String("hash", hash))
		}
		return false, nil
	}

	// Check regular blob
	blobPath := filepath.Join(d.path, hash[:2], hash)
	if _, err := os.Stat(blobPath); err == nil {
		if d.logger != nil {
			d.logger.Debug("found regular blob", zap.String("hash", hash), zap.String("path", blobPath))
		}
		return true, nil
	}

	// Check SD blob
	sdBlobPath := filepath.Join(d.path, "sd", hash[:2], hash)
	if _, err := os.Stat(sdBlobPath); err == nil {
		if d.logger != nil {
			d.logger.Debug("found SD blob", zap.String("hash", hash), zap.String("path", sdBlobPath))
		}
		return true, nil
	}

	if d.logger != nil {
		d.logger.Debug("blob not found", zap.String("hash", hash))
	}
	return false, nil
}

// Get retrieves a blob from the disk store
func (d *DiskStore) Get(hash string) ([]byte, error) {
	if len(hash) < 2 {
		err := fmt.Errorf("invalid hash: %s", hash)
		if d.logger != nil {
			d.logger.Error("invalid hash for Get operation", zap.String("hash", hash), zap.Error(err))
		}
		return nil, err
	}

	// Try to get regular blob first
	blobPath := filepath.Join(d.path, hash[:2], hash)
	if data, err := os.ReadFile(blobPath); err == nil {
		if d.logger != nil {
			d.logger.Debug("retrieved regular blob", 
				zap.String("hash", hash), 
				zap.String("path", blobPath), 
				zap.Int("size", len(data)))
		}
		return data, nil
	}

	// Try to get SD blob
	sdBlobPath := filepath.Join(d.path, "sd", hash[:2], hash)
	if data, err := os.ReadFile(sdBlobPath); err == nil {
		if d.logger != nil {
			d.logger.Debug("retrieved SD blob", 
				zap.String("hash", hash), 
				zap.String("path", sdBlobPath), 
				zap.Int("size", len(data)))
		}
		return data, nil
	}

	err := fmt.Errorf("blob not found: %s", hash)
	if d.logger != nil {
		d.logger.Debug("blob not found in Get operation", zap.String("hash", hash), zap.Error(err))
	}
	return nil, err
}

// Put stores a regular blob in the disk store
func (d *DiskStore) Put(hash string, data []byte) error {
	if len(hash) < 2 {
		err := fmt.Errorf("invalid hash: %s", hash)
		if d.logger != nil {
			d.logger.Error("invalid hash for Put operation", zap.String("hash", hash), zap.Error(err))
		}
		return err
	}

	// Create directory structure
	blobDir := filepath.Join(d.path, hash[:2])
	if err := os.MkdirAll(blobDir, 0755); err != nil {
		err = fmt.Errorf("failed to create blob directory: %w", err)
		if d.logger != nil {
			d.logger.Error("failed to create blob directory", 
				zap.String("hash", hash), 
				zap.String("directory", blobDir), 
				zap.Error(err))
		}
		return err
	}

	// Write blob data
	blobPath := filepath.Join(blobDir, hash)
	if err := os.WriteFile(blobPath, data, 0644); err != nil {
		err = fmt.Errorf("failed to write blob: %w", err)
		if d.logger != nil {
			d.logger.Error("failed to write blob", 
				zap.String("hash", hash), 
				zap.String("path", blobPath), 
				zap.Int("size", len(data)), 
				zap.Error(err))
		}
		return err
	}

	if d.logger != nil {
		d.logger.Debug("stored regular blob", 
			zap.String("hash", hash), 
			zap.String("path", blobPath), 
			zap.Int("size", len(data)))
	}
	return nil
}

// PutSD stores an SD blob in the disk store
func (d *DiskStore) PutSD(hash string, data []byte) error {
	if len(hash) < 2 {
		err := fmt.Errorf("invalid hash: %s", hash)
		if d.logger != nil {
			d.logger.Error("invalid hash for PutSD operation", zap.String("hash", hash), zap.Error(err))
		}
		return err
	}

	// Create directory structure for SD blobs
	sdBlobDir := filepath.Join(d.path, "sd", hash[:2])
	if err := os.MkdirAll(sdBlobDir, 0755); err != nil {
		err = fmt.Errorf("failed to create SD blob directory: %w", err)
		if d.logger != nil {
			d.logger.Error("failed to create SD blob directory", 
				zap.String("hash", hash), 
				zap.String("directory", sdBlobDir), 
				zap.Error(err))
		}
		return err
	}

	// Write SD blob data
	sdBlobPath := filepath.Join(sdBlobDir, hash)
	if err := os.WriteFile(sdBlobPath, data, 0644); err != nil {
		err = fmt.Errorf("failed to write SD blob: %w", err)
		if d.logger != nil {
			d.logger.Error("failed to write SD blob", 
				zap.String("hash", hash), 
				zap.String("path", sdBlobPath), 
				zap.Int("size", len(data)), 
				zap.Error(err))
		}
		return err
	}

	if d.logger != nil {
		d.logger.Debug("stored SD blob", 
			zap.String("hash", hash), 
			zap.String("path", sdBlobPath), 
			zap.Int("size", len(data)))
	}
	return nil
}

// Name returns the name of the disk store
func (d *DiskStore) Name() string {
	if d.logger != nil {
		d.logger.Debug("returning disk store name")
	}
	return "disk"
}
