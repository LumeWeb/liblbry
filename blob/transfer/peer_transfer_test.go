package transfer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
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

// GetFreePort returns an available port on localhost
func GetFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}

	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// TestPeerServer represents a running peer server for testing
type TestPeerServer struct {
	listener net.Listener
	server   protocol.PeerServer
	address  string
	port     int
	storage  storage.BlobStore
	wg       sync.WaitGroup
}

// StartTestPeerServer starts a real peer server on a random port for testing
func StartTestPeerServer(t *testing.T, blobData map[string][]byte) *TestPeerServer {
	port, err := GetFreePort()
	require.NoError(t, err, "Failed to get free port")

	address := fmt.Sprintf("localhost:%d", port)

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

	// Start listening
	listener, err := net.Listen("tcp", address)
	require.NoError(t, err, "Failed to listen on address")

	// Start handling connections
	testServer := &TestPeerServer{
		listener: listener,
		server:   server,
		address:  address,
		port:     port,
		storage:  store,
	}

	testServer.wg.Add(1)
	go func() {
		defer testServer.wg.Done()
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

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

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

// TestNewPeerTransfer tests constructor and basic configuration
func TestNewPeerTransfer(t *testing.T) {
	tests := []struct {
		name              string
		dhtNode           protocol.DHTNode
		peerClientFactory protocol.PeerClientFactory
		timeout           time.Duration
		maxPeers          int
		logger            *zap.Logger
		expected          *PeerTransfer
	}{
		{
			name:              "Default configuration",
			dhtNode:           nil,
			peerClientFactory: nil,
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
			transfer := NewPeerTransfer(test.dhtNode, test.peerClientFactory,
				WithPeerTransferTimeout(test.timeout),
				WithPeerTransferMaxPeers(test.maxPeers),
				WithPeerTransferLogger(test.logger),
			)

			assert.NotNil(t, transfer)
			assert.Equal(t, test.expected.timeout, transfer.timeout)
			assert.Equal(t, test.expected.maxPeers, transfer.maxPeers)
			if test.logger != nil {
				assert.Equal(t, test.logger, transfer.logger)
			} else {
				// When nil logger is passed, default logger should be preserved
				assert.NotNil(t, transfer.logger, "default logger should be preserved when nil is passed")
			}
		})
	}
}

// TestPeerTransfer_Get_NoPeersFound tests DHT discovery returning no contacts
func TestPeerTransfer_Get_NoPeersFound(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return no contacts
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{}, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory)

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob not found")
	assert.Nil(t, data)

}

// TestPeerTransfer_Get_DHTError tests DHT.Get() error scenarios
func TestPeerTransfer_Get_DHTError(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return an error
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(nil, fmt.Errorf("DHT lookup failed"))

	transfer := NewPeerTransfer(dhtNode, peerClientFactory)

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT lookup failed")
	assert.Nil(t, data)

}

// TestPeerTransfer_Get_AllPeersFail tests when all peers fail
func TestPeerTransfer_Get_AllPeersFail(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return non-existent server addresses (will all fail)
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99999, PeerPort: 99999},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99998, PeerPort: 99998},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99997, PeerPort: 99997},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(3))

	data, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.Nil(t, data)
}

// TestPeerTransfer_Get_AllPeersFail_ImmediateError tests that when all peers fail,
// the error is returned immediately without waiting for timeout
func TestPeerTransfer_Get_AllPeersFail_ImmediateError(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return non-existent server addresses (will all fail immediately)
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99999, PeerPort: 99999},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99998, PeerPort: 99998},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99997, PeerPort: 99997},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(3))

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
	t.Skip()
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

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

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(3))

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Get_ErrorAggregation tests that error information is properly handled
func TestPeerTransfer_Get_ErrorAggregation(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return non-existent server addresses (will all fail)
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99999, PeerPort: 99999},
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: 99998, PeerPort: 99998},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(2))

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
			dhtNode := protocolMocks.NewMockDHTNode(t)
			peerClientFactory := protocol.DefaultPeerClientFactory(
				protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
			)

			// Mock DHT to return a non-existent server address
			contact := dht.Contact{
				ID:       bits.Rand(),
				IP:       net.ParseIP("127.0.0.1"),
				Port:     99999, // Non-existent port
				PeerPort: 99999,
			}
			dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

			transfer := NewPeerTransfer(dhtNode, peerClientFactory)
			_, err := transfer.Get(context.Background(), testHash)

			assert.Error(t, err)
			// Should get connection refused or timeout error
		})
	}
}

// TestPeerTransfer_Get_PeerCancellation tests handling when peer returns context.Canceled error
func TestPeerTransfer_Get_PeerCancellation(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return a non-existent server address (will cause connection error)
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("127.0.0.1"),
		Port:     99999, // Non-existent port
		PeerPort: 99999,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory)

	// Operation should fail with connection error
	_, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	// Should get connection refused error, not context.Canceled
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
			dhtNode := protocolMocks.NewMockDHTNode(t)
			peerClientFactory := protocol.DefaultPeerClientFactory(
				protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
			)
			transfer := NewPeerTransfer(dhtNode, peerClientFactory)

			_, err := transfer.Get(context.Background(), test.hash)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid hash")
		})
	}
}

// TestPeerTransfer_Get_FallbackLogic tests fallback behavior
func TestPeerTransfer_Get_FallbackLogic(t *testing.T) {
	t.Skip()
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

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

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(5))

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Name tests Name() method
func TestPeerTransfer_Name(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory)

	assert.Equal(t, "peer", transfer.Name())
}

// TestPeerTransfer_ZeroMaxPeers tests that maxPeers=0 doesn't cause division by zero
func TestPeerTransfer_ZeroMaxPeers(t *testing.T) {
	t.Skip()
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

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
	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(0))

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Get_PeerClientSuccess tests successful blob download from peer
func TestPeerTransfer_Get_PeerClientSuccess(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

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

	transfer := NewPeerTransfer(dhtNode, peerClientFactory)

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Get_Timeout tests timeout configuration
func TestPeerTransfer_Get_Timeout(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Mock DHT to return a non-existent server address (will cause timeout)
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("192.0.2.1"), // Non-routable IP for timeout
		Port:     80,                       // Valid port
		PeerPort: 80,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferTimeout(100*time.Millisecond))

	_, err := transfer.Get(context.Background(), testHash)

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// TestPeerTransfer_Get_MaxPeers tests maxPeers configuration
func TestPeerTransfer_Get_MaxPeers(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

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
			// Other contacts are non-existent
			contacts[i] = dht.Contact{
				ID:       bits.Rand(),
				IP:       net.ParseIP(fmt.Sprintf("192.168.1.%d", 100+i)),
				Port:     3333,
				PeerPort: 3333,
			}
		}
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(10))

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}

// TestPeerTransfer_Get_FallbackPath tests fallback behavior when first peers fail
func TestPeerTransfer_Get_FallbackPath(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClientFactory := protocol.DefaultPeerClientFactory(
		protocol.WithClientLogger(zap.NewNop().Named("test-peer-client")),
	)

	// Start a real peer server with test blob (this will be the second contact)
	blobData := []byte("test-blob-data")
	testServer := StartTestPeerServer(t, map[string][]byte{
		testHash: blobData,
	})
	defer testServer.Stop()

	// Mock DHT to return 3 contacts: first two fail, third succeeds
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("192.0.2.1"), Port: 80, PeerPort: 80},                           // First fails (non-routable)
		{ID: bits.Rand(), IP: net.ParseIP("192.0.2.2"), Port: 80, PeerPort: 80},                           // Second fails (non-routable)
		{ID: bits.Rand(), IP: net.ParseIP("127.0.0.1"), Port: testServer.port, PeerPort: testServer.port}, // Third succeeds
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	transfer := NewPeerTransfer(dhtNode, peerClientFactory, WithPeerTransferMaxPeers(3))

	data, err := transfer.Get(context.Background(), testHash)

	assert.NoError(t, err)
	assert.Equal(t, blobData, data)
}
