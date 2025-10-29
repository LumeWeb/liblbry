package e2e

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob"
	lbrytesting "go.lumeweb.com/liblbry/internal/testing"
	"go.lumeweb.com/liblbry/protocol"
	"go.uber.org/zap/zaptest"
)

const (
	defaultPeerAddr         = "s1.lbry.network:5567"
	knownSDHash             = "acc6adf8b4f10dcddffc5c2ca87dbd9cb3a2664564695ac7aaab038193ff14a280cc3d4ebae55c71d0b885a7316d0137"
	invalidDownloadHash     = "invalidhash"
	nonExistentDownloadHash = "000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	malformedDownloadHash   = "xyz123"
	emptyDownloadHash       = ""
)

// createTestClient creates a new PeerClient with standard configuration
func createTestClient(t *testing.T, timeout time.Duration) protocol.PeerClient {
	t.Helper()
	logger := zaptest.NewLogger(t)
	client := protocol.NewPeerClient(
		protocol.WithClientLogger(logger),
		protocol.WithClientTimeout(timeout),
	)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return client
}

// createConnectedClient creates a new PeerClient connected to the reflector
func createConnectedClient(t *testing.T, timeout time.Duration) protocol.PeerClient {
	t.Helper()
	client := createTestClient(t, timeout)
	connectToPeer(t, client)
	return client
}

// connectToPeer connects a client to the public peer server.
// These are end-to-end integration tests that require network access to
// s1.lbry.network:5567 (configurable via LIBLBRY_E2E_PEER_ADDR env var).
// Tests may fail if the reflector service is unavailable.
func connectToPeer(t *testing.T, client protocol.PeerClient) {
	t.Helper()
	addr := defaultPeerAddr
	if v := strings.TrimSpace(os.Getenv("LIBLBRY_E2E_PEER_ADDR")); v != "" {
		addr = v
	}
	t.Logf("Connecting to reflector server at %s", addr)
	ctx := context.Background()
	err := client.Connect(ctx, addr)
	require.NoError(t, err, "Failed to connect to reflector server")
}

func TestPeerClientIntegration(t *testing.T) {
	ctx := context.Background()

	// Fetch known SD blob
	t.Run("FetchKnownBlob", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)
		t.Logf("Testing fetch of known SD blob: %s", knownSDHash)

		// Check if blob exists
		has, err := client.HasBlob(ctx, knownSDHash)
		require.NoError(t, err)
		require.True(t, has, "Known SD blob should be available")

		// Fetch the blob
		blobData, err := client.GetBlob(ctx, knownSDHash)
		require.NoError(t, err)
		require.NotEmpty(t, blobData, "Blob data should not be empty")

		// Verify hash matches
		hashBytes, err := blob.ComputeBlobHashBytes(blobData)
		require.NoError(t, err)
		require.Equal(t, knownSDHash, hex.EncodeToString(hashBytes), "Blob hash mismatch")
	})

	// Fetch stream
	t.Run("FetchStream", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)
		t.Logf("Testing fetch of stream with SD blob: %s", knownSDHash)

		// Fetch the stream
		stream, err := client.GetStream(ctx, knownSDHash)
		require.NoError(t, err)
		require.NotEmpty(t, stream, "Stream should not be empty")

		// Verify first blob is the SD blob
		hashBytes, err := blob.ComputeBlobHashBytes(stream[0])
		require.NoError(t, err)
		require.Equal(t, knownSDHash, hex.EncodeToString(hashBytes), "First blob should be the SD blob")
	})

	// Enhanced blob operations
	t.Run("EnhancedBlobOperations", func(t *testing.T) {
		client := createConnectedClient(t, 5*time.Second)

		// Test HasBlob with invalid hash
		t.Run("HasBlobInvalidHash", func(t *testing.T) {
			has, err := client.HasBlob(ctx, invalidDownloadHash)
			assertUploadValidationError(t, err, "Should return validation error for invalid hash")
			require.False(t, has, "Invalid hash should not be found")
		})

		// Test HasBlob with non-existent valid hash
		t.Run("HasBlobNonExistent", func(t *testing.T) {
			has, err := client.HasBlob(ctx, nonExistentDownloadHash)
			require.NoError(t, err)
			require.False(t, has, "Non-existent hash should not be found")
		})

		// Test GetBlob error handling with invalid hash
		t.Run("GetBlobInvalidHash", func(t *testing.T) {
			_, err := client.GetBlob(ctx, invalidDownloadHash)
			assertUploadValidationError(t, err, "Should return validation error for invalid hash")
		})

		// Test GetBlob error handling with non-existent hash
		t.Run("GetBlobNonExistent", func(t *testing.T) {
			_, err := client.GetBlob(ctx, nonExistentDownloadHash)
			if isNetworkError(err) {
				t.Skipf("Skipping test due to network error (likely timeout): %v", err)
			}
			assertBlobNotFoundError(t, err, "Expected blob not found error for non-existent hash")
		})
	})

	// Stream operations with real data validation
	t.Run("StreamOperations", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)

		// Test stream fetching and validation
		t.Run("FetchAndValidateStream", func(t *testing.T) {
			streamBlobs, err := client.GetStream(ctx, knownSDHash)
			require.NoError(t, err)
			require.NotEmpty(t, streamBlobs, "Stream should not be empty")

			// Validate SD blob
			sdBlob := streamBlobs[0]
			sdHashBytes, err := blob.ComputeBlobHashBytes(sdBlob)
			require.NoError(t, err)
			require.Equal(t, knownSDHash, hex.EncodeToString(sdHashBytes), "First blob should be the SD blob")

			// Validate that we can fetch each blob in the stream
			for i, blobData := range streamBlobs {
				hashBytes, err := blob.ComputeBlobHashBytes(blobData)
				require.NoError(t, err)

				// Verify we can check for the blob's existence
				has, err := client.HasBlob(ctx, hex.EncodeToString(hashBytes))
				require.NoError(t, err)
				require.True(t, has, "Blob %d should exist", i)

				// Verify we can fetch the blob
				fetchedBlob, err := client.GetBlob(ctx, hex.EncodeToString(hashBytes))
				require.NoError(t, err)
				require.Equal(t, blobData.ToBytes(), fetchedBlob, "Fetched blob %d should match original", i)
			}
		})
	})

	// Error handling and network failure tests
	t.Run("ErrorHandling", func(t *testing.T) {
		// Test connection to invalid address
		t.Run("InvalidAddressConnection", func(t *testing.T) {
			invalidClient := createTestClient(t, 5*time.Second)
			ctx := context.Background()
			err := invalidClient.Connect(ctx, "invalid.address:1234")
			require.Error(t, err, "Should fail to connect to invalid address")
			require.True(t, isNetworkError(err), "Error should be a network error")
		})
	})

	// Concurrent access tests
	t.Run("ConcurrentAccess", func(t *testing.T) {
		// Create multiple independent client instances to exercise true parallel IO
		createAdapter := func() lbrytesting.StoreOperations {
			client := createConnectedClient(t, 30*time.Second)
			return &PeerClientAdapter{client: client}
		}

		// Test concurrent Has operations using multiple client instances
		lbrytesting.TestConcurrentHasMultiple(t, createAdapter, knownSDHash, true, 10, 5)
	})

	// Context cancellation handling
	t.Run("ContextCancellation", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)

		// Test with context timeout
		t.Run("ContextTimeout", func(t *testing.T) {
			// Create a cancelled context to guarantee timeout
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately
			defer cancel()

			// Try to fetch a blob - should timeout
			_, err := client.GetBlob(ctx, knownSDHash)
			require.Error(t, err, "Should return error when context is cancelled")
			require.True(t, isNetworkError(err), "Cancellation should be classified as network error")
			require.True(t, errors.Is(err, context.Canceled), "Error should contain context canceled")

			// Try to check blob availability - should cancel
			_, err = client.HasBlob(ctx, knownSDHash)
			require.Error(t, err, "Should return error when context is canceled")
			require.True(t, isNetworkError(err), "Cancellation should be classified as network error")
			require.True(t, errors.Is(err, context.Canceled), "Error should contain context canceled")

			// Try to fetch a stream - should cancel
			_, err = client.GetStream(ctx, knownSDHash)
			require.Error(t, err, "Should return error when context is canceled")
			require.True(t, isNetworkError(err), "Cancellation should be classified as network error")
			require.True(t, errors.Is(err, context.Canceled), "Error should contain context canceled")
		})
	})

	// Large blob handling tests
	t.Run("LargeBlobHandling", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)

		// Test fetching known SD blob (which can be large)
		has, err := client.HasBlob(ctx, knownSDHash)
		require.NoError(t, err)
		require.True(t, has, "Large SD blob should be available")

		blobData, err := client.GetBlob(ctx, knownSDHash)
		require.NoError(t, err)
		require.NotEmpty(t, blobData, "Large blob data should not be empty")

		// Validate the blob
		hashBytes, err := blob.ComputeBlobHashBytes(blobData)
		require.NoError(t, err)
		require.Equal(t, knownSDHash, hex.EncodeToString(hashBytes), "Large blob hash should match")
	})

	// Malformed request handling tests
	t.Run("MalformedRequestHandling", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)

		// Test with empty hash
		t.Run("EmptyHash", func(t *testing.T) {
			has, err := client.HasBlob(ctx, emptyDownloadHash)
			assertUploadValidationError(t, err, "Should return validation error for empty hash")
			require.False(t, has, "Empty hash should not be found")

			_, err = client.GetBlob(ctx, emptyDownloadHash)
			assertUploadValidationError(t, err, "Should return validation error for empty hash")
		})

		// Test with malformed hex hash
		t.Run("MalformedHexHash", func(t *testing.T) {
			has, err := client.HasBlob(ctx, malformedDownloadHash)
			assertUploadValidationError(t, err, "Should return validation error for malformed hex hash")
			require.False(t, has, "Malformed hex hash should not be found")

			_, err = client.GetBlob(ctx, malformedDownloadHash)
			assertUploadValidationError(t, err, "Should return validation error for malformed hex hash")
		})
	})

	// Blob verification tests
	t.Run("BlobVerification", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)

		// Test that we can fetch and verify the known SD blob
		blobData, err := client.GetBlob(ctx, knownSDHash)
		require.NoError(t, err)
		require.NotEmpty(t, blobData, "Blob data should not be empty")

		// Verify the blob hash matches what we expect
		hashBytes, err := blob.ComputeBlobHashBytes(blobData)
		require.NoError(t, err)
		require.Equal(t, knownSDHash, hex.EncodeToString(hashBytes), "Blob hash should match expected value")

		// Test that we can verify blob existence
		has, err := client.HasBlob(ctx, knownSDHash)
		require.NoError(t, err)
		require.True(t, has, "Known blob should exist")
	})

	// Empty hash handling tests
	t.Run("EmptyHashHandling", func(t *testing.T) {
		client := createConnectedClient(t, 30*time.Second)

		// Test with empty string as hash
		has, err := client.HasBlob(ctx, emptyDownloadHash)
		assertUploadValidationError(t, err, "Should return validation error for empty hash")
		require.False(t, has, "Empty hash should not be found")

		// Test fetching empty hash
		_, err = client.GetBlob(ctx, emptyDownloadHash)
		assertUploadValidationError(t, err, "Should return validation error for empty hash")
	})
}

// PeerClientAdapter implements the StoreOperations interface for PeerClient
// Note: Put method is not supported by PeerClient and will return an error
type PeerClientAdapter struct {
	client protocol.PeerClient
}

// ErrPutNotSupported is returned when attempting to use Put operation
// which is not supported by PeerClient
var ErrPutNotSupported = errors.New("Put operation not supported by PeerClient")

func (a *PeerClientAdapter) Has(hash string) (bool, error) {
	ctx := context.Background()
	return a.client.HasBlob(ctx, hash)
}

func (a *PeerClientAdapter) Get(hash string) ([]byte, error) {
	ctx := context.Background()
	return a.client.GetBlob(ctx, hash)
}

func (a *PeerClientAdapter) Put(_ string, _ []byte) error {
	return ErrPutNotSupported
}
