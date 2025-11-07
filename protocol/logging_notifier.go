package protocol

import (
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// LoggingNotifier implements the Notifier interface for logging notifications
type LoggingNotifier struct {
	logger *zap.Logger
}

// NewLoggingNotifier creates a new LoggingNotifier with the specified logger
func NewLoggingNotifier(logger *zap.Logger) *LoggingNotifier {
	return &LoggingNotifier{
		logger: logger,
	}
}

// Notify handles notifications and logs appropriate messages
func (n *LoggingNotifier) Notify(id string, data string) error {
	// Handle different notification types
	switch id {
	case NOTIFY_BLOB_ADDED:
		// Convert multihash to LBRY hash for logging
		lbryHash, err := stream.FromMultihash(data)
		if err != nil {
			n.logger.Info("Blob added to storage",
				zap.String("multihash", data))
		} else {
			n.logger.Info("Blob added to storage",
				zap.String("multihash", data),
				zap.String("lbry_hash", lbryHash))
		}
	case NOTIFY_BLOB_REMOVED:
		// Convert multihash to LBRY hash for logging
		lbryHash, err := stream.FromMultihash(data)
		if err != nil {
			n.logger.Info("Blob removed from storage",
				zap.String("multihash", data))
		} else {
			n.logger.Info("Blob removed from storage",
				zap.String("multihash", data),
				zap.String("lbry_hash", lbryHash))
		}
	default:
		n.logger.Info("Unknown notification type",
			zap.String("type", id),
			zap.String("multihash", data))
	}

	return nil
}
