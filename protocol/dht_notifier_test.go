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

const testMultihash = "bafksamcuqxgjwm3fwqyf362oqm36bjmyuv2pqjbl6fzityg5nqqkhtkeuce54fvljkzqr5r6isyrodvv6ukq"

// Helper function to create a test DHT notifier
func createTestNotifier(t *testing.T, mockAnnouncer *mocks.MockDHTAnnouncer) *DHTNotifier {
	logger := zaptest.NewLogger(t)
	return NewDHTNotifier(mockAnnouncer, logger)
}

// Helper function to create a test DHT notifier with custom logger
func createTestNotifierWithLogger(t *testing.T, mockAnnouncer *mocks.MockDHTAnnouncer, logger *zap.Logger) *DHTNotifier {
	return NewDHTNotifier(mockAnnouncer, logger)
}


// Helper function to create a debug logger
func createDebugLogger(t *testing.T) *zap.Logger {
	core, _ := observer.New(zapcore.DebugLevel)
	return zap.New(core)
}

// TestDHTNotifier_NewDHTNotifier tests the creation of a new DHT notifier
func TestDHTNotifier_NewDHTNotifier(t *testing.T) {
	t.Parallel()

	// Create a mock DHT announcer
	mockAnnouncer := mocks.NewMockDHTAnnouncer(t)

	// Create the DHT notifier
	notifier := createTestNotifier(t, mockAnnouncer)

	// Verify the notifier was created correctly
	assert.NotNil(t, notifier)
	assert.Equal(t, mockAnnouncer, notifier.dhtAnnouncer)
	assert.NotNil(t, notifier.logger)
	assert.IsType(t, &zap.Logger{}, notifier.logger)
}

// TestDHTNotifier_Notify_BlobAdded tests successful blob added notification
func TestDHTNotifier_Notify_BlobAdded(t *testing.T) {
	t.Parallel()

	// Create a mock DHT announcer
	mockAnnouncer := mocks.NewMockDHTAnnouncer(t)
	mockAnnouncer.EXPECT().AnnounceBlob(testMultihash).Return(nil)

	// Create the DHT notifier
	notifier := createTestNotifier(t, mockAnnouncer)

	// Test successful notification
	err := notifier.Notify(NOTIFY_BLOB_ADDED, testMultihash)
	assert.NoError(t, err)
}

// TestDHTNotifier_Notify_BlobRemoved tests successful blob removed notification
func TestDHTNotifier_Notify_BlobRemoved(t *testing.T) {
	t.Parallel()

	// Create a mock DHT announcer
	mockAnnouncer := mocks.NewMockDHTAnnouncer(t)
	mockAnnouncer.EXPECT().RemoveBlob(testMultihash).Return(nil)

	// Create the DHT notifier
	notifier := createTestNotifier(t, mockAnnouncer)

	// Test successful notification
	err := notifier.Notify(NOTIFY_BLOB_REMOVED, testMultihash)
	assert.NoError(t, err)
}

// TestDHTNotifier_Notify_UnknownType tests handling of unknown notification types
func TestDHTNotifier_Notify_UnknownType(t *testing.T) {
	t.Parallel()

	// Create a mock DHT announcer
	mockAnnouncer := mocks.NewMockDHTAnnouncer(t)

	// Create the DHT notifier
	notifier := createTestNotifier(t, mockAnnouncer)

	// Test unknown notification type
	err := notifier.Notify("unknown_type", testMultihash)
	assert.NoError(t, err)
}

// TestDHTNotifier_HandleBlobAdded_ErrorCases consolidates error scenarios for blob added
func TestDHTNotifier_HandleBlobAdded_ErrorCases(t *testing.T) {
	t.Parallel()

	// Test case 1: Failed multihash to LBRY hash conversion
	t.Run("Failed multihash conversion", func(t *testing.T) {
		t.Parallel()

		// Create a mock DHT announcer
		mockAnnouncer := mocks.NewMockDHTAnnouncer(t)

		// Create the DHT notifier
		notifier := createTestNotifier(t, mockAnnouncer)

		// Test with invalid multihash that causes conversion error
		err := notifier.handleBlobAdded("invalid_hash")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to convert multihash to LBRY hash")
	})

	// Test case 2: Empty multihash
	t.Run("Empty multihash", func(t *testing.T) {
		t.Parallel()

		// Create a mock DHT announcer
		mockAnnouncer := mocks.NewMockDHTAnnouncer(t)

		// Create the DHT notifier
		notifier := createTestNotifier(t, mockAnnouncer)

		// Test with empty multihash string
		err := notifier.handleBlobAdded("")
		assert.NoError(t, err) // Should not error, just log warning
	})

	// Test case 3: DHT announcer announce failure
	t.Run("DHT announcer announce failure", func(t *testing.T) {
		t.Parallel()

		// Create a mock DHT announcer
		mockAnnouncer := mocks.NewMockDHTAnnouncer(t)
		mockAnnouncer.EXPECT().AnnounceBlob(testMultihash).Return(errors.New("announce failed"))

		// Create the DHT notifier
		notifier := createTestNotifier(t, mockAnnouncer)

		// Test with announcer failure
		err := notifier.handleBlobAdded(testMultihash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to announce blob to DHT")
	})
}

// TestDHTNotifier_HandleBlobRemoved_ErrorCases consolidates error scenarios for blob removed
func TestDHTNotifier_HandleBlobRemoved_ErrorCases(t *testing.T) {
	t.Parallel()

	// Test case 1: Failed multihash to LBRY hash conversion
	t.Run("Failed multihash conversion", func(t *testing.T) {
		t.Parallel()

		// Create a mock DHT announcer
		mockAnnouncer := mocks.NewMockDHTAnnouncer(t)

		// Create the DHT notifier
		notifier := createTestNotifier(t, mockAnnouncer)

		// Test with invalid multihash that causes conversion error
		err := notifier.handleBlobRemoved("invalid_hash")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to convert multihash to LBRY hash")
	})

	// Test case 2: Empty multihash
	t.Run("Empty multihash", func(t *testing.T) {
		t.Parallel()

		// Create a mock DHT announcer
		mockAnnouncer := mocks.NewMockDHTAnnouncer(t)

		// Create the DHT notifier
		notifier := createTestNotifier(t, mockAnnouncer)

		// Test with empty multihash string
		err := notifier.handleBlobRemoved("")
		assert.NoError(t, err) // Should not error, just log warning
	})

	// Test case 3: DHT announcer remove failure
	t.Run("DHT announcer remove failure", func(t *testing.T) {
		t.Parallel()

		// Create a mock DHT announcer
		mockAnnouncer := mocks.NewMockDHTAnnouncer(t)
		mockAnnouncer.EXPECT().RemoveBlob(testMultihash).Return(errors.New("remove failed"))

		// Create the DHT notifier
		notifier := createTestNotifier(t, mockAnnouncer)

		// Test with announcer failure
		err := notifier.handleBlobRemoved(testMultihash)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove blob from DHT")
	})
}
