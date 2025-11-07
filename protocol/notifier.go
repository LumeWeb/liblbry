package protocol

import (
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// Notification types
const (
	NOTIFY_BLOB_ADDED   = "blob_added"
	NOTIFY_BLOB_REMOVED = "blob_removed"
)

// Notifier interface for handling notifications
type Notifier interface {
	Notify(id string, data string) error
}

// GroupNotifier implements Notifier and can hold multiple notifiers
type GroupNotifier struct {
	notifiers []Notifier
}

// NewGroupNotifier creates a new GroupNotifier
func NewGroupNotifier() *GroupNotifier {
	return &GroupNotifier{
		notifiers: make([]Notifier, 0),
	}
}

// AddNotifier adds a notifier to the group
func (gn *GroupNotifier) AddNotifier(notifier Notifier) {
	gn.notifiers = append(gn.notifiers, notifier)
}

// Notify sends notification to all notifiers in the group
func (gn *GroupNotifier) Notify(id string, data string) error {
	for _, notifier := range gn.notifiers {
		if err := notifier.Notify(id, data); err != nil {
			return err
		}
	}
	return nil
}

// NotifyBlob is a helper function that handles blob notifications with proper error handling and logging
// It converts LBRY hash to multihash for notification, with fallback to original hash on conversion errors
func NotifyBlob(notifier Notifier, logger *zap.Logger, notificationType, blobHash string) {
	if notifier == nil {
		return
	}

	// Convert LBRY hash to multihash for notification
	multihash, err := stream.ToMultihash(blobHash)
	if err != nil {
		logger.Warn("Failed to convert blob hash to multihash for notification",
			zap.String("blobHash", blobHash),
			zap.Error(err))
		// Continue with notification using original hash as fallback
		multihash = blobHash
	}

	if err := notifier.Notify(notificationType, multihash); err != nil {
		action := "addition"
		if notificationType == NOTIFY_BLOB_REMOVED {
			action = "removal"
		}
		logger.Warn("Failed to notify about blob "+action,
			zap.String("multihash", multihash),
			zap.String("hash", blobHash),
			zap.Error(err))
	}
}
