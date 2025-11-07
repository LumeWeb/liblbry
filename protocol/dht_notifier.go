package protocol

import (
	"fmt"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// DHTNotifier implements the Notifier interface for DHT announcements
type DHTNotifier struct {
	dhtAnnouncer DHTAnnouncer
	logger       *zap.Logger
}

// NewDHTNotifier creates a new DHT notifier
func NewDHTNotifier(dhtAnnouncer DHTAnnouncer, logger *zap.Logger) *DHTNotifier {
	return &DHTNotifier{
		dhtAnnouncer: dhtAnnouncer,
		logger:       logger,
	}
}

// Notify handles notifications for DHT announcements
func (n *DHTNotifier) Notify(id string, data string) error {
	switch id {
	case NOTIFY_BLOB_ADDED:
		return n.handleBlobAdded(data)
	case NOTIFY_BLOB_REMOVED:
		return n.handleBlobRemoved(data)
	default:
		n.logger.Debug("Unknown notification type", zap.String("id", id))
		return nil
	}
}

// handleBlobAdded processes blob added notifications
func (n *DHTNotifier) handleBlobAdded(multihash string) error {
	// Handle empty multihash case
	if multihash == "" {
		n.logger.Warn("Empty multihash provided")
		return nil
	}

	// Convert multihash to LBRY hash for logging purposes
	lbryHash, err := stream.FromMultihash(multihash)
	if err != nil {
		n.logger.Error("Failed to convert multihash to LBRY hash", zap.String("multihash", multihash), zap.Error(err))
		return fmt.Errorf("failed to convert multihash to LBRY hash: %w", err)
	}

	if lbryHash == "" {
		n.logger.Warn("Empty LBRY hash after conversion", zap.String("multihash", multihash))
		return fmt.Errorf("empty LBRY hash after conversion from multihash")
	}

	n.logger.Debug("Announcing blob to DHT", zap.String("multihash", multihash), zap.String("lbry_hash", lbryHash))

	// Announce the blob to DHT using the multihash directly
	// The DHT announcer expects the actual hash format it can work with
	err = n.dhtAnnouncer.AnnounceBlob(multihash)
	if err != nil {
		n.logger.Error("Failed to announce blob to DHT", zap.String("multihash", multihash), zap.String("lbry_hash", lbryHash), zap.Error(err))
		return fmt.Errorf("failed to announce blob to DHT: %w", err)
	}

	n.logger.Debug("Successfully announced blob to DHT", zap.String("multihash", multihash), zap.String("lbry_hash", lbryHash))
	return nil
}

// handleBlobRemoved processes blob removed notifications
func (n *DHTNotifier) handleBlobRemoved(multihash string) error {
	// Handle empty multihash case
	if multihash == "" {
		n.logger.Warn("Empty multihash provided")
		return nil
	}

	// Convert multihash to LBRY hash for logging purposes
	lbryHash, err := stream.FromMultihash(multihash)
	if err != nil {
		n.logger.Error("Failed to convert multihash to LBRY hash", zap.String("multihash", multihash), zap.Error(err))
		return fmt.Errorf("failed to convert multihash to LBRY hash: %w", err)
	}

	if lbryHash == "" {
		n.logger.Warn("Empty LBRY hash after conversion", zap.String("multihash", multihash))
		return fmt.Errorf("empty LBRY hash after conversion from multihash")
	}

	n.logger.Debug("Removing blob from DHT", zap.String("multihash", multihash), zap.String("lbry_hash", lbryHash))

	// Remove the blob from DHT using the multihash directly
	// The DHT announcer expects the actual hash format it can work with
	err = n.dhtAnnouncer.RemoveBlob(multihash)
	if err != nil {
		n.logger.Error("Failed to remove blob from DHT", zap.String("multihash", multihash), zap.String("lbry_hash", lbryHash), zap.Error(err))
		return fmt.Errorf("failed to remove blob from DHT: %w", err)
	}

	n.logger.Debug("Successfully removed blob from DHT", zap.String("multihash", multihash), zap.String("lbry_hash", lbryHash))
	return nil
}
