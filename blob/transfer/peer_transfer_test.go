package transfer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/lbryio/lbry.go/v2/dht"
	"github.com/lbryio/lbry.go/v2/dht/bits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.uber.org/zap"
)

// TestNewPeerTransfer tests constructor and basic configuration
func TestNewPeerTransfer(t *testing.T) {
	tests := []struct {
		name       string
		dhtNode    protocol.DHTNode
		peerClient protocol.PeerClient
		timeout    time.Duration
		maxPeers   int
		logger     *zap.Logger
		expected   *PeerTransfer
	}{
		{
			name:       "Default configuration",
			dhtNode:    nil,
			peerClient: nil,
			timeout:    30 * time.Second,
			maxPeers:   5,
			logger:     nil,
			expected: &PeerTransfer{
				timeout:  30 * time.Second,
				maxPeers: 5,
			},
		},
		{
			name:       "With custom DHT and peer client",
			dhtNode:    protocolMocks.NewMockDHTNode(t),
			peerClient: protocolMocks.NewMockPeerClient(t),
			timeout:    60 * time.Second,
			maxPeers:   10,
			logger:     zap.NewNop(),
			expected: &PeerTransfer{
				timeout:  60 * time.Second,
				maxPeers: 10,
				logger:   zap.NewNop(),
			},
		},
		{
			name:       "With logger",
			dhtNode:    protocolMocks.NewMockDHTNode(t),
			peerClient: protocolMocks.NewMockPeerClient(t),
			timeout:    30 * time.Second,
			maxPeers:   5,
			logger:     zap.NewNop().Named("test-logger"),
			expected: &PeerTransfer{
				timeout:  30 * time.Second,
				maxPeers: 5,
				logger:   zap.NewNop().Named("test-logger"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transfer := NewPeerTransfer(test.dhtNode, test.peerClient,
				WithPeerTransferTimeout(test.timeout),
				WithPeerTransferMaxPeers(test.maxPeers),
				WithPeerTransferLogger(test.logger),
			)

			assert.NotNil(t, transfer)
			assert.Equal(t, test.expected.timeout, transfer.timeout)
			assert.Equal(t, test.expected.maxPeers, transfer.maxPeers)
			assert.Equal(t, test.expected.logger, transfer.logger)
		})
	}
}

// TestPeerTransfer_Get_Success tests successful blob retrieval from first peer
func TestPeerTransfer_Get_Success(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return a contact
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("192.168.1.100"),
		Port:     3333,
		PeerPort: 3333,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// Mock peer client to succeed on first attempt
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	data, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_NoPeersFound tests DHT discovery returning no contacts
func TestPeerTransfer_Get_NoPeersFound(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return no contacts
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{}, nil)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	data, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob not found")
	assert.Nil(t, data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_AllPeersFail tests fallback logic when all peers fail
func TestPeerTransfer_Get_AllPeersFail(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return 3 contacts
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.101"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.102"), Port: 3333, PeerPort: 3333},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	// Mock all peer clients to fail
	for i := 0; i < 3; i++ {
		peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("peer %d failed", i+1))
	}

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(3))

	data, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch blob")
	assert.Nil(t, data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_PeerClientSuccess tests successful blob download from peer
func TestPeerTransfer_Get_PeerClientSuccess(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return a contact
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("192.168.1.100"),
		Port:     3333,
		PeerPort: 3333,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// Mock peer client to succeed
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	data, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_PeerClientFailure tests peer client failure scenarios
func TestPeerTransfer_Get_PeerClientFailure(t *testing.T) {
	tests := []struct {
		name        string
		setupError  error
		clientError error
		expectError bool
	}{
		{
			name:        "Connection error",
			setupError:  fmt.Errorf("setup failed"),
			clientError: fmt.Errorf("connection failed"),
			expectError: true,
		},
		{
			name:        "Timeout error",
			setupError:  nil,
			clientError: fmt.Errorf("timeout"),
			expectError: true,
		},
		{
			name:        "Context cancellation",
			setupError:  nil,
			clientError: context.Canceled,
			expectError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dhtNode := protocolMocks.NewMockDHTNode(t)
			peerClient := protocolMocks.NewMockPeerClient(t)

			// Mock DHT to return a contact
			contact := dht.Contact{
				ID:       bits.Rand(),
				IP:       net.ParseIP("192.168.1.100"),
				Port:     3333,
				PeerPort: 3333,
			}
			dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

			// Mock peer client based on test case
			switch test.name {
			case "Connection error":
				peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, test.clientError)
			case "Timeout error":
				peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, test.clientError)
			case "Context cancellation":
				peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, test.clientError)
			}

			transfer := NewPeerTransfer(dhtNode, peerClient)
			_, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

			assert.Error(t, err)
			assert.True(t, test.expectError)
			dhtNode.AssertExpectations(t)
			peerClient.AssertExpectations(t)
		})
	}
}

// TestPeerTransfer_Get_ContextCancellation tests context cancellation during operations
func TestPeerTransfer_Get_ContextCancellation(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return a contact
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("192.168.1.100"),
		Port:     3333,
		PeerPort: 3333,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// Mock peer client to handle context cancellation
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, context.Canceled)

	// Cancel context immediately after starting operation
	_, cancel := context.WithCancel(context.Background())
	cancel()

	transfer := NewPeerTransfer(dhtNode, peerClient)

	// Operation should be cancelled
	_, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.Canceled))
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
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
			hash:        "g" + string(make([]byte, 65)),
			description: "exceeds maximum hash length",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dhtNode := protocolMocks.NewMockDHTNode(t)
			peerClient := protocolMocks.NewMockPeerClient(t)
			transfer := NewPeerTransfer(dhtNode, peerClient)

			_, err := transfer.Get(test.hash)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid hash")
			dhtNode.AssertExpectations(t)
			peerClient.AssertExpectations(t)
		})
	}
}

// TestPeerTransfer_Get_Timeout tests timeout configuration
func TestPeerTransfer_Get_Timeout(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return a contact
	contact := dht.Contact{
		ID:       bits.Rand(),
		IP:       net.ParseIP("192.168.1.100"),
		Port:     3333,
		PeerPort: 3333,
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{contact}, nil)

	// Mock peer client to timeout after delay
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("timeout"))

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferTimeout(100*time.Millisecond))

	_, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_MaxPeers tests maxPeers configuration
func TestPeerTransfer_Get_MaxPeers(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return 10 contacts
	contacts := make([]dht.Contact, 10)
	for i := 0; i < 10; i++ {
		contacts[i] = dht.Contact{
			ID:       bits.Rand(),
			IP:       net.ParseIP("192.168.1.100"),
			Port:     3333,
			PeerPort: 3333,
		}
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	// Mock peer client to succeed
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(10))

	data, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_FallbackLogic tests fallback behavior
func TestPeerTransfer_Get_FallbackLogic(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return 5 contacts
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.101"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.102"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.103"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.104"), Port: 3333, PeerPort: 3333},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	// Mock peer clients: first succeeds, others fail
	for i := 0; i < 5; i++ {
		if i == 0 {
			peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)
		} else {
			peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("peer %d failed", i+1))
		}
	}

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(5))

	data, err := transfer.Get("76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Name tests Name() method
func TestPeerTransfer_Name(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	assert.Equal(t, "peer", transfer.Name())
}

// TestPeerTransfer_Options tests configuration options
func TestPeerTransfer_Options(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	transfer := NewPeerTransfer(
		dhtNode,
		peerClient,
		WithPeerTransferTimeout(45*time.Second),
		WithPeerTransferMaxPeers(3),
		WithPeerTransferLogger(zap.NewNop().Named("test-options")),
	)

	assert.Equal(t, 45*time.Second, transfer.timeout)
	assert.Equal(t, 3, transfer.maxPeers)
	assert.NotNil(t, transfer.logger)
}

// TableDriven tests for comprehensive scenario coverage
func TestPeerTransfer_TableDrivenTests(t *testing.T) {
	tests := []struct {
		name        string
		hash        string
		contacts    []dht.Contact
		expectError bool
		expectData  bool
	}{
		// Success cases
		{
			name:        "Single peer success",
			hash:        "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b",
			contacts:    []dht.Contact{{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333}},
			expectError: false,
			expectData:  true,
		},
		{
			name: "Multiple peers, first succeeds",
			hash: "47ebe801fbdae21f43239f0ec7c1b1d0e8072c3c72963078c22c447bac59e45cfa065581410e5a67390df90e615a27e2",
			contacts: []dht.Contact{
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333},
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.101"), Port: 3333, PeerPort: 3333},
			},
			expectError: false,
			expectData:  true,
		},
		{
			name: "Multiple peers, all fail",
			hash: "42bdb10fa69c9082304dd6af0881fcfe6f4c3cb5eebc75fdda49da043fb06b7d5701bea596f6f9b09bc420465eba1e25",
			contacts: []dht.Contact{
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333},
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.101"), Port: 3333, PeerPort: 3333},
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.102"), Port: 3333, PeerPort: 3333},
			},
			expectError: true,
			expectData:  false,
		},
		// Error cases
		{
			name:        "No DHT node",
			hash:        "86611c066b95318cd2ed08482cdb91b785cea7479564430d25cec003078fb412732f686380fdbae918c16490f5074fb3",
			contacts:    []dht.Contact{}, // Empty slice, not nil
			expectError: true,
			expectData:  false,
		},
		{
			name:        "No peer client",
			hash:        "1c1e2c3ff5d5740054f39f10291a9eae6131851221312d3a1c550323f2631166737ee6a68e4c3ddc793527a84f25bdd8",
			contacts:    []dht.Contact{{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333}},
			expectError: true,
			expectData:  false,
		},
		{
			name:        "Invalid hash",
			hash:        "invalid-hash",
			contacts:    []dht.Contact{{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333}},
			expectError: true,
			expectData:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dhtNode := protocolMocks.NewMockDHTNode(t)
			peerClient := protocolMocks.NewMockPeerClient(t)

			// Handle different test scenarios
			if test.name == "Invalid hash" {
				// For invalid hash, no DHT or peer client calls should be made
				transfer := NewPeerTransfer(dhtNode, peerClient)
				data, err := transfer.Get(test.hash)

				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid hash")
				assert.Nil(t, data)
				// No expectations to assert since no calls should be made
				return
			}

			// Always set up DHT expectations since Get is always called
			dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(test.contacts, nil)

			// Only set up peer client expectations if we expect calls to be made
			// For "No DHT node" case with empty contacts, no GetBlob calls should be made
			if len(test.contacts) > 0 {
				if test.expectError {
					peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("connection failed"))
				} else if test.expectData {
					peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)
				}
			}

			transfer := NewPeerTransfer(dhtNode, peerClient)

			data, err := transfer.Get(test.hash)

			if test.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if test.expectData {
				assert.NotNil(t, data)
			} else {
				assert.Nil(t, data)
			}

			dhtNode.AssertExpectations(t)
			peerClient.AssertExpectations(t)
		})
	}
}
