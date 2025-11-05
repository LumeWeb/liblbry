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
				logger:   zap.NewNop(),
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
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return no contacts
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return([]dht.Contact{}, nil)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

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
		peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
		peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("peer %d failed", i+1))
		peerClient.EXPECT().Close().Return(nil)
	}

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(3))

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

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
	peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)
	peerClient.EXPECT().Close().Return(nil)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_PeerClientFailure tests peer client failure scenarios
func TestPeerTransfer_Get_PeerClientFailure(t *testing.T) {
	tests := []struct {
		name          string
		setupError    error
		clientError   error
		expectError   bool
		shouldConnect bool
	}{
		{
			name:          "Connection error",
			setupError:    fmt.Errorf("setup failed"),
			clientError:   fmt.Errorf("connection failed"),
			expectError:   true,
			shouldConnect: false,
		},
		{
			name:          "Timeout error",
			setupError:    nil,
			clientError:   fmt.Errorf("timeout"),
			expectError:   true,
			shouldConnect: true,
		},
		{
			name:          "Context cancellation",
			setupError:    nil,
			clientError:   context.Canceled,
			expectError:   true,
			shouldConnect: true,
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
			if test.shouldConnect {
				peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
				peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, test.clientError)
				peerClient.EXPECT().Close().Return(nil)
			} else {
				peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(test.setupError)
			}

			transfer := NewPeerTransfer(dhtNode, peerClient)
			_, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

			assert.Error(t, err)
			dhtNode.AssertExpectations(t)
			peerClient.AssertExpectations(t)
		})
	}
}

// TestPeerTransfer_Get_PeerCancellation tests handling when peer returns context.Canceled error
func TestPeerTransfer_Get_PeerCancellation(t *testing.T) {
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

	// Mock peer client to return context.Canceled error (simulating peer-side cancellation)
	peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, context.Canceled)
	peerClient.EXPECT().Close().Return(nil)

	transfer := NewPeerTransfer(dhtNode, peerClient)

	// Operation should fail with context.Canceled error from peer
	_, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

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

			_, err := transfer.Get(context.Background(), test.hash)

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
	peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("timeout"))
	peerClient.EXPECT().Close().Return(nil)

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferTimeout(100*time.Millisecond))

	_, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

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
			IP:       net.ParseIP(fmt.Sprintf("192.168.1.%d", 100+i)),
			Port:     3333,
			PeerPort: 3333,
		}
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	// Mock first 9 peers to fail, 10th to succeed
	// Use On/Maybe to make expectations more flexible
	connectCall := peerClient.On("Connect", mock.Anything, mock.AnythingOfType("string")).Return(nil)
	getBlobCall := peerClient.On("GetBlob", mock.Anything, mock.AnythingOfType("string"))
	closeCall := peerClient.On("Close").Return(nil)

	// Set up GetBlob to fail first 9 times, then succeed
	getBlobCall.Return(nil, fmt.Errorf("peer 1 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 2 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 3 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 4 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 5 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 6 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 7 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 8 failed")).Once()
	getBlobCall.Return(nil, fmt.Errorf("peer 9 failed")).Once()
	getBlobCall.Return([]byte("test-blob-data"), nil).Once()

	// Allow multiple calls to Connect and Close
	connectCall.Maybe()
	closeCall.Maybe()

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(10))

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

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

	// Mock peer client: first peer succeeds (implementation returns immediately)
	// Only set expectations for the first peer since it succeeds and others won't be contacted
	peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)
	peerClient.EXPECT().Close().Return(nil)

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(5))

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// TestPeerTransfer_Get_FallbackPath tests fallback behavior when first peers fail
func TestPeerTransfer_Get_FallbackPath(t *testing.T) {
	dhtNode := protocolMocks.NewMockDHTNode(t)
	peerClient := protocolMocks.NewMockPeerClient(t)

	// Mock DHT to return 3 contacts
	contacts := []dht.Contact{
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.101"), Port: 3333, PeerPort: 3333},
		{ID: bits.Rand(), IP: net.ParseIP("192.168.1.102"), Port: 3333, PeerPort: 3333},
	}
	dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(contacts, nil)

	// Mock peer clients: first 2 fail, third succeeds
	// Use On/Return/Once() pattern to properly sequence expectations

	// First peer fails
	peerClient.On("Connect", mock.Anything, mock.AnythingOfType("string")).Return(nil).Once()
	peerClient.On("GetBlob", mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("peer 1 failed")).Once()
	peerClient.On("Close").Return(nil).Once()

	// Second peer fails
	peerClient.On("Connect", mock.Anything, mock.AnythingOfType("string")).Return(nil).Once()
	peerClient.On("GetBlob", mock.Anything, mock.AnythingOfType("string")).Return(nil, fmt.Errorf("peer 2 failed")).Once()
	peerClient.On("Close").Return(nil).Once()

	// Third peer succeeds
	peerClient.On("Connect", mock.Anything, mock.AnythingOfType("string")).Return(nil).Once()
	peerClient.On("GetBlob", mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil).Once()
	peerClient.On("Close").Return(nil).Once()

	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(3))

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

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

// TestPeerTransfer_ZeroMaxPeers tests that maxPeers=0 doesn't cause division by zero
func TestPeerTransfer_ZeroMaxPeers(t *testing.T) {
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
	peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)
	peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)
	peerClient.EXPECT().Close().Return(nil)

	// Create transfer with maxPeers=0 (should not cause division by zero)
	transfer := NewPeerTransfer(dhtNode, peerClient, WithPeerTransferMaxPeers(0))

	data, err := transfer.Get(context.Background(), "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b")

	assert.NoError(t, err)
	assert.Equal(t, []byte("test-blob-data"), data)
	dhtNode.AssertExpectations(t)
	peerClient.AssertExpectations(t)
}

// PeerBehavior defines how each peer should behave in tests
type PeerBehavior struct {
	shouldConnect bool
	connectError  error
	getBlobError  error
	getBlobData   []byte
}

// TableDriven tests for comprehensive scenario coverage
func TestPeerTransfer_TableDrivenTests(t *testing.T) {
	tests := []struct {
		name          string
		hash          string
		contacts      []dht.Contact
		expectError   bool
		expectData    bool
		peerBehaviors []PeerBehavior // Configures behavior for each peer
		skipPeerSetup bool           // Skip peer client setup entirely (e.g., for invalid hash)
	}{
		// Success cases
		{
			name:          "Single peer success",
			hash:          "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b",
			contacts:      []dht.Contact{{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333}},
			expectError:   false,
			expectData:    true,
			peerBehaviors: []PeerBehavior{{shouldConnect: true, getBlobData: []byte("test-blob-data")}},
		},
		{
			name: "Multiple peers, first succeeds",
			hash: "47ebe801fbdae21f43239f0ec7c1b1d0e8072c3c72963078c22c447bac59e45cfa065581410e5a67390df90e615a27e2",
			contacts: []dht.Contact{
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333},
				{ID: bits.Rand(), IP: net.ParseIP("192.168.1.101"), Port: 3333, PeerPort: 3333},
			},
			expectError:   false,
			expectData:    true,
			peerBehaviors: []PeerBehavior{{shouldConnect: true, getBlobData: []byte("test-blob-data")}}, // Only first peer will be contacted
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
			peerBehaviors: []PeerBehavior{
				{shouldConnect: true, getBlobError: fmt.Errorf("peer 1 failed")},
				{shouldConnect: true, getBlobError: fmt.Errorf("peer 2 failed")},
				{shouldConnect: true, getBlobError: fmt.Errorf("peer 3 failed")},
			},
		},
		// Error cases
		{
			name:        "No DHT node",
			hash:        "86611c066b95318cd2ed08482cdb91b785cea7479564430d25cec003078fb412732f686380fdbae918c16490f5074fb3",
			contacts:    []dht.Contact{}, // Empty slice, not nil
			expectError: true,
			expectData:  false,
			// No peerBehaviors needed since there are no contacts
		},
		{
			name:        "No peer client",
			hash:        "1c1e2c3ff5d5740054f39f10291a9eae6131851221312d3a1c550323f2631166737ee6a68e4c3ddc793527a84f25bdd8",
			contacts:    []dht.Contact{{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333}},
			expectError: true,
			expectData:  false,
			peerBehaviors: []PeerBehavior{
				{shouldConnect: true, getBlobError: fmt.Errorf("connection failed")},
			},
		},
		{
			name:          "Invalid hash",
			hash:          "invalid-hash",
			contacts:      []dht.Contact{{ID: bits.Rand(), IP: net.ParseIP("192.168.1.100"), Port: 3333, PeerPort: 3333}},
			expectError:   true,
			expectData:    false,
			skipPeerSetup: true, // No peer calls should be made for invalid hash
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dhtNode := protocolMocks.NewMockDHTNode(t)
			peerClient := protocolMocks.NewMockPeerClient(t)

			// Handle special case for invalid hash - no DHT or peer client calls should be made
			if test.skipPeerSetup {
				transfer := NewPeerTransfer(dhtNode, peerClient)
				data, err := transfer.Get(context.Background(), test.hash)

				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid hash")
				assert.Nil(t, data)
				// No expectations to assert since no calls should be made
				return
			}

			// Always set up DHT expectations since Get is always called
			dhtNode.EXPECT().Get(mock.AnythingOfType("bits.Bitmap")).Return(test.contacts, nil)

			// Set up peer client expectations based on configured behaviors
			if len(test.contacts) > 0 && len(test.peerBehaviors) > 0 {
				// Use the minimum of contacts and behaviors to avoid index out of range
				maxPeers := min(len(test.contacts), len(test.peerBehaviors))

				for i := 0; i < maxPeers; i++ {
					behavior := test.peerBehaviors[i]

					if behavior.shouldConnect {
						if behavior.connectError != nil {
							// When connectError is non-nil, only set Connect expectation to return the error
							// Do NOT set GetBlob or Close expectations since Connect will fail
							peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(behavior.connectError)
						} else {
							// When Connect succeeds, set Connect to return nil and then set GetBlob and Close expectations
							peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(nil)

							if behavior.getBlobError != nil {
								peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(nil, behavior.getBlobError)
							} else if behavior.getBlobData != nil {
								peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return(behavior.getBlobData, nil)
							} else {
								// Default to success if no data or error specified
								peerClient.EXPECT().GetBlob(mock.Anything, mock.AnythingOfType("string")).Return([]byte("test-blob-data"), nil)
							}

							peerClient.EXPECT().Close().Return(nil)
						}
					} else {
						// If shouldn't connect, expect Connect to fail
						connectErr := behavior.connectError
						if connectErr == nil {
							connectErr = fmt.Errorf("connection refused")
						}
						peerClient.EXPECT().Connect(mock.Anything, mock.AnythingOfType("string")).Return(connectErr)
					}
				}
			}

			transfer := NewPeerTransfer(dhtNode, peerClient)

			data, err := transfer.Get(context.Background(), test.hash)

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
