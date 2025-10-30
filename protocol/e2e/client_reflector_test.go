package e2e

import (
	"crypto/rand"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/protocol"
	"go.uber.org/zap/zaptest"
)

const (
	defaultReflectorAddr = "s1.lbry.network:5566"
	malformedUploadHash  = "xyz123"
	emptyUploadHash      = ""
)

// createTestReflectorClient creates a new ReflectorClient with standard configuration
func createTestReflectorClient(t *testing.T, timeout time.Duration) protocol.ReflectorClient {
	t.Helper()
	logger := zaptest.NewLogger(t)
	client := protocol.NewReflectorClient(
		protocol.WithReflectorClientLogger(logger),
		protocol.WithReflectorClientTimeout(timeout),
	)
	t.Cleanup(func() {
		require.NoError(t, client.Close())
	})
	return client
}

// createConnectedReflectorClient creates a new ReflectorClient connected to the reflector
func createConnectedReflectorClient(t *testing.T, timeout time.Duration) protocol.ReflectorClient {
	t.Helper()
	client := createTestReflectorClient(t, timeout)
	connectToReflectorUpload(t, client)
	return client
}

// connectToReflectorUpload connects a client to the public reflector server.
// These are end-to-end integration tests that require network access to
// s1.lbry.network:5566 (configurable via LIBLBRY_E2E_REFLECTOR_ADDR env var).
// Tests may fail if the reflector service is unavailable.
func connectToReflectorUpload(t *testing.T, client protocol.ReflectorClient) {
	t.Helper()
	addr := defaultReflectorAddr
	if v := strings.TrimSpace(os.Getenv("LIBLBRY_E2E_REFLECTOR_ADDR")); v != "" {
		addr = v
	}
	t.Logf("Connecting to reflector server at %s", addr)
	err := client.Connect(addr)
	require.NoError(t, err, "Failed to connect to reflector server")
}

// createTestBlob creates a blob with test data
func createTestBlob(t *testing.T, testData []byte) (blob.Blob, string, []byte, []byte) {
	key := make([]byte, 16)
	iv := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, key)
	require.NoError(t, err)
	_, err = io.ReadFull(rand.Reader, iv)
	require.NoError(t, err)

	testBlob, err := blob.NewBlob(testData, key, iv)
	require.NoError(t, err)

	blobHash := testBlob.HashHex()
	return testBlob, blobHash, key, iv
}

// TestReflectorClientUpload tests basic blob upload functionality
func TestReflectorClientUpload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Test regular blob upload
	t.Run("SendBlob", func(t *testing.T) {
		client := createConnectedReflectorClient(t, 30*time.Second)

		testData := []byte("test blob data for e2e reflector")
		// Add a random number to make the test data unique for each run
		testData = append(testData, []byte(time.Now().String())...)
		testBlob, blobHash, _, _ := createTestBlob(t, testData)

		// Test SendBlob - this will likely return "blob already exists" error
		// since we're using a public reflector
		err := client.SendBlob(blobHash, testBlob.ToBytes())
		if err != nil {
			assertBlobExistsError(t, err, "SendBlob should return blob exists error or succeed")
		} else {
			// Upload succeeded
			t.Logf("Successfully uploaded blob %s", blobHash)
		}
	})

	// Test SD blob upload
	t.Run("SendSDBlob", func(t *testing.T) {
		client := createConnectedReflectorClient(t, 30*time.Second)

		// Create SD blob data (JSON format)
		sdData := []byte(`{"stream_type": "video", "blobs": []}`)
		sdBlob, sdBlobHash, _, _ := createTestBlob(t, sdData)

		// Test SendSDBlob
		err := client.SendSDBlob(sdBlobHash, sdBlob.ToBytes())
		if err != nil {
			assertBlobExistsError(t, err, "SendSDBlob should return blob exists error or succeed")
		} else {
			// Upload succeeded
			t.Logf("Successfully uploaded SD blob %s", sdBlobHash)
		}
	})
}

// TestReflectorClientConnectionManagement tests connection management features
func TestReflectorClientConnectionManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	client := createTestReflectorClient(t, 30*time.Second)

	// Connect client to reflector
	err := client.Connect(getReflectorAddr())
	require.NoError(t, err)

	// Test that we can't connect again
	err = client.Connect(getReflectorAddr())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection already established")

	// Close connection
	err = client.Close()
	require.NoError(t, err)

	// Test operations after close
	err = client.SendBlob("somehash", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// TestReflectorClientTimeoutHandling tests timeout scenarios
func TestReflectorClientTimeoutHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create client with short timeout
	client := createTestReflectorClient(t, 1*time.Millisecond)

	// This should timeout quickly
	err := client.Connect(getReflectorAddr())
	if err != nil {
		// Expected to timeout
		assertNetworkError(t, err, "Expected network timeout error")
	} else {
		// If it connected, this is a test failure - we expected a timeout
		_ = client.Close()
		t.Fatalf("expected timeout but connected")
	}
}

// TestReflectorClientErrorHandling tests various error scenarios for upload operations
func TestReflectorClientErrorHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Test upload without connection
	t.Run("SendWithoutConnection", func(t *testing.T) {
		client := protocol.NewReflectorClient()

		err := client.SendBlob("hash", []byte("data"))
		if err == nil || !strings.Contains(err.Error(), "not connected") {
			t.Errorf("expected 'not connected' error, got %v", err)
		}

		err = client.SendSDBlob("hash", []byte("data"))
		if err == nil || !strings.Contains(err.Error(), "not connected") {
			t.Errorf("expected 'not connected' error, got %v", err)
		}
	})

	// Test blob too large
	t.Run("BlobTooLarge", func(t *testing.T) {
		client := createConnectedReflectorClient(t, 30*time.Second)

		largeBlob := make([]byte, blob.MaxBlobSize+1)
		err := client.SendBlob("hash", largeBlob)
		if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
			t.Errorf("expected size error, got %v", err)
		}
	})

	// Test invalid hash handling
	t.Run("InvalidHashHandling", func(t *testing.T) {
		client := createConnectedReflectorClient(t, 30*time.Second)

		// Test with empty hash
		err := client.SendBlob(emptyUploadHash, []byte("data"))
		assertValidationError(t, err, "Should return validation error for empty hash")

		// Test with malformed hex hash
		err = client.SendBlob(malformedUploadHash, []byte("data"))
		assertValidationError(t, err, "Should return validation error for malformed hex hash")
	})
}

// TestReflectorClientConcurrentUploads tests multiple concurrent uploads
func TestReflectorClientConcurrentUploads(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create multiple clients for concurrent uploads
	clients := make([]protocol.ReflectorClient, 5)
	for i := range clients {
		clients[i] = createConnectedReflectorClient(t, 30*time.Second)
	}

	// Create test blobs
	testBlobs := make([]blob.Blob, 5)
	blobHashes := make([]string, 5)
	for i := range testBlobs {
		testData := []byte("concurrent test blob data " + string(rune(i+'0')))
		testBlob, blobHash, _, _ := createTestBlob(t, testData)
		testBlobs[i] = testBlob
		blobHashes[i] = blobHash
	}

	// Upload blobs concurrently
	var wg sync.WaitGroup
	wg.Add(len(clients))
	for i, client := range clients {
		i := i // Capture loop variable
		client := client
		go func() {
			defer wg.Done()
			err := client.SendBlob(blobHashes[i], testBlobs[i].ToBytes())
			if err != nil {
				// Expected to get blob exists errors for some
				assertBlobExistsError(t, err, "Concurrent upload should return blob exists error or succeed")
			} else {
				t.Logf("Successfully uploaded blob %s concurrently", blobHashes[i])
			}
		}()
	}

	wg.Wait()
}

// TestReflectorClientLargeBlobHandling tests handling of large blobs
func TestReflectorClientLargeBlobHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	client := createConnectedReflectorClient(t, 30*time.Second)

	// Create large test data (close to 2MB limit)
	largeData := make([]byte, blob.MaxBlobSize-1000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	largeBlob, blobHash, _, _ := createTestBlob(t, largeData)

	// Test SendBlob with large blob
	err := client.SendBlob(blobHash, largeBlob.ToBytes())
	if err != nil {
		// If we get an error, it should be a blob exists error
		assertBlobExistsError(t, err, "Large blob upload should return blob exists error or succeed")
	} else {
		// Upload succeeded
		t.Logf("Successfully uploaded large blob %s", blobHash)
	}
}

// getReflectorAddr returns the reflector address from environment variable or default
func getReflectorAddr() string {
	addr := os.Getenv("LIBLBRY_E2E_REFLECTOR_ADDR")
	if addr == "" {
		return defaultReflectorAddr
	}
	return addr
}
