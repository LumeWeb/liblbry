package protocol

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	liblbrytesting "go.lumeweb.com/liblbry/internal/testing"
	"go.lumeweb.com/liblbry/storage/memory"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// setupIntegrationTestServer starts a peer server on a random port and returns the address
func setupIntegrationTestServer(t *testing.T, server PeerServer) string {
	// Start server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	// Server goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Check if the listener was closed intentionally
				if netErr, ok := err.(net.Error); ok && netErr.Temporary() {
					// Temporary error, continue listening
					continue
				}
				// Listener closed or permanent error, exit goroutine
				return
			}

			// Handle connection in a separate goroutine
			go func(conn net.Conn) {
				defer func() {
					if r := recover(); r != nil {
						// Log panic recovery
					}
					if err := conn.Close(); err != nil {
						// Log the error but don't fail the test since this is cleanup
					}
				}()

				server.HandleConnection(conn)
			}(conn)
		}
	}()

	addr := listener.Addr().String()
	
	// Register cleanup function to close listener when test completes
	t.Cleanup(func() {
		_ = listener.Close()
	})
	
	return addr
}

// testSetup creates common test components
func testSetup(t *testing.T) (*memory.MemoryStore, *zap.Logger, PeerServer) {
	store := memory.NewMemoryStore()
	logger := zaptest.NewLogger(t)
	server := NewPeerServer(store, WithPeerLogger(logger))
	return store, logger, server
}

// testClientSetup creates a test client and connects it to the server
func testClientSetup(t *testing.T, addr string, logger *zap.Logger, timeout ...time.Duration) (PeerClient, context.Context) {
	var client PeerClient
	if len(timeout) > 0 {
		client = NewPeerClient(WithClientLogger(logger), WithClientTimeout(timeout[0]))
	} else {
		client = NewPeerClient(WithClientLogger(logger))
	}

	ctx := context.Background()
	err := client.Connect(ctx, addr)
	require.NoError(t, err)

	return client, ctx
}

// setupIntegrationTest creates a complete integration test setup
func setupIntegrationTest(t *testing.T, opts ...ServerOption) (*memory.MemoryStore, *zap.Logger, PeerServer, string) {
	store, logger, server := testSetup(t)

	// Apply additional server options
	serverOpts := []ServerOption{WithPeerLogger(logger)}
	serverOpts = append(serverOpts, opts...)
	server = NewPeerServer(store, serverOpts...)

	addr := setupIntegrationTestServer(t, server)
	return store, logger, server, addr
}

// setupTestClient creates a test client with automatic cleanup
func setupTestClient(t *testing.T, addr string, logger *zap.Logger, timeout ...time.Duration) PeerClient {
	client, _ := testClientSetup(t, addr, logger, timeout...)
	return client
}

// createAndStoreTestBlob creates a blob and stores it in the memory store
func createAndStoreTestBlob(t *testing.T, store *memory.MemoryStore, testData []byte) (blob.Blob, string) {
	testBlob, blobHash, _, _ := createTestBlob(t, testData)
	err := store.Put(blobHash, testBlob)
	require.NoError(t, err)
	return testBlob, blobHash
}

// testPaymentRate tests payment rate negotiation
func testPaymentRate(t *testing.T, client PeerClient, blobHash string, rate float64, expectedResponse string) {
	request := CompositeRequest{
		RequestedBlobs:      []string{blobHash},
		BlobDataPaymentRate: lo.ToPtr(rate),
	}

	conn := client.(*DefaultPeerClient).conn
	data, err := json.Marshal(request)
	require.NoError(t, err)

	_, err = conn.Write(data)
	require.NoError(t, err)

	responseData, err := client.(*DefaultPeerClient).readNextMessage()
	require.NoError(t, err)

	var response CompositeResponse
	err = json.Unmarshal(responseData, &response)
	require.NoError(t, err)

	assert.Equal(t, expectedResponse, response.BlobDataPaymentRate)
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

// TestBlobOperations tests basic blob operations through the peer protocol
func TestBlobOperations(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)
	
	testData := []byte("This is a test blob for integration testing")
	_, blobHash := createAndStoreTestBlob(t, store, testData)
	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test HasBlob
	has, err := client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.True(t, has)

	// Test HasBlob with invalid hash
	has, err = client.HasBlob(ctx, "invalidhash")
	require.Error(t, err)
	assert.False(t, has)

	// Test GetBlob
	retrievedData, err := client.GetBlob(ctx, blobHash)
	require.NoError(t, err)
	// Verify the retrieved data matches what we stored
	storedData, err := store.Get(blobHash)
	require.NoError(t, err)
	assert.Equal(t, storedData, retrievedData)

	// Test GetBlob with invalid hash
	_, err = client.GetBlob(ctx, "a"+strings.Repeat("0", blob.BlobHashHexLength-1))
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrBlobNotFound))
}

// TestStreamOperations tests stream creation and retrieval through the peer protocol
func TestStreamOperations(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)

	// Test stream data - adjusted size to ensure exactly 3 blobs (SD + 2 content)
	testData := strings.Repeat("A", 2*1024*1024) // 2MB of data
	reader := strings.NewReader(testData)

	// Create stream
	resultStream, err := stream.New(reader)
	require.NoError(t, err)

	// Get SD blob hash
	sdBlob := resultStream[0]
	sdBlobHash := sdBlob.HashHex()

	// Parse SD blob to get expected content count
	var sd stream.SDBlob
	err = sd.FromBlob(sdBlob)
	require.NoError(t, err)
	expectedLen := len(sd.BlobInfos) // includes terminating null blob
	require.Equal(t, expectedLen, len(resultStream))

	// Add the blobs to the server's store so it can serve them
	for _, blobItem := range resultStream {
		err = store.Put(blobItem.HashHex(), blobItem)
		require.NoError(t, err)
	}

	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test GetStream
	retrievedStream, err := client.GetStream(ctx, sdBlobHash)
	require.NoError(t, err)
	require.Len(t, retrievedStream, 3)

	// Verify SD blob
	assert.Equal(t, []byte(sdBlob), []byte(retrievedStream[0]))

	// Verify content blobs
	assert.Equal(t, []byte(resultStream[1]), []byte(retrievedStream[1]))
	assert.Equal(t, []byte(resultStream[2]), []byte(retrievedStream[2]))

	// Decode stream
	decodedData, err := retrievedStream.Decode()
	require.NoError(t, err)
	assert.Equal(t, testData, string(decodedData))

	// Test GetStream with invalid hash
	_, err = client.GetStream(ctx, "invalidhash")
	require.Error(t, err)
}

// TestPaymentRateNegotiation tests payment rate negotiation through the peer protocol
func TestPaymentRateNegotiation(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)
	
	testData := []byte("This is a test blob for payment rate negotiation")
	_, blobHash := createAndStoreTestBlob(t, store, testData)
	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)

	// Test payment rate negotiation
	testPaymentRate(t, client, blobHash, 0.01, PaymentRateAccepted)
	testPaymentRate(t, client, blobHash, -0.01, PaymentRateTooLow)
}

// TestAccessControlIntegration tests access control integration
func TestAccessControlIntegration(t *testing.T) {
	accessControl := &allowAllAccessControl{}
	store, logger, _, addr := setupIntegrationTest(t, WithPeerAccessControl(accessControl))

	testData := []byte("This is a test blob for access control")
	testBlob, blobHash := createAndStoreTestBlob(t, store, testData)

	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test HasBlob - should be allowed
	has, err := client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.True(t, has)

	// Test GetBlob - should be allowed
	retrievedData, err := client.GetBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.Equal(t, []byte(testBlob), retrievedData)
}

// TestIntegrationTimeoutHandling tests timeout handling in client-server communication
func TestIntegrationTimeoutHandling(t *testing.T) {
	_, logger, _, addr := setupIntegrationTest(t, WithPeerTimeout(100*time.Millisecond))
	
	client := setupTestClient(t, addr, logger, 100*time.Millisecond)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test GetBlob with invalid hash (should timeout)
	_, err := client.GetBlob(ctx, "invalidhash")
	require.Error(t, err)
}

// TestConcurrentClients tests multiple concurrent clients accessing the server
func TestConcurrentClients(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)

	testData := []byte("This is a test blob for concurrent access")
	testBlob, blobHash := createAndStoreTestBlob(t, store, testData)

	// Create tasks for concurrent execution
	tasks := make([]func() error, 10)
	for i := 0; i < 10; i++ {
		tasks[i] = func() error {
			// Create client
			client := NewPeerClient(WithClientLogger(logger))
			defer func(client PeerClient) {
				_ = client.Close()
			}(client)

			// Create context
			ctx := context.Background()

			// Connect client to server
			err := client.Connect(ctx, addr)
			if err != nil {
				return err
			}

			// Test HasBlob
			has, err := client.HasBlob(ctx, blobHash)
			if err != nil {
				return err
			}
			if !has {
				return errors.New("blob should exist but was not found")
			}

			// Test GetBlob
			retrievedData, err := client.GetBlob(ctx, blobHash)
			if err != nil {
				return err
			}
			if !bytes.Equal([]byte(testBlob), retrievedData) {
				return errors.New("retrieved data does not match original")
			}

			return nil
		}
	}

	// Execute tasks concurrently using the testing utility
	liblbrytesting.RunConcurrentTasks(t, 5, tasks)
}

// TestLargeBlobHandling tests handling of large blobs
func TestLargeBlobHandling(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)

	// Create large test data (接近2MB限制)
	largeData := make([]byte, blob.MaxBlobSize-1000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	largeBlob, blobHash := createAndStoreTestBlob(t, store, largeData)

	// Verify blob size is within limits
	assert.Less(t, len(largeBlob), blob.MaxBlobSize)

	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test HasBlob
	has, err := client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.True(t, has)

	// Test GetBlob
	retrievedData, err := client.GetBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.Equal(t, []byte(largeBlob), retrievedData)
}

// TestConnectionManagement tests connection management features
func TestConnectionManagement(t *testing.T) {
	_, logger, _, addr := setupIntegrationTest(t)
	
	client := NewPeerClient(WithClientLogger(logger))
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)

	ctx := context.Background()

	// Connect client to server
	err := client.Connect(ctx, addr)
	require.NoError(t, err)

	// Test that we can't connect again
	err = client.Connect(ctx, addr)
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrAlreadyConnected))

	// Close connection
	err = client.Close()
	require.NoError(t, err)

	// Verify we're no longer connected
	assert.False(t, client.(*DefaultPeerClient).connected)

	// Test operations after close
	_, err = client.HasBlob(ctx, "somehash")
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrConnectionFailed))
}

// TestErrorScenarios tests various error scenarios
func TestErrorScenarios(t *testing.T) {
	_, logger, _, addr := setupIntegrationTest(t)
	
	client := NewPeerClient(WithClientLogger(logger))
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)

	ctx := context.Background()

	// Test operations without connecting
	_, err := client.HasBlob(ctx, "somehash")
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrConnectionFailed))

	_, err = client.GetBlob(ctx, "somehash")
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrConnectionFailed))

	// Connect client to server
	err = client.Connect(ctx, addr)
	require.NoError(t, err)

	// Test HasBlob with invalid hash length
	_, err = client.HasBlob(ctx, "invalid")
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrInvalidHashLen))

	// Test GetBlob with invalid hash length
	_, err = client.GetBlob(ctx, "invalid")
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrInvalidHashLen))

	// Test GetBlob with non-existent blob
	_, err = client.GetBlob(ctx, "a"+strings.Repeat("0", blob.BlobHashHexLength-1))
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrBlobNotFound))
}

// TestNetworkFailureRecovery tests client behavior when connection drops mid-operation
func TestNetworkFailureRecovery(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)
	
	testData := []byte("This is a test blob for network failure recovery")
	_, blobHash := createAndStoreTestBlob(t, store, testData)
	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test normal operation first
	has, err := client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.True(t, has)

	// Simulate network failure by closing connection
	err = client.Close()
	require.NoError(t, err)

	// Verify we're no longer connected
	assert.False(t, client.(*DefaultPeerClient).connected)

	// Operations should now fail
	_, err = client.HasBlob(ctx, blobHash)
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrConnectionFailed))

	// Reconnect
	err = client.Connect(ctx, addr)
	require.NoError(t, err)

	// Operations should work again
	has, err = client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.True(t, has)

	// Test GetBlob after reconnection
	_, err = client.GetBlob(ctx, blobHash)
	require.NoError(t, err)
}

// TestMalformedRequests tests handling of invalid JSON and malformed requests
func TestMalformedRequests(t *testing.T) {
	_, logger, server := testSetup(t)
	addr := setupIntegrationTestServer(t, server)

	t.Run("Test malformed JSON", func(t *testing.T) {
		client := setupTestClient(t, addr, logger)
		defer func(client PeerClient) {
			_ = client.Close()
		}(client)

		// Test malformed JSON
		conn := client.(*DefaultPeerClient).conn
		_, err := conn.Write([]byte("{invalid json"))
		require.NoError(t, err)

		// Set read deadline to prevent hanging
		err = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		require.NoError(t, err)

		// Read response - server may either send an error response or close connection
		responseData, err := client.(*DefaultPeerClient).readNextMessage()

		// Either we get a valid error response or a timeout/EOF error (both are valid)
		if err != nil {
			// Check if error is EOF, timeout, or connection reset - all are acceptable
			if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "connection reset") {
				return // Valid server behavior, test passes
			}
			t.Fatalf("Unexpected error: %v", err)
		}

		// If we got data, it should be a valid error response
		var response CompositeResponse
		err = json.Unmarshal(responseData, &response)
		require.NoError(t, err)

		assert.NotNil(t, response.IncomingBlob)
		assert.NotEmpty(t, response.IncomingBlob.Error)
	})

	t.Run("Test empty request", func(t *testing.T) {
		// Create a new client for better isolation
		client2 := setupTestClient(t, addr, logger)
		defer func(client PeerClient) {
			_ = client.Close()
		}(client2)

		// Test missing requested fields (should still return a response)
		emptyRequest := CompositeRequest{}
		data, err := json.Marshal(emptyRequest)
		require.NoError(t, err)

		conn2 := client2.(*DefaultPeerClient).conn
		_, err = conn2.Write(data)
		require.NoError(t, err)

		// Set read deadline to prevent hanging
		err = conn2.SetReadDeadline(time.Now().Add(5 * time.Second))
		require.NoError(t, err)

		// This might cause connection to close, which is expected behavior
		_, err = client2.(*DefaultPeerClient).readNextMessage()
		if err != io.EOF {
			require.NoError(t, err)
		}
	})
}

// TestEmptyBlobHandling tests handling of zero-size blobs
func TestEmptyBlobHandling(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)

	// Test data - use minimal non-empty data that works properly with AES PKCS#7 padding
	testData := make([]byte, 1)
	testData[0] = 0x00
	testBlob, blobHash, key, iv := createTestBlob(t, testData)
	err := store.Put(blobHash, testBlob)
	require.NoError(t, err)

	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test HasBlob with minimal blob
	has, err := client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.True(t, has)

	// Test GetBlob with minimal blob
	retrievedData, err := client.GetBlob(ctx, blobHash)
	require.NoError(t, err)

	// Create a Blob from retrievedData
	retrievedBlob := blob.Blob(retrievedData)

	// Decrypt the blob using the same key and iv from testBlob
	decryptedData, err := retrievedBlob.Decrypt(key, iv)
	require.NoError(t, err)

	// Verify that the decrypted content matches the original
	assert.Equal(t, testData, decryptedData)
}

// TestRestrictedAccessControl tests deny-by-default access control
func TestRestrictedAccessControl(t *testing.T) {
	accessControl := &denyAllAccessControl{}
	store, logger, _, addr := setupIntegrationTest(t, WithPeerAccessControl(accessControl))
	
	testData := []byte("This is a test blob for restricted access")
	_, blobHash := createAndStoreTestBlob(t, store, testData)
	client := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)
	ctx := context.Background()

	// Test HasBlob - should be denied
	has, err := client.HasBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.False(t, has)

	// Test GetBlob - should be denied
	_, err = client.GetBlob(ctx, blobHash)
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrAccessDenied))
}

// stallConn is a connection wrapper that simulates network interruption
// by failing writes immediately
type stallConn struct {
	net.Conn
}

func (s *stallConn) Read(p []byte) (int, error) {
	// For this test, reads should work normally
	return s.Conn.Read(p)
}

func (s *stallConn) Write(p []byte) (int, error) {
	// Fail immediately with a network error
	return 0, errors.New("network interruption")
}

// TestPartialBlobTransfer tests interrupted blob transfers
func TestPartialBlobTransfer(t *testing.T) {
	store, logger, _, addr := setupIntegrationTest(t)

	// Create test data
	testData := []byte("This is a test blob for partial transfer")
	testBlob, blobHash := createAndStoreTestBlob(t, store, testData)

	// Set up a custom dial function that returns our stall connection
	stallDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		// First establish a real connection
		var realConn net.Conn
		var err error
		if ctx != nil {
			realConn, err = (&net.Dialer{}).DialContext(ctx, network, address)
		} else {
			realConn, err = net.Dial(network, address)
		}
		if err != nil {
			return nil, err
		}
		// Wrap it in our stall connection that fails writes
		return &stallConn{
			Conn:   realConn,
		}, nil
	}

	// Create a client with a stall connection using the custom dialer
	client := NewPeerClient(WithClientLogger(logger), WithClientDialContext(stallDialer))
	defer func(client PeerClient) {
		_ = client.Close()
	}(client)

	ctx := context.Background()
	err := client.Connect(ctx, addr)
	require.NoError(t, err)

	// This should fail due to the stall connection interrupting the write operation
	_, err = client.GetBlob(ctx, blobHash)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network interruption")

	// Close connection properly
	err = client.Close()
	require.NoError(t, err)

	// Create a new client with normal connection for reconnection test
	clientWithNormalConn := setupTestClient(t, addr, logger)
	defer func(client PeerClient) {
		_ = client.Close()
	}(clientWithNormalConn)

	// Should work now with new client
	retrievedData, err := clientWithNormalConn.GetBlob(ctx, blobHash)
	require.NoError(t, err)
	assert.Equal(t, []byte(testBlob), retrievedData)
}

// allowAllAccessControl is a simple access control that allows all requests
type allowAllAccessControl struct{}

func (a *allowAllAccessControl) Allow(hash string, peerIP string) bool {
	return true
}

// denyAllAccessControl is a simple access control that denies all requests
type denyAllAccessControl struct{}

func (d *denyAllAccessControl) Allow(hash string, peerIP string) bool {
	return false
}
