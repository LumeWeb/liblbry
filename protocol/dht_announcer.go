package protocol

import (
	"sync"

	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

// DHTAnnouncer interface for blob announcement
// This interface provides a clean abstraction for blob announcement
// without duplicating the underlying DHT reannouncement logic
type DHTAnnouncer interface {
	// AnnounceBlob announces a blob hash to the DHT network
	AnnounceBlob(hash string) error

	// RemoveBlob removes a blob hash from DHT announcements
	RemoveBlob(hash string) error
}

// DefaultDHTAnnouncer implements DHTAnnouncer with minimal overhead
// It provides a clean interface for coordinating blob announcements
// without duplicating the underlying DHT reannouncement logic
type DefaultDHTAnnouncer struct {
	dhtNode DHTNode
	mu      sync.Mutex
}

// NewDefaultDHTAnnouncer creates a new DefaultDHTAnnouncer
// This provides a clean interface for blob announcement coordination
func NewDefaultDHTAnnouncer(dhtNode DHTNode) *DefaultDHTAnnouncer {
	return &DefaultDHTAnnouncer{
		dhtNode: dhtNode,
	}
}

// AnnounceBlob announces a blob hash to the DHT network
// This delegates to the underlying DHT implementation which handles
// the actual announcement and reannouncement logic
func (s *DefaultDHTAnnouncer) AnnounceBlob(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate input
	if hash == "" {
		return liblbryerrors.ErrInvalidHash
	}

	// Parse the hash string to bits.Bitmap
	// Note: This assumes ParseHashFromString is available in the same package
	// or that the DHTNode can handle string hashes directly
	hashBitmap, err := ParseHashFromString(hash)
	if err != nil {
		return err
	}

	s.dhtNode.Add(hashBitmap)
	return nil
}

// RemoveBlob removes a blob hash from DHT announcements
// This delegates to the underlying DHT implementation
func (s *DefaultDHTAnnouncer) RemoveBlob(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Validate input
	if hash == "" {
		return liblbryerrors.ErrInvalidHash
	}

	// Parse the hash string to bits.Bitmap
	// The DHTNode is expected to handle string hashes directly
	hashBitmap, err := ParseHashFromString(hash)
	if err != nil {
		return err
	}

	s.dhtNode.Remove(hashBitmap)
	return nil
}
