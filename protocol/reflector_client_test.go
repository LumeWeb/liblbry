package protocol

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	liblbrytesting "go.lumeweb.com/liblbry/internal/testing"
	"go.lumeweb.com/liblbry/storage/memory"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// setupReflectorIntegrationTestServer starts a reflector server on a random port and returns the address
func setupReflectorIntegrationTestServer(t *testing.T, server ReflectorServer) string {
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

// testReflectorSetup creates common test components
func testReflectorSetup(t *testing.T) (*memory.MemoryStore, *zap.Logger, ReflectorServer) {
	store := memory.NewMemoryStore()
	logger := zaptest.NewLogger(t)
	server := NewReflectorServer(store, WithReflectorLogger(logger))
	return store, logger, server
}

// testReflectorClientSetup creates a test client and connects it to the server
func testReflectorClientSetup(t *testing.T, addr string, logger *zap.Logger, timeout ...time.Duration) (ReflectorClient, context.Context) {
	var client ReflectorClient
	if len(timeout) > 0 {
		client = NewReflectorClient(WithReflectorClientLogger(logger), WithReflectorClientTimeout(timeout[0]))
	} else {
		client = NewReflectorClient(WithReflectorClientLogger(logger))
	}

	ctx := context.Background()
	err := client.Connect(addr)
	require.NoError(t, err)

	return client, ctx
}

// setupReflectorIntegrationTest creates a complete integration test setup
func setupReflectorIntegrationTest(t *testing.T, opts ...ReflectorServerOption) (*memory.MemoryStore, *zap.Logger, ReflectorServer, string) {
	store, logger, server := testReflectorSetup(t)

	// Apply additional server options
	serverOpts := []ReflectorServerOption{WithReflectorLogger(logger)}
	serverOpts = append(serverOpts, opts...)
	server = NewReflectorServer(store, serverOpts...)

	addr := setupReflectorIntegrationTestServer(t, server)
	return store, logger, server, addr
}

// setupReflectorTestClient creates a test client with automatic cleanup
func setupReflectorTestClient(t *testing.T, addr string, logger *zap.Logger, timeout ...time.Duration) ReflectorClient {
	client, _ := testReflectorClientSetup(t, addr, logger, timeout...)
	return client
}

// createReflectorTestBlob creates a blob with test data
func createReflectorTestBlob(t *testing.T, testData []byte) (blob.Blob, string, []byte, []byte) {
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

// TestReflectorClientNew tests reflector client creation with different options
func TestReflectorClientNew(t *testing.T) {
	tests := []struct {
		name    string
		options []ReflectorClientOption
		verify  func(t *testing.T, c *DefaultReflectorClient)
	}{
		{
			name:    "default options",
			options: nil,
			verify: func(t *testing.T, c *DefaultReflectorClient) {
				if c.logger == nil {
					t.Error("expected default logger")
				}
				if c.timeout != 30*time.Second {
					t.Errorf("expected default timeout, got %v", c.timeout)
				}
				if c.version != 1 {
					t.Errorf("expected default version 1, got %d", c.version)
				}
			},
		},
		{
			name: "with options",
			options: []ReflectorClientOption{
				WithReflectorClientLogger(zaptest.NewLogger(t)),
				WithReflectorClientTimeout(10 * time.Second),
				WithReflectorClientVersion(2),
			},
			verify: func(t *testing.T, c *DefaultReflectorClient) {
				if c.logger == nil {
					t.Error("expected logger")
				}
				if c.timeout != 10*time.Second {
					t.Errorf("expected 10s timeout, got %v", c.timeout)
				}
				if c.version != 2 {
					t.Errorf("expected version 2, got %d", c.version)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewReflectorClient(tt.options...)
			if dc, ok := client.(*DefaultReflectorClient); ok {
				tt.verify(t, dc)
			} else {
				t.Error("expected *DefaultReflectorClient")
			}
		})
	}
}

// TestReflectorClientConnectAndClose tests connection establishment and closing
func TestReflectorClientConnectAndClose(t *testing.T) {
	store, logger, _, addr := setupReflectorIntegrationTest(t)

	client := setupReflectorTestClient(t, addr, logger)
	if dc, ok := client.(*DefaultReflectorClient); ok {
		if dc.conn == nil {
			t.Error("expected connection to be set")
		}

		err := client.Close()
		if err != nil {
			t.Errorf("Close() error = %v", err)
		}
	} else {
		t.Error("expected *DefaultReflectorClient")
	}

	// Add blob to store to test upload
	testData := []byte("test blob data")
	testBlob, blobHash, _, _ := createReflectorTestBlob(t, testData)
	err := store.Put(blobHash, testBlob)
	require.NoError(t, err)
}

// TestReflectorClientSendBlob tests blob sending functionality
func TestReflectorClientSendBlob(t *testing.T) {
	store, logger, _, addr := setupReflectorIntegrationTest(t)

	testData := []byte("test blob data")
	testBlob, blobHash, _, _ := createReflectorTestBlob(t, testData)
	
	client := setupReflectorTestClient(t, addr, logger)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client)

	// First upload should succeed
	err := client.SendBlob(blobHash, testBlob.ToBytes())
	require.NoError(t, err)

	// Store blob to test duplicate handling
	err = store.Put(blobHash, testBlob)
	require.NoError(t, err)

	// Second upload should return ErrBlobExists
	err = client.SendBlob(blobHash, testBlob.ToBytes())
	if err == nil {
		t.Error("expected error but got nil")
	} else if err.Error() != liblbryerrors.ErrBlobExists.Error() {
		t.Errorf("expected ErrBlobExists message, got %v", err.Error())
	}

	// Create a fresh server for SD blob test to avoid conflicts
	sdStore, logger2, _, sdAddr := setupReflectorIntegrationTest(t)
	
	// Test SendSDBlob with different data
	sdBlobData := []byte("test sd blob data")
	sdTestBlob, sdBlobHash, _, _ := createReflectorTestBlob(t, sdBlobData)
	
	sdClient := setupReflectorTestClient(t, sdAddr, logger2)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(sdClient)

	err = sdClient.SendSDBlob(sdBlobHash, sdTestBlob.ToBytes())
	require.NoError(t, err)

	// Store SD blob to test duplicate handling
	err = sdStore.Put(sdBlobHash, sdTestBlob)
	require.NoError(t, err)

	// Second SD blob upload should return ErrBlobExists
	err = sdClient.SendSDBlob(sdBlobHash, sdTestBlob.ToBytes())
	if err == nil {
		t.Error("expected error but got nil")
	} else if err.Error() != liblbryerrors.ErrBlobExists.Error() {
		t.Errorf("expected ErrBlobExists message, got %v", err.Error())
	}
}

// TestReflectorClientErrorHandling tests various error scenarios
func TestReflectorClientErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() ReflectorClient
		operation   func(c ReflectorClient) error
		expectError string
	}{
		{
			name: "send without connection",
			setup: func() ReflectorClient {
				return NewReflectorClient()
			},
			operation: func(c ReflectorClient) error {
				return c.SendBlob("hash", []byte("data"))
			},
			expectError: "not connected",
		},
		{
			name: "blob too large",
			setup: func() ReflectorClient {
				client := NewReflectorClient()
				// Create a fake connection to bypass the "not connected" check
				client.(*DefaultReflectorClient).conn = &fakeConn{}
				return client
			},
			operation: func(c ReflectorClient) error {
				largeBlob := make([]byte, blob.MaxBlobSize+1)
				return c.SendBlob("hash", largeBlob)
			},
			expectError: "exceeds maximum size",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := tt.setup()
			err := tt.operation(client)
			if err == nil || !strings.Contains(err.Error(), tt.expectError) {
				t.Errorf("expected error containing %q, got %v", tt.expectError, err)
			}
		})
	}
}

// TestReflectorClientTimeoutHandling tests timeout scenarios
func TestReflectorClientTimeoutHandling(t *testing.T) {
	_, logger, _, addr := setupReflectorIntegrationTest(t, WithReflectorTimeout(100*time.Millisecond))

	client := setupReflectorTestClient(t, addr, logger, 100*time.Millisecond)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client)

	// Test send with timeout
	testData := []byte("test data")
	err := client.SendBlob("hash", testData)
	if err == nil {
		t.Error("expected timeout error")
	}
}

// TestReflectorClientConcurrentClients tests multiple concurrent clients accessing the server
func TestReflectorClientConcurrentClients(t *testing.T) {
	// Create one server for all clients to connect to
	store, logger, _, addr := setupReflectorIntegrationTest(t)
	
	// Create ONE blob that all clients will try to upload
	testData := []byte("This is test blob data for concurrent access")
	testBlob, blobHash, _, _ := createReflectorTestBlob(t, testData)
	err := store.Put(blobHash, testBlob)
	require.NoError(t, err)

	// Create tasks for concurrent execution
	tasks := make([]func() error, 10)

	for i := 0; i < 10; i++ {
		tasks[i] = func() error {
			// Create client
			client := NewReflectorClient(WithReflectorClientLogger(logger))
			defer func(client ReflectorClient) {
				_ = client.Close()
			}(client)

			// Connect client to the SAME server
			err := client.Connect(addr)
			if err != nil {
				return err
			}

			// Test SendBlob - should return ErrBlobExists for all clients 
			// since they're all trying to upload the same blob
			err = client.SendBlob(blobHash, testBlob.ToBytes())
			if err != nil {
				// Check if it's the expected "blob already exists" error
				if err.Error() != liblbryerrors.ErrBlobExists.Error() {
					return err // Return unexpected errors
				}
				// ErrBlobExists is expected, so this is success
				return nil
			}
			
			// If no error, this was the first client to upload successfully
			return nil
		}
	}

	// Execute tasks concurrently using the testing utility
	liblbrytesting.RunConcurrentTasks(t, 5, tasks)
}

// TestReflectorClientLargeBlobHandling tests handling of large blobs
func TestReflectorClientLargeBlobHandling(t *testing.T) {
	store, logger, _, addr := setupReflectorIntegrationTest(t)

	// Create large test data (接近2MB限制)
	largeData := make([]byte, blob.MaxBlobSize-1000)
	for i := range largeData {
		largeData[i] = byte(i % 256)
	}

	largeBlob, blobHash, _, _ := createReflectorTestBlob(t, largeData)

	client := setupReflectorTestClient(t, addr, logger)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client)

	// Test SendBlob with large blob - first upload should succeed
	err := client.SendBlob(blobHash, largeBlob.ToBytes())
	require.NoError(t, err)

	// NOW put the blob in the store to test duplicate handling
	err = store.Put(blobHash, largeBlob)
	require.NoError(t, err)

	// Create a fresh client and server for testing duplicate upload
	store2, logger2, _, addr2 := setupReflectorIntegrationTest(t)
	
	// Put the same blob in the new store
	err = store2.Put(blobHash, largeBlob)
	require.NoError(t, err)
	
	client2 := setupReflectorTestClient(t, addr2, logger2)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client2)

	// Test SendBlob with large blob - second upload should return ErrBlobExists
	err = client2.SendBlob(blobHash, largeBlob.ToBytes())
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrBlobExists))
}

// TestReflectorClientConnectionManagement tests connection management features
func TestReflectorClientConnectionManagement(t *testing.T) {
	_, logger, _, addr := setupReflectorIntegrationTest(t)

	client := NewReflectorClient(WithReflectorClientLogger(logger))
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client)

	// Connect client to server
	err := client.Connect(addr)
	require.NoError(t, err)

	// Test that we can't connect again
	err = client.Connect(addr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection already established")

	// Close connection
	err = client.Close()
	require.NoError(t, err)

	// Verify we're no longer connected
	assert.Nil(t, client.(*DefaultReflectorClient).conn)

	// Test operations after close
	err = client.SendBlob("somehash", []byte("data"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

// TestReflectorClientNetworkFailureRecovery tests client behavior when connection drops mid-operation
func TestReflectorClientNetworkFailureRecovery(t *testing.T) {
	store, logger, _, addr := setupReflectorIntegrationTest(t)

	testData := []byte("This is a test blob for network failure recovery")
	testBlob, blobHash, _, _ := createReflectorTestBlob(t, testData)

	client := setupReflectorTestClient(t, addr, logger)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client)

	// Test normal operation first
	err := client.SendBlob(blobHash, testBlob.ToBytes())
	require.NoError(t, err)

	// Now put the blob in the store to test duplicate handling after recovery
	err = store.Put(blobHash, testBlob)
	require.NoError(t, err)

	// Simulate network failure by closing connection
	err = client.Close()
	require.NoError(t, err)

	// Operations should now fail
	err = client.SendBlob(blobHash, testBlob.ToBytes())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")

	// Reconnect to the same server (should still work)
	err = client.Connect(addr)
	require.NoError(t, err)

	// Operations should work again, but blob already exists so expect ErrBlobExists
	err = client.SendBlob(blobHash, testBlob.ToBytes())
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrBlobExists))
}

// fakeConn is a fake connection for testing error cases
type fakeConn struct{}

func (f *fakeConn) Read(b []byte) (n int, err error)  { return 0, errors.New("read error") }
func (f *fakeConn) Write(b []byte) (n int, err error) { return 0, errors.New("write error") }
func (f *fakeConn) Close() error                      { return nil }
func (f *fakeConn) LocalAddr() net.Addr               { return nil }
func (f *fakeConn) RemoteAddr() net.Addr              { return nil }
func (f *fakeConn) SetDeadline(t time.Time) error     { return nil }
func (f *fakeConn) SetReadDeadline(t time.Time) error { return nil }
func (f *fakeConn) SetWriteDeadline(t time.Time) error {
	return nil
}

// reflectorStallConn is a connection wrapper that simulates network interruption
// by failing writes immediately
type reflectorStallConn struct {
	net.Conn
}

func (s *reflectorStallConn) Read(p []byte) (int, error) {
	// For this test, reads should work normally
	return s.Conn.Read(p)
}

func (s *reflectorStallConn) Write(p []byte) (int, error) {
	// Fail immediately with a network error
	return 0, errors.New("network interruption")
}

// TestReflectorClientPartialBlobTransfer tests interrupted blob transfers
func TestReflectorClientPartialBlobTransfer(t *testing.T) {
	store, logger, _, addr := setupReflectorIntegrationTest(t)

	// Create test data
	testData := []byte("This is a test blob for partial transfer")
	testBlob, blobHash, _, _ := createReflectorTestBlob(t, testData)
	err := store.Put(blobHash, testBlob)
	require.NoError(t, err)

	// Set up a custom dial function that returns our stall connection
	stallDialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		// First establish a real connection
		realConn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		// Wrap it in our stall connection that fails writes
		return &reflectorStallConn{
			Conn: realConn,
		}, nil
	}

	// Create a client with a stall connection using the custom dialer
	client := NewReflectorClient(WithReflectorClientLogger(logger), WithReflectorClientDialContext(stallDialer))
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(client)

	// This should fail during handshake due to the stall connection
	err = client.Connect(addr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network interruption")

	// Close connection properly
	err = client.Close()
	require.NoError(t, err)

	// Create a new client with normal connection for reconnection test
	clientWithNormalConn := setupReflectorTestClient(t, addr, logger)
	defer func(client ReflectorClient) {
		_ = client.Close()
	}(clientWithNormalConn)

	// Should work now with new client, but blob already exists so expect ErrBlobExists
	err = clientWithNormalConn.SendBlob(blobHash, testBlob.ToBytes())
	require.Error(t, err)
	assert.True(t, liblbryerrors.Is(err, liblbryerrors.ErrBlobExists))
}
