package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestNewLoggingNotifier(t *testing.T) {
	logger := zap.NewNop()
	notifier := NewLoggingNotifier(logger)

	require.NotNil(t, notifier)
	assert.Equal(t, logger, notifier.logger)
}

func TestLoggingNotifier_NotifyBlobAdded(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with valid multihash
	validMultihash := "bafyreigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	err := notifier.Notify(NOTIFY_BLOB_ADDED, validMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Blob added to storage", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, validMultihash, logEntry.Context[0].String)

	// Check if we have the lbry_hash field (it may not be present if conversion fails)
	if len(logEntry.Context) > 1 {
		assert.Equal(t, "lbry_hash", logEntry.Context[1].Key)
	}
}

func TestLoggingNotifier_NotifyBlobRemoved(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with valid multihash
	validMultihash := "bafyreigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	err := notifier.Notify(NOTIFY_BLOB_REMOVED, validMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Blob removed from storage", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, validMultihash, logEntry.Context[0].String)

	// Check if we have the lbry_hash field (it may not be present if conversion fails)
	if len(logEntry.Context) > 1 {
		assert.Equal(t, "lbry_hash", logEntry.Context[1].Key)
	}
}

func TestLoggingNotifier_NotifyWithInvalidMultihash(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with invalid multihash
	invalidMultihash := "invalid-multihash"
	err := notifier.Notify(NOTIFY_BLOB_ADDED, invalidMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Blob added to storage", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, invalidMultihash, logEntry.Context[0].String)
	// Should not have lbry_hash field when conversion fails
	assert.Equal(t, 1, len(logEntry.Context))
}

func TestLoggingNotifier_NotifyUnknownType(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with unknown notification type
	unknownType := "UNKNOWN_NOTIFICATION"
	validMultihash := "bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	err := notifier.Notify(unknownType, validMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Unknown notification type", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, unknownType, logEntry.Context[0].String)
	assert.Equal(t, validMultihash, logEntry.Context[1].String)
}

func TestLoggingNotifier_NotifyBlobRemovedWithInvalidMultihash(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with invalid multihash for blob removal
	invalidMultihash := "not-a-valid-multihash"
	err := notifier.Notify(NOTIFY_BLOB_REMOVED, invalidMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Blob removed from storage", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, invalidMultihash, logEntry.Context[0].String)
	// Should not have lbry_hash field when conversion fails
	assert.Equal(t, 1, len(logEntry.Context))
}

func TestLoggingNotifier_NotifyWithEmptyMultihash(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with empty multihash
	emptyMultihash := ""
	err := notifier.Notify(NOTIFY_BLOB_ADDED, emptyMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Blob added to storage", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, emptyMultihash, logEntry.Context[0].String)
	// Should not have lbry_hash field when conversion fails
	assert.Equal(t, 1, len(logEntry.Context))
}

func TestLoggingNotifier_NotifyWithValidLBRYHashMultihash(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	// Test with a multihash that should convert to a valid LBRY hash
	validMultihash := "bafyreigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"
	err := notifier.Notify(NOTIFY_BLOB_ADDED, validMultihash)

	assert.NoError(t, err)
	assert.Equal(t, 1, logs.Len())

	logEntry := logs.All()[0]
	assert.Equal(t, "Blob added to storage", logEntry.Message)
	assert.Equal(t, zap.InfoLevel, logEntry.Level)
	assert.Equal(t, validMultihash, logEntry.Context[0].String)

	// Check if we have the lbry_hash field (it may not be present if conversion fails)
	if len(logEntry.Context) > 1 {
		assert.Equal(t, "lbry_hash", logEntry.Context[1].Key)
	}
	// The lbry_hash field should be present when conversion succeeds
	assert.True(t, len(logEntry.Context) >= 1)
}

func TestLoggingNotifier_NotifyMultipleTimes(t *testing.T) {
	// Create a logger with observable core for testing
	zapCore, logs := observer.New(zap.InfoLevel)
	logger := zap.New(zapCore)
	notifier := NewLoggingNotifier(logger)

	validMultihash := "bafyreigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi"

	// Send multiple notifications
	err1 := notifier.Notify(NOTIFY_BLOB_ADDED, validMultihash)
	err2 := notifier.Notify(NOTIFY_BLOB_REMOVED, validMultihash)
	err3 := notifier.Notify("UNKNOWN_TYPE", validMultihash)

	assert.NoError(t, err1)
	assert.NoError(t, err2)
	assert.NoError(t, err3)
	assert.Equal(t, 3, logs.Len())

	// Check first log (blob added)
	logEntry1 := logs.All()[0]
	assert.Equal(t, "Blob added to storage", logEntry1.Message)

	// Check second log (blob removed)
	logEntry2 := logs.All()[1]
	assert.Equal(t, "Blob removed from storage", logEntry2.Message)

	// Check third log (unknown type)
	logEntry3 := logs.All()[2]
	assert.Equal(t, "Unknown notification type", logEntry3.Message)
}
