package protocol

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	mocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest"
	"go.uber.org/zap/zaptest/observer"
)

// Helper functions for test setup

// createTestLogger creates a logger with observer for testing
func createTestLogger(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	return logger, logs
}

// setupGroupNotifierWithMocks creates a group notifier with specified number of mock notifiers
func setupGroupNotifierWithMocks(t *testing.T, count int) (*GroupNotifier, []*mocks.MockNotifier) {
	groupNotifier := NewGroupNotifier()
	mockNotifiers := make([]*mocks.MockNotifier, count)

	for i := 0; i < count; i++ {
		mockNotifiers[i] = mocks.NewMockNotifier(t)
		groupNotifier.AddNotifier(mockNotifiers[i])
	}

	return groupNotifier, mockNotifiers
}

// Constants for test data
const validTestHash = "68c0ff52fca66bc20c736e49967760d6378ce73aaf4b0a870f1c2142455629ab50dc49dae0b03c56a9bff7f270a2edf3"
const expectedMultihash = "bafksamdiyd7vf7fgnpbay43ojglhoygwg6gooovpjmfiody4efbekvrjvninyso24cydyvvjx737e4fc5xzq"

// TestNewGroupNotifier verifies the creation of a new GroupNotifier instance
func TestNewGroupNotifier(t *testing.T) {
	t.Parallel()

	groupNotifier := NewGroupNotifier()
	assert.NotNil(t, groupNotifier)
	assert.NotNil(t, groupNotifier.notifiers)
	assert.Len(t, groupNotifier.notifiers, 0)
}

// TestGroupNotifierAddNotifier verifies adding notifiers to the group
func TestGroupNotifierAddNotifier(t *testing.T) {
	t.Parallel()

	groupNotifier := NewGroupNotifier()

	// Add first notifier
	mockNotifier1 := mocks.NewMockNotifier(t)
	groupNotifier.AddNotifier(mockNotifier1)

	assert.Len(t, groupNotifier.notifiers, 1)
	assert.Contains(t, groupNotifier.notifiers, mockNotifier1)

	// Add second notifier
	mockNotifier2 := mocks.NewMockNotifier(t)
	groupNotifier.AddNotifier(mockNotifier2)

	assert.Len(t, groupNotifier.notifiers, 2)
	assert.Contains(t, groupNotifier.notifiers, mockNotifier1)
	assert.Contains(t, groupNotifier.notifiers, mockNotifier2)
}

// TestGroupNotifierNotify verifies that all notifiers in the group receive notifications
func TestGroupNotifierNotify(t *testing.T) {
	t.Parallel()

	groupNotifier, mockNotifiers := setupGroupNotifierWithMocks(t, 2)
	mockNotifier1 := mockNotifiers[0]
	mockNotifier2 := mockNotifiers[1]

	// Setup expectations
	mockNotifier1.EXPECT().Notify("test_notification", "test_data").Return(nil)
	mockNotifier2.EXPECT().Notify("test_notification", "test_data").Return(nil)

	// Test notification
	id := "test_notification"
	data := "test_data"

	err := groupNotifier.Notify(id, data)

	// Verify no error occurred
	assert.NoError(t, err)
}

// TestGroupNotifierNotifyWithError verifies error handling when one notifier fails
func TestGroupNotifierNotifyWithError(t *testing.T) {
	t.Parallel()

	groupNotifier, mockNotifiers := setupGroupNotifierWithMocks(t, 2)
	mockNotifier1 := mockNotifiers[0]
	mockNotifier2 := mockNotifiers[1]

	// Setup expectations
	mockNotifier1.EXPECT().Notify("test_notification", "test_data").Return(nil)
	mockNotifier2.EXPECT().Notify("test_notification", "test_data").Return(errors.New("mock notifier error"))

	// Test notification with error
	id := "test_notification"
	data := "test_data"

	err := groupNotifier.Notify(id, data)

	// Verify error occurred
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mock notifier error")
}

// TestGroupNotifierNotifyBestEffort verifies that all notifiers are called even when some fail
func TestGroupNotifierNotifyBestEffort(t *testing.T) {
	t.Parallel()

	groupNotifier, mockNotifiers := setupGroupNotifierWithMocks(t, 3)
	mockNotifier1 := mockNotifiers[0]
	mockNotifier2 := mockNotifiers[1]
	mockNotifier3 := mockNotifiers[2]

	// Setup expectations - first notifier fails, others succeed
	mockNotifier1.EXPECT().Notify("test_notification", "test_data").Return(errors.New("first notifier error"))
	mockNotifier2.EXPECT().Notify("test_notification", "test_data").Return(nil)
	mockNotifier3.EXPECT().Notify("test_notification", "test_data").Return(nil)

	// Test notification
	id := "test_notification"
	data := "test_data"

	err := groupNotifier.Notify(id, data)

	// Verify first error is returned but all notifiers were called
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "first notifier error")
}

// TestGroupNotifierNotifyMultipleErrors verifies that the first error is returned when multiple notifiers fail
func TestGroupNotifierNotifyMultipleErrors(t *testing.T) {
	t.Parallel()

	groupNotifier, mockNotifiers := setupGroupNotifierWithMocks(t, 3)
	mockNotifier1 := mockNotifiers[0]
	mockNotifier2 := mockNotifiers[1]
	mockNotifier3 := mockNotifiers[2]

	// Setup expectations - all notifiers fail
	mockNotifier1.EXPECT().Notify("test_notification", "test_data").Return(errors.New("first error"))
	mockNotifier2.EXPECT().Notify("test_notification", "test_data").Return(errors.New("second error"))
	mockNotifier3.EXPECT().Notify("test_notification", "test_data").Return(errors.New("third error"))

	// Test notification
	id := "test_notification"
	data := "test_data"

	err := groupNotifier.Notify(id, data)

	// Verify first error is returned
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "first error")
	assert.NotContains(t, err.Error(), "second error")
	assert.NotContains(t, err.Error(), "third error")
}

// TestGroupNotifierEmpty verifies behavior with empty notifier list
func TestGroupNotifierEmpty(t *testing.T) {
	t.Parallel()

	groupNotifier := NewGroupNotifier()

	// Test notification with no notifiers
	id := "test_notification"
	data := "test_data"

	err := groupNotifier.Notify(id, data)

	// Should succeed with no notifiers
	assert.NoError(t, err)

	// Verify no calls were made
	assert.Len(t, groupNotifier.notifiers, 0)
}

// TestGroupNotifierMultipleCalls verifies multiple notification calls to the group
func TestGroupNotifierMultipleCalls(t *testing.T) {
	t.Parallel()

	groupNotifier, mockNotifiers := setupGroupNotifierWithMocks(t, 2)
	mockNotifier1 := mockNotifiers[0]
	mockNotifier2 := mockNotifiers[1]

	// Setup expectations
	mockNotifier1.EXPECT().Notify("notification_0", "data_0").Return(nil)
	mockNotifier2.EXPECT().Notify("notification_0", "data_0").Return(nil)
	mockNotifier1.EXPECT().Notify("notification_1", "data_1").Return(nil)
	mockNotifier2.EXPECT().Notify("notification_1", "data_1").Return(nil)
	mockNotifier1.EXPECT().Notify("notification_2", "data_2").Return(nil)
	mockNotifier2.EXPECT().Notify("notification_2", "data_2").Return(nil)

	// Test multiple notifications
	for i := 0; i < 3; i++ {
		id := "notification_" + string(rune(i+'0'))
		data := "data_" + string(rune(i+'0'))

		err := groupNotifier.Notify(id, data)
		assert.NoError(t, err)
	}
}

// TestNotifyBlob_NilNotifier tests that nil notifier returns early without error
func TestNotifyBlob_NilNotifier(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	blobHash := "1234567890abcdef1234567890abcdef12345678"

	// Should not panic and should return immediately
	NotifyBlob(nil, logger, NOTIFY_BLOB_ADDED, blobHash)
}

// TestNotifyBlob_BlobAddedSuccess tests successful blob added notification with valid hash conversion
func TestNotifyBlob_BlobAddedSuccess(t *testing.T) {
	t.Parallel()

	// Create a mock notifier
	mockNotifier := mocks.NewMockNotifier(t)

	// Create a logger with observer to capture logs
	logger, logs := createTestLogger(t)

	blobHash := validTestHash

	// Setup expectation - should be called with multihash
	mockNotifier.EXPECT().Notify(NOTIFY_BLOB_ADDED, expectedMultihash).Return(nil)

	// Call NotifyBlob
	NotifyBlob(mockNotifier, logger, NOTIFY_BLOB_ADDED, blobHash)

	// Verify no error logs
	assert.Empty(t, logs.All())
}

// TestNotifyBlob_BlobRemovedSuccess tests successful blob removed notification with valid hash conversion
func TestNotifyBlob_BlobRemovedSuccess(t *testing.T) {
	t.Parallel()

	// Create a mock notifier
	mockNotifier := mocks.NewMockNotifier(t)

	// Create a logger with observer to capture logs
	logger, logs := createTestLogger(t)

	blobHash := validTestHash

	// Setup expectation - should be called with multihash
	mockNotifier.EXPECT().Notify(NOTIFY_BLOB_REMOVED, expectedMultihash).Return(nil)

	// Call NotifyBlob
	NotifyBlob(mockNotifier, logger, NOTIFY_BLOB_REMOVED, blobHash)

	// Verify no error logs
	assert.Empty(t, logs.All())
}

// TestNotifyBlob_HashConversionError tests hash conversion error with fallback
func TestNotifyBlob_HashConversionError(t *testing.T) {
	t.Parallel()

	// Create a mock notifier
	mockNotifier := mocks.NewMockNotifier(t)

	// Create a logger with observer to capture logs
	logger, logs := createTestLogger(t)

	// Use an invalid hash that will cause conversion to fail
	invalidBlobHash := "invalid_hash"

	// Setup expectation - should be called with original hash as fallback
	mockNotifier.EXPECT().Notify(NOTIFY_BLOB_ADDED, invalidBlobHash).Return(nil)

	// Call NotifyBlob
	NotifyBlob(mockNotifier, logger, NOTIFY_BLOB_ADDED, invalidBlobHash)

	// Verify warning log about conversion failure
	warnLogs := logs.FilterLevelExact(zap.WarnLevel).All()
	assert.Len(t, warnLogs, 1)
	assert.Contains(t, warnLogs[0].Message, "Failed to convert blob hash to multihash for notification")
	assert.Equal(t, invalidBlobHash, warnLogs[0].Context[0].String)
}

// TestNotifyBlob_NotifierError tests notifier.Notify error handling
func TestNotifyBlob_NotifierError(t *testing.T) {
	t.Parallel()

	// Create a mock notifier
	mockNotifier := mocks.NewMockNotifier(t)

	// Create a logger with observer to capture logs
	logger, logs := createTestLogger(t)

	blobHash := validTestHash
	notifierError := errors.New("notifier failed")

	// Setup expectation - notifier returns error
	mockNotifier.EXPECT().Notify(NOTIFY_BLOB_ADDED, expectedMultihash).Return(notifierError)

	// Call NotifyBlob
	NotifyBlob(mockNotifier, logger, NOTIFY_BLOB_ADDED, blobHash)

	// Verify warning log about notification failure
	warnLogs := logs.FilterLevelExact(zap.WarnLevel).All()
	assert.Len(t, warnLogs, 1)
	assert.Contains(t, warnLogs[0].Message, "Failed to notify about blob addition")
	assert.Equal(t, expectedMultihash, warnLogs[0].Context[0].String)
	assert.Equal(t, blobHash, warnLogs[0].Context[1].String)
	assert.Equal(t, notifierError, warnLogs[0].Context[2].Interface)
}

// TestNotifyBlob_EmptyHash tests empty blob hash handling
func TestNotifyBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	// Create a mock notifier
	mockNotifier := mocks.NewMockNotifier(t)

	// Create a logger with observer to capture logs
	logger, logs := createTestLogger(t)

	emptyBlobHash := ""

	// Setup expectation - should be called with empty hash
	mockNotifier.EXPECT().Notify(NOTIFY_BLOB_ADDED, emptyBlobHash).Return(nil)

	// Call NotifyBlob
	NotifyBlob(mockNotifier, logger, NOTIFY_BLOB_ADDED, emptyBlobHash)

	// Verify warning log about conversion failure for empty hash
	warnLogs := logs.FilterLevelExact(zap.WarnLevel).All()
	assert.Len(t, warnLogs, 1)
	assert.Contains(t, warnLogs[0].Message, "Failed to convert blob hash to multihash for notification")
	assert.Equal(t, emptyBlobHash, warnLogs[0].Context[0].String)
}

// TestNotifyBlob_InvalidHash tests invalid blob hash handling
func TestNotifyBlob_InvalidHash(t *testing.T) {
	t.Parallel()

	// Create a mock notifier
	mockNotifier := mocks.NewMockNotifier(t)

	// Create a logger with observer to capture logs
	logger, logs := createTestLogger(t)

	// Use a hash that's wrong length (should be 96 hex chars for SHA-384)
	invalidLengthHash := "123"

	// Setup expectation - should be called with original hash as fallback
	mockNotifier.EXPECT().Notify(NOTIFY_BLOB_ADDED, invalidLengthHash).Return(nil)

	// Call NotifyBlob
	NotifyBlob(mockNotifier, logger, NOTIFY_BLOB_ADDED, invalidLengthHash)

	// Verify warning log about conversion failure
	warnLogs := logs.FilterLevelExact(zap.WarnLevel).All()
	assert.Len(t, warnLogs, 1)
	assert.Contains(t, warnLogs[0].Message, "Failed to convert blob hash to multihash for notification")
	assert.Equal(t, invalidLengthHash, warnLogs[0].Context[0].String)
}
