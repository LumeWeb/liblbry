package discovery

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dht "go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewPeerDiscovery(t *testing.T) {
	setup := setupTest(t)
	discovery := setup.discovery

	assert.NotNil(t, discovery)
	assert.Equal(t, setup.mockDHT, discovery.DHTNode())
	assert.Equal(t, 0, discovery.RetryAttempts())
	assert.Equal(t, 100*time.Millisecond, discovery.RetryDelay())
	assert.Empty(t, discovery.GetFixedPeers())
	assert.False(t, discovery.IsStopped())
}

func TestPeerDiscovery_SetRetryConfig(t *testing.T) {
	setup := setupTest(t)
	discovery := setup.discovery

	discovery.SetRetryConfig(5, 200*time.Millisecond)

	assert.Equal(t, 5, discovery.RetryAttempts())
	assert.Equal(t, 200*time.Millisecond, discovery.RetryDelay())
}

func TestPeerDiscovery_SetFixedPeers(t *testing.T) {
	setup := setupTest(t)
	discovery := setup.discovery

	contacts := createTestContacts()[:1] // Use first contact from helper

	discovery.SetFixedPeers(contacts)

	assert.Equal(t, contacts, discovery.GetFixedPeers())
}

func TestPeerDiscovery_StartStop(t *testing.T) {
	setup := setupTest(t)
	discovery := setup.discovery

	// Initially not stopped
	assert.False(t, discovery.IsStopped())

	// Stop it
	discovery.Stop()
	assert.True(t, discovery.IsStopped())

	// Start it again
	discovery.Start()
	assert.False(t, discovery.IsStopped())
}

func TestPeerDiscovery_GetFixedPeers(t *testing.T) {
	setup := setupTest(t)
	discovery := setup.discovery

	// Initially empty
	assert.Empty(t, discovery.GetFixedPeers())

	// Set some peers
	contacts := createTestContacts()[:1] // Use first contact from helper
	discovery.SetFixedPeers(contacts)

	assert.Equal(t, contacts, discovery.GetFixedPeers())
}

func TestPeerDiscovery_IsFixedPeer(t *testing.T) {
	setup := setupTest(t)
	discovery := setup.discovery

	// No fixed peers configured
	assert.False(t, discovery.IsFixedPeer(createTestContact("192.168.1.100", 4444, 3333)))

	// Configure fixed peers
	contacts := createTestContacts()
	discovery.SetFixedPeers(contacts)

	// Test fixed peer detection
	testCases := []struct {
		name     string
		contact  dht.Contact
		expected bool
	}{
		{
			name:     "Fixed peer 1",
			contact:  createTestContact("192.168.1.100", 4444, 3333),
			expected: true,
		},
		{
			name:     "Fixed peer 2",
			contact:  createTestContact("10.0.0.1", 4444, 4444),
			expected: true,
		},
		{
			name:     "Non-fixed peer",
			contact:  createTestContact("8.8.8.8", 4444, 5555),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expected, discovery.IsFixedPeer(tc.contact))
		})
	}
}

func TestPeerDiscovery_ParseFixedPeers(t *testing.T) {
	t.Run("WithDefaultResolver", func(t *testing.T) {
		setup := setupTest(t)
		discovery := setup.discovery

		t.Run("Valid IP addresses", func(t *testing.T) {
			addresses := []string{"192.168.1.100:3333", "10.0.0.1:4444"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			require.NoError(t, err)
			require.Len(t, contacts, 2)

			assert.Equal(t, net.ParseIP("192.168.1.100"), contacts[0].IP)
			assert.Equal(t, 3333, contacts[0].PeerPort)
			assert.Equal(t, protocol.DefaultDHTPort, contacts[0].Port)

			assert.Equal(t, net.ParseIP("10.0.0.1"), contacts[1].IP)
			assert.Equal(t, 4444, contacts[1].PeerPort)
			assert.Equal(t, protocol.DefaultDHTPort, contacts[1].Port)
		})

		t.Run("Valid hostname", func(t *testing.T) {
			addresses := []string{"localhost:3333"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			require.NoError(t, err)
			require.Len(t, contacts, 1)

			// localhost should resolve to either 127.0.0.1 or ::1
			ip := contacts[0].IP
			assert.True(t, ip.Equal(net.ParseIP("127.0.0.1")) || ip.Equal(net.ParseIP("::1")))
			assert.Equal(t, 3333, contacts[0].PeerPort)
			assert.Equal(t, protocol.DefaultDHTPort, contacts[0].Port)
		})

		t.Run("Mixed IP and hostname", func(t *testing.T) {
			addresses := []string{"192.168.1.100:3333", "localhost:4444"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			require.NoError(t, err)
			require.Len(t, contacts, 2)

			assert.Equal(t, net.ParseIP("192.168.1.100"), contacts[0].IP)
			assert.Equal(t, 3333, contacts[0].PeerPort)

			// localhost should resolve to either 127.0.0.1 or ::1
			ip := contacts[1].IP
			assert.True(t, ip.Equal(net.ParseIP("127.0.0.1")) || ip.Equal(net.ParseIP("::1")))
			assert.Equal(t, 4444, contacts[1].PeerPort)
		})

		t.Run("Invalid hostname", func(t *testing.T) {
			addresses := []string{"nonexistent.invalid.hostname.example:3333"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "failed to resolve hostname")
			assert.Nil(t, contacts)
		})
	})

	t.Run("WithMockResolver", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		mockDHT := protocolMocks.NewMockDHTNode(t)
		mockResolver := NewMockHostResolver()
		discovery := NewPeerDiscoveryWithHostResolver(mockDHT, logger, mockResolver)

		t.Run("Mocked hostname resolution", func(t *testing.T) {
			// Setup mock resolution
			expectedIP := net.ParseIP("203.0.113.100")
			mockResolver.SetResolution("test.example.com", expectedIP)

			addresses := []string{"test.example.com:3333"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			require.NoError(t, err)
			require.Len(t, contacts, 1)
			assert.Equal(t, expectedIP, contacts[0].IP)
			assert.Equal(t, 3333, contacts[0].PeerPort)
			assert.Equal(t, protocol.DefaultDHTPort, contacts[0].Port)
			assert.Contains(t, mockResolver.GetCalls(), "test.example.com")
		})

		t.Run("Mocked hostname error", func(t *testing.T) {
			// Setup mock error
			expectedError := &net.DNSError{Err: "custom DNS error", Name: "error.example.com"}
			mockResolver.SetError("error.example.com", expectedError)

			addresses := []string{"error.example.com:3333"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			assert.Error(t, err)
			assert.Equal(t, expectedError, err)
			assert.Nil(t, contacts)
			assert.Contains(t, mockResolver.GetCalls(), "error.example.com")
		})

		t.Run("Multiple mocked resolutions", func(t *testing.T) {
			// Setup multiple mock resolutions
			mockResolver.SetResolution("host1.example.com", net.ParseIP("192.0.2.1"))
			mockResolver.SetResolution("host2.example.com", net.ParseIP("192.0.2.2"))

			addresses := []string{"host1.example.com:3333", "host2.example.com:4444"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			require.NoError(t, err)
			require.Len(t, contacts, 2)

			assert.Equal(t, net.ParseIP("192.0.2.1"), contacts[0].IP)
			assert.Equal(t, 3333, contacts[0].PeerPort)

			assert.Equal(t, net.ParseIP("192.0.2.2"), contacts[1].IP)
			assert.Equal(t, 4444, contacts[1].PeerPort)

			calls := mockResolver.GetCalls()
			assert.Contains(t, calls, "host1.example.com")
			assert.Contains(t, calls, "host2.example.com")
		})
	})

	t.Run("Common edge cases", func(t *testing.T) {
		setup := setupTest(t)
		discovery := setup.discovery

		t.Run("Empty list", func(t *testing.T) {
			contacts, err := discovery.ParseFixedPeers([]string{})
			require.NoError(t, err)
			assert.Nil(t, contacts)
		})

		t.Run("Nil list", func(t *testing.T) {
			contacts, err := discovery.ParseFixedPeers(nil)
			require.NoError(t, err)
			assert.Nil(t, contacts)
		})

		t.Run("Invalid port", func(t *testing.T) {
			addresses := []string{"192.168.1.100:invalid"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid port")
			assert.Nil(t, contacts)
		})

		t.Run("Invalid format", func(t *testing.T) {
			addresses := []string{"not-an-address"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), "invalid peer address format")
			assert.Nil(t, contacts)
		})

		t.Run("Empty address skipped", func(t *testing.T) {
			addresses := []string{"", "192.168.1.100:3333"}
			contacts, err := discovery.ParseFixedPeers(addresses)

			require.NoError(t, err)
			require.Len(t, contacts, 1)
			assert.Equal(t, net.ParseIP("192.168.1.100"), contacts[0].IP)
		})
	})
}

func TestPeerDiscovery_DiscoverPeers(t *testing.T) {
	t.Run("Successful discovery", func(t *testing.T) {
		setup := setupTest(t)
		hashBitmap := bits.Rand()

		expectedContacts := createTestContacts()[:1]
		setup.mockDHT.EXPECT().Get(hashBitmap).Return(expectedContacts, nil)

		contacts, err := setup.discovery.DiscoverPeers(context.Background(), hashBitmap)

		assertSuccessfulOperation(t, err, contacts)
		assert.Equal(t, expectedContacts, contacts)
	})

	t.Run("DHT error", func(t *testing.T) {
		setup := setupTest(t)
		hashBitmap := bits.Rand()

		expectedErr := fmt.Errorf("DHT error")
		setup.mockDHT.EXPECT().Get(hashBitmap).Return(nil, expectedErr)

		contacts, err := setup.discovery.DiscoverPeers(context.Background(), hashBitmap)

		// DHT errors should now return empty contacts to allow fallback to fixed peers
		assert.NoError(t, err)
		assert.Empty(t, contacts)
	})

	t.Run("Stopped discovery", func(t *testing.T) {
		setup := setupTest(t)
		hashBitmap := bits.Rand()

		setup.discovery.Stop()

		contacts, err := setup.discovery.DiscoverPeers(context.Background(), hashBitmap)

		assertStoppedDiscovery(t, setup.discovery, err, contacts)
	})

	t.Run("Context cancelled", func(t *testing.T) {
		setup := setupTest(t)
		hashBitmap := bits.Rand()

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		contacts, err := setup.discovery.DiscoverPeers(ctx, hashBitmap)

		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
		assert.Nil(t, contacts)
	})
}

func TestPeerDiscovery_DiscoverPeersWithRetry(t *testing.T) {
	setup := setupTest(t)
	setup.discovery.SetRetryConfig(2, 10*time.Millisecond)

	hashBitmap := bits.Rand()

	t.Run("Success on first attempt", func(t *testing.T) {
		expectedContacts := createTestContacts()[:1]
		setup.mockDHT.EXPECT().Get(hashBitmap).Return(expectedContacts, nil).Once()

		contacts, err := setup.discovery.DiscoverPeers(context.Background(), hashBitmap)

		assertSuccessfulOperation(t, err, contacts)
		assert.Equal(t, expectedContacts, contacts)
	})

	t.Run("Success on retry", func(t *testing.T) {
		expectedContacts := createTestContacts()[:1]
		// First attempt returns empty, second succeeds
		setup.mockDHT.EXPECT().Get(hashBitmap).Return([]dht.Contact{}, nil).Once()
		setup.mockDHT.EXPECT().Get(hashBitmap).Return(expectedContacts, nil).Once()

		contacts, err := setup.discovery.DiscoverPeers(context.Background(), hashBitmap)

		assertSuccessfulOperation(t, err, contacts)
		assert.Equal(t, expectedContacts, contacts)
	})

	t.Run("No contacts after retries", func(t *testing.T) {
		// All attempts return empty
		setup.mockDHT.EXPECT().Get(hashBitmap).Return([]dht.Contact{}, nil).Times(3) // 1 initial + 2 retries

		contacts, err := setup.discovery.DiscoverPeers(context.Background(), hashBitmap)

		assertSuccessfulOperation(t, err, contacts)
		assert.Empty(t, contacts)
	})
}

func TestPeerDiscovery_calculateBackoffDelay(t *testing.T) {
	baseDelay := 100 * time.Millisecond

	// Test that delay increases exponentially with jitter
	delays := make([]time.Duration, 5)
	for i := 1; i <= 5; i++ {
		delays[i-1] = calculateBackoffDelay(i, baseDelay)
	}

	// First attempt should be around base delay (with jitter)
	assert.GreaterOrEqual(t, delays[0], baseDelay*3/4) // 75% of base
	assert.LessOrEqual(t, delays[0], baseDelay*5/4)    // 125% of base

	// Second attempt should be around 2x base delay (with jitter)
	assert.GreaterOrEqual(t, delays[1], baseDelay*2*3/4) // 75% of 2x base
	assert.LessOrEqual(t, delays[1], baseDelay*2*5/4)    // 125% of 2x base

	// Third attempt should be around 4x base delay (with jitter)
	assert.GreaterOrEqual(t, delays[2], baseDelay*4*3/4) // 75% of 4x base
	assert.LessOrEqual(t, delays[2], baseDelay*4*5/4)    // 125% of 4x base
}

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components for discovery tests
type testSetup struct {
	logger    *zap.Logger
	mockDHT   *protocolMocks.MockDHTNode
	discovery PeerDiscovery
}

// setupTest creates a standard test environment with logger, mock DHT, and discovery
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t)
	mockDHT := protocolMocks.NewMockDHTNode(t)
	discovery := NewPeerDiscovery(mockDHT, logger)

	return &testSetup{
		logger:    logger,
		mockDHT:   mockDHT,
		discovery: discovery,
	}
}

// setupTestWithMockResolver creates a test environment with a mock host resolver
// This allows for controlled hostname resolution testing
func setupTestWithMockResolver(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t)
	mockDHT := protocolMocks.NewMockDHTNode(t)
	mockResolver := NewMockHostResolver()
	discovery := NewPeerDiscoveryWithHostResolver(mockDHT, logger, mockResolver)

	return &testSetup{
		logger:    logger,
		mockDHT:   mockDHT,
		discovery: discovery,
	}
}

// assertStoppedDiscovery checks common stopped discovery behavior
// Consolidates repetitive assertions for stopped discovery test scenarios.
func assertStoppedDiscovery(t *testing.T, discovery PeerDiscovery, err error, result interface{}) {
	assert.True(t, discovery.IsStopped())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "peer discovery is stopped")
	assert.Nil(t, result)
}

// assertSuccessfulOperation checks common successful operation patterns
// Reduces duplication of success assertions across test cases.
func assertSuccessfulOperation(t *testing.T, err error, result interface{}) {
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// createTestContacts creates commonly used test contact structures
// Eliminates duplication of contact creation across multiple test functions.
func createTestContacts() []dht.Contact {
	return []dht.Contact{
		{ID: bits.Bitmap{}, IP: net.ParseIP("192.168.1.100"), Port: 4444, PeerPort: 3333},
		{ID: bits.Bitmap{}, IP: net.ParseIP("10.0.0.1"), Port: 4444, PeerPort: 4444},
	}
}

// createTestContact creates a single test contact with the specified IP and ports
// Provides flexibility for tests that need custom contact configurations.
func createTestContact(ip string, dhtPort, peerPort int) dht.Contact {
	return dht.Contact{
		ID: bits.Rand(), IP: net.ParseIP(ip), Port: dhtPort, PeerPort: peerPort,
	}
}
