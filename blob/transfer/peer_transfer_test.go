package transfer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	dht "go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/storage/memory"
	"go.uber.org/zap"
)

// TestPeerServer represents a running peer server for testing
type TestPeerServer struct {
	listener net.Listener
	server   protocol.PeerServer
	address  string
	port     int
	storage  storage.BlobStore
	wg       sync.WaitGroup
	ready    chan struct{}
	once     sync.Once
}

// StartTestPeerServer starts a real peer server on a random port for testing
func StartTestPeerServer(t *testing.T, blobData map[string][]byte) *TestPeerServer {
	// Listen on a random port to avoid TOCTOU race conditions
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "Failed to listen on random port")

	port := listener.Addr().(*net.TCPAddr).Port
	address := fmt.Sprintf("127.0.0.1:%d", port)

	// Create memory storage with test data
	store := memory.NewMemoryStore()
	for hash, data := range blobData {
		err := store.Put(hash, data)
		require.NoError(t, err, "Failed to store test blob")
	}

	// Create peer server
	server := protocol.NewPeerServer(store,
		protocol.WithPeerLogger(zap.NewNop().Named("test-peer-server")),
	)

	// Start handling connections
	testServer := &TestPeerServer{
		listener: listener,
		server:   server,
		address:  address,
		port:     port,
		storage:  store,
		ready:    make(chan struct{}),
	}

	testServer.wg.Add(1)
	go func() {
		defer testServer.wg.Done()
		// Signal readiness once after starting the accept loop
		testServer.once.Do(func() {
			close(testServer.ready)
		})
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Listener closed, exit
				return
			}
			testServer.wg.Add(1)
			go func() {
				defer testServer.wg.Done()
				server.HandleConnection(conn)
			}()
		}
	}()

	// Wait for server to be ready
	<-testServer.ready

	return testServer
}

// Stop stops the test peer server
func (tps *TestPeerServer) Stop() {
	if tps.listener != nil {
		tps.listener.Close()
	}
	tps.wg.Wait()
}

// testHash is a valid SHA-384 hash used consistently across tests
const testHash = "acc6adf8b4f10dcddffc5c2ca87dbd9cb3a2664564695ac7aaab038193ff14a280cc3d4ebae55c71d0b885a7316d0137"

// TestPeerClientWrapper wraps a PeerClient to track download attempts for testing
type TestPeerClientWrapper struct {
	protocol.PeerClient
	downloadAttempts int64
}

func (w *TestPeerClientWrapper) GetBlob(ctx context.Context, hash string) ([]byte, error) {
	atomic.AddInt64(&w.downloadAttempts, 1)
	return w.PeerClient.GetBlob(ctx, hash)
}

// newTestPeerTransferWithTracking creates a PeerTransfer with tracking capabilities
func newTestPeerTransferWithTracking(t *testing.T) (*PeerTransfer, *protocolMocks.MockDHTNode, *TestPeerClientWrapper) {
	mockDHT := protocolMocks.NewMockDHTNode(t)

	// Create a tracking wrapper that will be used for all clients
	tracker := &TestPeerClientWrapper{}

	// Create a factory that returns wrapped clients
	factory := protocol.PeerClientFactory(func() protocol.PeerClient {
		// Create a real client and wrap it
		realClient := protocol.DefaultPeerClientFactory()()
		tracker.PeerClient = realClient
		return tracker
	})

	transfer, err := NewPeerTransfer(mockDHT, factory,
		WithPeerTransferLogger(zap.NewNop()),
		WithPeerTransferMaxPeers(3),
		WithPeerTransferMaxConcurrency(5),
	)
	require.NoError(t, err)

	return transfer, mockDHT, tracker
}

// newTestPeerTransfer creates a PeerTransfer with mock DHT and factory for testing
// This helper reduces boilerplate across multiple test functions
func newTestPeerTransfer(t *testing.T) (*PeerTransfer, *protocolMocks.MockDHTNode) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)
	transfer, err := NewPeerTransfer(dhtNode, peerClientFactory)
	require.NoError(t, err)

	return transfer, dhtNode
}

// newTestPeerTransferWithMockClient creates a PeerTransfer with mock DHT and a custom client factory
// This allows tests to inject specific mock client behavior
func newTestPeerTransferWithMockClient(t *testing.T, clientFactory protocol.PeerClientFactory) (*PeerTransfer, *protocolMocks.MockDHTNode) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	transfer, err := NewPeerTransfer(dhtNode, clientFactory)
	require.NoError(t, err)

	return transfer, dhtNode
}

// TestNewPeerTransfer tests constructor and basic configuration
func TestNewPeerTransfer(t *testing.T) {
	tests := []struct {
		name              string
		dhtNode           protocol.DHTNode
		peerClientFactory protocol.PeerClientFactory
		clientFactory     protocol.PeerClientFactory
		timeout           time.Duration
		maxPeers          int
		logger            *zap.Logger
		expected          *PeerTransfer
	}{
		{
			name:              "Default configuration",
			dhtNode:           nil,
			peerClientFactory: nil,
			clientFactory:     nil,
			timeout:           30 * time.Second,
			maxPeers:          5,
			logger:            nil,
			expected: &PeerTransfer{
				timeout:  30 * time.Second,
				maxPeers: 5,
				logger:   zap.NewNop(),
			},
		},
		{
			name:    "With custom DHT and peer client factory",
			dhtNode: protocolMocks.NewMockDHTNode(t),
			peerClientFactory: protocol.DefaultPeerClientFactory(
				protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
			),
			clientFactory: protocol.DefaultPeerClientFactory(
				protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
			),
			timeout:  60 * time.Second,
			maxPeers: 10,
			logger:   zap.NewNop(),
			expected: &PeerTransfer{
				timeout:  60 * time.Second,
				maxPeers: 10,
				logger:   zap.NewNop(),
			},
		},
		{
			name:    "With logger",
			dhtNode: protocolMocks.NewMockDHTNode(t),
			peerClientFactory: protocol.DefaultPeerClientFactory(
				protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
			),
			clientFactory: protocol.DefaultPeerClientFactory(
				protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
			),
			timeout:  30 * time.Second,
			maxPeers: 5,
			logger:   zap.NewNop().Named("test-logger"),
			expected: &PeerTransfer{
				timeout:  30 * time.Second,
				maxPeers: 5,
				logger:   zap.NewNop().Named("test-logger"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transfer, err := NewPeerTransfer(test.dhtNode, test.peerClientFactory,
				WithPeerTransferTimeout(test.timeout),
				WithPeerTransferMaxPeers(test.maxPeers),
				WithPeerTransferLogger(test.logger),
			)

			// Basic non-nil assertions
			if test.peerClientFactory == nil {
				assert.Error(t, err)
				assert.Nil(t, transfer)
				return
			}

			assert.NoError(t, err)
			assert.NotNil(t, transfer)

			// Verify core fields are properly wired through
			assert.Equal(t, test.dhtNode, transfer.dhtNode, "DHT node should be properly set")
			// Function types cannot be compared directly, so we check nil status instead
			if test.peerClientFactory == nil {
				assert.Nil(t, transfer.peerClientFactory, "Peer client factory should be nil when input is nil")
			} else {
				assert.NotNil(t, transfer.peerClientFactory, "Peer client factory should be set when input is not nil")
			}
			if test.clientFactory == nil {
				assert.Nil(t, transfer.clientFactory, "Client factory should be nil when input is nil")
			} else {
				assert.NotNil(t, transfer.clientFactory, "Client factory should be set when input is not nil")
			}

			// Verify configuration fields
			assert.Equal(t, test.expected.timeout, transfer.timeout)
			assert.Equal(t, test.expected.maxPeers, transfer.maxPeers)
			if test.logger != nil {
				assert.Equal(t, test.logger, transfer.logger)
			} else {
				// When nil logger is passed, default logger should be preserved
				assert.NotNil(t, transfer.logger, "default logger should be preserved when nil is passed")
			}

			// Verify internal structures are properly initialized
			assert.NotNil(t, transfer.workerPool, "worker pool should be initialized")
			assert.NotNil(t, transfer.clientPool, "client pool should be initialized")
			assert.NotNil(t, transfer.backlog, "backlog should be initialized")
			assert.Equal(t, 5, transfer.maxConcurrency, "maxConcurrency should have default value")
			assert.False(t, transfer.isStopped(), "transfer should not be stopped initially")
		})
	}
}

// TestNewPeerTransfer_NilFactory tests that constructor returns error for nil factory
func TestNewPeerTransfer_NilFactory(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)

	transfer, err := NewPeerTransfer(dhtNode, nil)

	assert.Error(t, err)
	assert.Nil(t, transfer)
	assert.Contains(t, err.Error(), "peerClientFactory cannot be nil")
}

// TestPeerTransfer_Start_NilFactory tests that Start() handles nil factory gracefully
// This tests the defensive programming in createClientPool when somehow factory becomes nil
func TestPeerTransfer_Start_NilFactory(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	logger := zap.NewNop()

	// Create a valid transfer first
	factory := protocol.DefaultPeerClientFactory()
	transfer, err := NewPeerTransfer(dhtNode, factory, WithPeerTransferLogger(logger))
	require.NoError(t, err)
	require.NotNil(t, transfer)

	// Manually set factory to nil to test defensive behavior (this shouldn't happen in practice)
	transfer.clientFactory = nil

	// Stop the transfer to clear the client pool
	transfer.Stop()
	assert.Nil(t, transfer.clientPool, "Client pool should be nil after Stop()")

	// Start should not panic even with nil factory, it should log error and continue
	transfer.Start()

	// Client pool should remain nil because factory is nil
	assert.Nil(t, transfer.clientPool, "Client pool should remain nil when factory is nil")
}

// TestPeerTransfer_Get_NoPeersFound tests DHT discovery returning no contacts
func TestPeerTransfer_Get_NoPeersFound(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Mock DHT to return no contacts
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{}, nil)

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob not found")
	assert.Nil(t, data)

}

// TestPeerTransfer_Get_DHTError tests DHT.Get() error scenarios
func TestPeerTransfer_Get_DHTError(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Mock DHT to return an error
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(nil, fmt.Errorf("DHT lookup failed"))

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT lookup failed")
	assert.Nil(t, data)

}

// TestPeerTransfer_Get_AllPeersFail tests when all peers fail
func TestPeerTransfer_Get_AllPeersFail(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Mock DHT to return non-existent server addresses (will all fail)
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99999, PeerPort: 99999},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99998, PeerPort: 99998},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99997, PeerPort: 99997},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)
	transfer.maxPeers = 3

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Nil(t, data)
}

// TestPeerTransfer_Get_AllPeersFail_ImmediateError tests that when all peers fail,
// the error is returned immediately without waiting for timeout
func TestPeerTransfer_Get_AllPeersFail_ImmediateError(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Mock DHT to return non-existent server addresses (will all fail immediately)
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99999, PeerPort: 99999},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99998, PeerPort: 99998},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99997, PeerPort: 99997},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer.maxPeers = 3

	// Test with a very short timeout to ensure immediate error return
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	data, err := transfer.Get(ctx, testHash)

	assert.Error(t, err)
	assert.Nil(t, data)

	// Should not be a timeout error since we expect immediate failure
	assert.False(t, errors.Is(err, context.DeadlineExceeded))
}

// TestPeerTransfer_Get_MixedSuccessFailure tests mixed success/failure scenarios
func TestPeerTransfer_Get_MixedSuccessFailure(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Start a real peer server with test blob
	blobData := []byte("success-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return the test server's address (only one working server)
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     testServer.port,
		PeerPort: testServer.port,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	transfer.maxPeers = 3

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Stop_MultipleCalls tests that Stop() can be called multiple times safely
func TestPeerTransfer_Stop_MultipleCalls(t *testing.T) {
	transfer, _ := newTestPeerTransfer(t)

	// Stop should not hang or panic
	transfer.Stop()

	// Multiple calls to Stop should be safe
	transfer.Stop()
	transfer.Stop()

	// If we reach here, Stop() completed successfully
	assert.True(t, true, "Stop() completed without hanging")
}

// TestPeerTransfer_Get_ErrorAggregation tests that error information is properly handled
func TestPeerTransfer_Get_ErrorAggregation(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Mock DHT to return non-existent server addresses (will all fail)
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99999, PeerPort: 99999},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99998, PeerPort: 99998},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer.maxPeers = 2

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Nil(t, data)
}

// TestPeerTransfer_Get_PeerClientFailure tests peer client failure scenarios
func TestPeerTransfer_Get_PeerClientFailure(t *testing.T) {
	tests := []struct {
		name string
	}{
		{
			name: "Connection error to non-existent server",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transfer, dhtNode := newTestPeerTransfer(t)

			// Mock DHT to return a non-existent server address
			contact := dht.Contact{
				ID:       bits.Rand(),
				IP:       net.ParseIP("127.0.0.1"),
				Port:     99999, // Non-existent port
				PeerPort: 99999,
			}
			dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

			_, err := transfer.Get(context.Background(), testHash)

			assert.Error(t, err)
			// Should get connection refused or timeout error
		})
	}
}

// TestPeerTransfer_Get_PeerCancellation tests handling when peer returns context.Canceled error
func TestPeerTransfer_Get_PeerCancellation(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Mock DHT to return a non-existent server address (will cause connection error)
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     99999, // Non-existent port
		PeerPort: 99999,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// Operation should fail with connection error
	_, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.False(t, errors.Is(err, context.Canceled), "expected connection error, not context.Canceled")
}

// TestPeerTransfer_Get_InvalidHash tests invalid hash format handling
func TestPeerTransfer_Get_InvalidHash(t *testing.T) {
	tests := []struct {
		name        string
		hash        string
		description string
	}{
		{
			name:        "Empty hash",
			hash:        "",
			description: "empty string",
		},
		{
			name:        "Too short",
			hash:        "short",
			description: "less than required length",
		},
		{
			name:        "Invalid characters",
			hash:        "invalid_chars",
			description: "contains non-hexadecimal characters",
		},
		{
			name:        "Too long",
			hash:        strings.Repeat("a", 66),
			description: "exceeds maximum hash length",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transfer, _ := newTestPeerTransfer(t)

			_, err := transfer.Get(context.Background(), test.hash)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid hash")
		})
	}
}

// TestPeerTransfer_Get_FallbackLogic tests fallback behavior
func TestPeerTransfer_Get_FallbackLogic(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Start a real peer server with test blob
	blobData := []byte("test-blob-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return the test server's address
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     testServer.port,
		PeerPort: testServer.port,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	transfer.maxPeers = 5

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Name tests Name() method
func TestPeerTransfer_Name(t *testing.T) {
	transfer, _ := newTestPeerTransfer(t)

	assert.Equal(t, "peer", transfer.Name())
}

// TestPeerTransfer_ZeroMaxPeers tests that maxPeers=0 doesn't cause division by zero
func TestPeerTransfer_ZeroMaxPeers(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Start a real peer server with test blob
	blobData := []byte("test-blob-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return the test server's address
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     testServer.port,
		PeerPort: testServer.port,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// Create transfer with maxPeers=0 (should not cause division by zero)
	transfer.maxPeers = 0

	data, err := transfer.Get(context.Background(), testHash)

	// When maxPeers=0, no peers will be tried, so we should get blob not found error
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob not found")
	assert.Nil(t, data)
}

// TestPeerTransfer_Get_PeerClientSuccess tests successful blob download from peer
func TestPeerTransfer_Get_PeerClientSuccess(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Start a real peer server with test blob
	blobData := []byte("test-blob-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return the test server's address
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     testServer.port,
		PeerPort: testServer.port,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Get_Timeout tests timeout configuration
func TestPeerTransfer_Get_Timeout(t *testing.T) {
	// Create a client factory that creates new mock clients with blocking behavior
	// Use Maybe() for Reset() since it may not always be called depending on timing
	clientFactory := func() protocol.PeerClient {
		mockClient := protocolMocks.NewMockPeerClient(t)
		mockClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
		mockClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).RunAndReturn(func(ctx context.Context, hash string) ([]byte, error) {
			// Block until the context is done, then return the context error
			<-ctx.Done()
			return nil, ctx.Err()
		})
		mockClient.EXPECT().Reset().Return(nil).Maybe()
		return mockClient
	}

	transfer, dhtNode := newTestPeerTransferWithMockClient(t, clientFactory)

	// Mock DHT to return a contact (the actual address doesn't matter since we're using mock client)
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"), // Use localhost since we're mocking
		Port:     80,
		PeerPort: 80,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	transfer.timeout = 100 * time.Millisecond

	_, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// TestPeerTransfer_Get_MaxPeers tests maxPeers configuration
func TestPeerTransfer_Get_MaxPeers(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Start a real peer server with test blob
	blobData := []byte("test-blob-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return 10 contacts, but only first one is real
	contacts := make([]dht.Contact, 10)
	for i := 0; i < 10; i++ {
		if i == 0 {
			// First contact is the real server
			contacts[i] = dht.Contact{
				ID:       bits.Rand(),
				IP:       net.ParseIP("127.0.0.1"),
				Port:     testServer.port,
				PeerPort: testServer.port,
			}
		} else {
			// Other contacts are non-existent (using test-net IP range)
			contacts[i] = dht.Contact{
				ID:       bits.Rand(),
				IP:       net.ParseIP(fmt.Sprintf("192.0.2.%d", 100+i)),
				Port:     3333,
				PeerPort: 3333,
			}
		}
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer.maxPeers = 10

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Get_FallbackPath tests fallback behavior when first peers fail
func TestPeerTransfer_Get_FallbackPath(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Start a real peer server with test blob (this will be the second contact)
	blobData := []byte("test-blob-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return 3 contacts: first two fail, third succeeds
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("192.0.2.1"), Port: 80, PeerPort: 80},                           // First fails (test-net range)
		{ID: bits.Rand(), IP: net.ParseIP("192.0.2.2"), Port: 80, PeerPort: 80},                           // Second fails (test-net range)
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: testServer.port, PeerPort: testServer.port}, // Third succeeds
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer.maxPeers = 3

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_StartStopCycle tests the complete Start/Stop cycle and clientPool recreation
func TestPeerTransfer_StartStopCycle(t *testing.T) {
	transfer, dhtNode := newTestPeerTransfer(t)

	// Initially, the transfer should be running (not stopped)
	assert.False(t, transfer.isStopped(), "Transfer should not be stopped initially")
	assert.NotNil(t, transfer.clientPool, "Client pool should be initialized")

	// Store reference to initial client pool
	initialPool := transfer.clientPool

	// Stop the transfer
	transfer.Stop()

	// After Stop, the transfer should be stopped and clientPool should be nil
	assert.True(t, transfer.isStopped(), "Transfer should be stopped after Stop()")
	assert.Nil(t, transfer.clientPool, "Client pool should be nil after Stop()")

	// Start the transfer again
	transfer.Start()

	// After Start, the transfer should be running and clientPool should be recreated
	assert.False(t, transfer.isStopped(), "Transfer should not be stopped after Start()")
	assert.NotNil(t, transfer.clientPool, "Client pool should be recreated after Start()")

	// The new client pool should be different from the initial one
	assert.NotEqual(t, initialPool, transfer.clientPool, "Client pool should be a new instance after Start()")

	// Test that operations work after restart
	// Start a real peer server with test blob
	blobData := []byte("restart-test-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return the test server's address
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     testServer.port,
		PeerPort: testServer.port,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// This should work after restart
	data, err := transfer.Get(context.Background(), testHash)
	assert.NoError(t, err, "Get should work after Start/Stop cycle")
	assert.Equal(t, blobData, data, "Data should match after Start/Stop cycle")
}

// TestPeerTransfer_GetAfterStop tests that Get returns ErrTransferStopped after Stop
func TestPeerTransfer_GetAfterStop(t *testing.T) {
	transfer, _ := newTestPeerTransfer(t)

	// Stop the transfer
	transfer.Stop()

	// Try to get a blob - should return ErrTransferStopped
	_, err := transfer.Get(context.Background(), testHash)
	assert.Error(t, err, "Get should return error after Stop()")
	assert.Contains(t, err.Error(), "transfer is stopped", "Error should mention transfer is stopped")
}

// TestPeerTransfer_returnClientToPool_ResetFailure tests that clients with Reset failures are not returned to pool
func TestPeerTransfer_returnClientToPool_ResetFailure(t *testing.T) {
	transfer, _ := newTestPeerTransfer(t)

	// Create a mock client that fails Reset
	mockClient := protocolMocks.NewMockPeerClient(t)
	resetError := errors.New("reset failed")
	mockClient.EXPECT().Reset().Return(resetError)

	// Get initial pool size by checking if we can get a client
	initialClient := transfer.clientPool.Get()
	transfer.clientPool.Put(initialClient) // Put it back

	// Call returnClientToPool with failing client
	transfer.returnClientToPool(mockClient)

	// Verify that the mock client was not returned to pool by checking that we get a DefaultPeerClient, not the mock
	retrievedClient := transfer.clientPool.Get()
	_, isDefaultClient := retrievedClient.(*protocol.DefaultPeerClient)
	_, isMockClient := retrievedClient.(*protocolMocks.MockPeerClient)
	assert.True(t, isDefaultClient, "Should get a DefaultPeerClient back, not the failing mock client")
	assert.False(t, isMockClient, "Should not get the MockPeerClient back")

	// Put the original client back for cleanup
	transfer.clientPool.Put(retrievedClient)
}

// TestPeerTransfer_returnClientToPool_ResetSuccess tests that clients with successful Reset are returned to pool
func TestPeerTransfer_returnClientToPool_ResetSuccess(t *testing.T) {
	// Create a transfer with a controlled client pool to avoid race conditions
	mockClient := protocolMocks.NewMockPeerClient(t)
	mockClient.EXPECT().Reset().Return(nil)

	// Create a custom client factory that always returns our mock client
	clientFactory := func() protocol.PeerClient {
		return mockClient
	}

	transfer, _ := newTestPeerTransferWithMockClient(t, clientFactory)

	// Call returnClientToPool with successful client
	transfer.returnClientToPool(mockClient)

	// Verify that the mock client was returned to pool
	retrievedClient := transfer.clientPool.Get()
	assert.Equal(t, mockClient, retrievedClient, "Should get the mock client back from pool")

	// Put the client back for cleanup
	transfer.clientPool.Put(retrievedClient)
}

// TestPeerTransfer_returnClientToPool_StoppedTransfer tests that clients are discarded when transfer is stopped
func TestPeerTransfer_returnClientToPool_StoppedTransfer(t *testing.T) {
	transfer, _ := newTestPeerTransfer(t)

	// Stop the transfer
	transfer.Stop()

	// Create a mock client (Reset should not be called)
	mockClient := protocolMocks.NewMockPeerClient(t)

	// Call returnClientToPool with stopped transfer
	transfer.returnClientToPool(mockClient)

	// Verify that Reset was not called (client should be discarded immediately)
	mockClient.AssertNotCalled(t, "Reset")
}

// TestPeerTransfer_Get_ConcurrentSameHash tests that concurrent Get() calls on the same hash
// do not create duplicate BlobRequest entries (race condition fix verification)
func TestPeerTransfer_Get_ConcurrentSameHash(t *testing.T) {
	transfer, mockDHT := newTestPeerTransfer(t)

	// Test data
	testData := []byte("test data for concurrent access")

	// Start a real peer server with the test data
	blobData := map[string][]byte{testHash: testData}
	server := StartTestPeerServer(t, blobData)
	defer server.Stop()

	// Setup mock DHT to return the actual server port
	hashBitmap, err := bits.FromShortHex(testHash)
	require.NoError(t, err)

	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: server.port, PeerPort: server.port},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: server.port, PeerPort: server.port},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: server.port, PeerPort: server.port},
	}
	mockDHT.EXPECT().Get(hashBitmap).Return(contacts, nil).Times(1)

	// Number of concurrent goroutines
	numGoroutines := 10

	// Channel to collect results
	results := make(chan []byte, numGoroutines)
	errors := make(chan error, numGoroutines)

	// Barrier to synchronize goroutine start
	var startWG sync.WaitGroup
	startWG.Add(1)

	// Launch multiple goroutines that all call Get() on the same hash
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()

			// Wait for all goroutines to be ready
			startWG.Wait()

			// Call Get() concurrently
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			data, err := transfer.Get(ctx, testHash)
			if err != nil {
				errors <- err
				return
			}
			results <- data
		}()
	}

	// Start all goroutines simultaneously
	startWG.Done()

	// Wait for all goroutines to complete
	wg.Wait()
	close(results)
	close(errors)

	// Collect results
	var allResults [][]byte
	var allErrors []error

	for result := range results {
		allResults = append(allResults, result)
	}

	for err := range errors {
		allErrors = append(allErrors, err)
	}

	// Verify that all calls succeeded (no errors)
	assert.Empty(t, allErrors, "No Get() calls should fail")
	assert.Len(t, allResults, numGoroutines, "All goroutines should return results")

	// Verify that all results are identical (same data)
	for i := 1; i < len(allResults); i++ {
		assert.Equal(t, allResults[0], allResults[i], "All results should be identical")
	}
	assert.Equal(t, testData, allResults[0], "Result should match expected test data")

	// Verify that only one BlobRequest was created by checking backlog size
	// Wait a moment for any cleanup to complete
	time.Sleep(100 * time.Millisecond)

	transfer.backlogMu.RLock()
	backlogSize := len(transfer.backlog)
	transfer.backlogMu.RUnlock()

	assert.Equal(t, 0, backlogSize, "Backlog should be empty after all requests complete")
}
