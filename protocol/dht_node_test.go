package protocol

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/lbryio/lbry.go/v2/dht"
	"github.com/lbryio/lbry.go/v2/dht/bits"
	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/liblbry/protocol/mocks"
	"go.uber.org/zap/zaptest"
)

// Test constants for DHT node testing
const (
	testDHTAddress  = "127.0.0.1:4444"
	testPingAddress = "127.0.0.1:5567"
	testContactIP   = "127.0.0.1"
	testContactPort = 5567
)

// Test helper functions
func getTestHash() bits.Bitmap {
	return bits.Bitmap{1, 2, 3}
}

func getTestContact() dht.Contact {
	return dht.Contact{
		ID:   bits.Bitmap{4, 5, 6},
		IP:   net.ParseIP(testContactIP),
		Port: testContactPort,
	}
}

// TestNewDHTNode tests the creation of a new DHT node
func TestNewDHTNode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		dhtImpl     DHT
		options     []DHTOption
		expectError bool
	}{
		{
			name:    "with nil DHT implementation",
			dhtImpl: nil,
			options: []DHTOption{
				WithDHTAddress(testDHTAddress),
			},
			expectError: false,
		},
		{
			name:        "with provided DHT implementation",
			dhtImpl:     mocks.NewMockDHT(t),
			options:     []DHTOption{},
			expectError: false,
		},
		{
			name:    "with multiple options",
			dhtImpl: nil,
			options: []DHTOption{
				WithDHTAddress(testDHTAddress),
				WithDHTLogger(zaptest.NewLogger(t)),
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			node, err := NewDHTNode(tt.dhtImpl, tt.options...)

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, node)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, node)
				assert.IsType(t, &managedDHTNode{}, node)
			}
		})
	}
}

// TestManagedDHTNode_NewDHTNodeWithDefaults tests the creation with defaults
func TestManagedDHTNode_NewDHTNodeWithDefaults(t *testing.T) {
	t.Parallel()

	node, err := NewDHTNodeWithDefaults()
	assert.NoError(t, err)
	assert.NotNil(t, node)
	assert.IsType(t, &managedDHTNode{}, node)
}

// TestManagedDHTNode_StartStop tests the start and stop lifecycle
func TestManagedDHTNode_StartStop(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Start().Return(nil)
	mockDHT.EXPECT().Shutdown()
	mockDHT.EXPECT().WaitUntilJoined()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Test start
	err = node.Start()
	assert.NoError(t, err)
	assert.False(t, node.(*managedDHTNode).stopped)

	// Wait for join to complete
	node.WaitUntilJoined()

	// Test stop
	node.Shutdown()
	assert.True(t, node.(*managedDHTNode).stopped)

	// Wait for all goroutines to complete
	node.Wait()
}

// TestManagedDHTNode_StartError tests start error handling
func TestManagedDHTNode_StartError(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Start().Return(assert.AnError)

	node := &managedDHTNode{
		dht:     mockDHT,
		config:  &DHTConfig{},
		stopped: false,
	}

	err := node.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start DHT node")
}

// TestManagedDHTNode_StartWhenStopped tests start when already stopped
func TestManagedDHTNode_StartWhenStopped(t *testing.T) {
	t.Parallel()

	node := &managedDHTNode{
		dht:     nil,
		config:  &DHTConfig{},
		stopped: true,
	}

	err := node.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT node is stopped")
}

// TestManagedDHTNode_Restart tests the restart functionality
func TestManagedDHTNode_Restart(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	// Expect Start() to be called twice - once for initial start, once for restart
	mockDHT.EXPECT().Start().Return(nil).Twice()
	// Expect Shutdown() to be called once
	mockDHT.EXPECT().Shutdown()
	// Expect WaitUntilJoined() to be called twice (once for each start)
	mockDHT.EXPECT().WaitUntilJoined()

	// Create node using NewDHTNode for proper initialization
	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Initial start
	err = node.Start()
	assert.NoError(t, err)

	// Wait for join to complete
	node.WaitUntilJoined()

	// Stop the node
	node.Shutdown()
	assert.True(t, node.(*managedDHTNode).stopped)

	// Restart
	err = node.Restart()
	assert.NoError(t, err)
	assert.False(t, node.(*managedDHTNode).stopped)

	// Wait for join to complete after restart
	node.WaitUntilJoined()

	// Clean up any remaining goroutines
	node.Wait()
}

// TestManagedDHTNode_RestartError tests restart when not stopped
func TestManagedDHTNode_RestartError(t *testing.T) {
	t.Parallel()

	// Create a properly initialized node using NewDHTNode
	node, err := NewDHTNode(mocks.NewMockDHT(t), WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Ensure the node is not stopped (default state after creation)
	assert.False(t, node.(*managedDHTNode).stopped)

	err = node.Restart()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT node must be stopped before restarting")
}

// TestManagedDHTNode_WaitUntilJoined tests waiting for join
func TestManagedDHTNode_WaitUntilJoined(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Start().Return(nil)
	mockDHT.EXPECT().WaitUntilJoined()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Start the node before waiting for join
	err = node.Start()
	assert.NoError(t, err)

	node.WaitUntilJoined()
	assert.True(t, node.(*managedDHTNode).joined)
}

// TestManagedDHTNode_WaitUntilJoinedWhenStopped tests waiting when stopped
func TestManagedDHTNode_WaitUntilJoinedWhenStopped(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Stop the node first
	node.Shutdown()

	joinedBefore := node.(*managedDHTNode).joined
	node.WaitUntilJoined()
	assert.Equal(t, joinedBefore, node.(*managedDHTNode).joined) // Should not change when stopped
}

// TestManagedDHTNode_ID tests the ID method
func TestManagedDHTNode_ID(t *testing.T) {
	t.Parallel()

	expectedID := getTestHash()
	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().ID().Return(expectedID)

	node := &managedDHTNode{
		dht:     mockDHT,
		config:  &DHTConfig{},
		stopped: false,
	}

	id := node.ID()
	assert.Equal(t, expectedID, id)
}

// TestManagedDHTNode_IDWhenStopped tests ID when stopped
func TestManagedDHTNode_IDWhenStopped(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Stop the node
	node.Shutdown()

	id := node.ID()
	assert.Equal(t, bits.Bitmap{}, id)
}

// TestManagedDHTNode_Address tests the Address method
func TestManagedDHTNode_Address(t *testing.T) {
	t.Parallel()

	expectedAddr := testDHTAddress
	node := &managedDHTNode{
		dht:     nil,
		config:  &DHTConfig{Address: expectedAddr},
		stopped: false,
	}

	addr := node.Address()
	assert.Equal(t, expectedAddr, addr)
}

// TestManagedDHTNode_Ping tests the Ping method
func TestManagedDHTNode_Ping(t *testing.T) {
	t.Parallel()

	testAddr := testPingAddress
	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Ping(testAddr).Return(nil)

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	err = node.Ping(testAddr)
	assert.NoError(t, err)
}

// TestManagedDHTNode_PingError tests Ping error handling
func TestManagedDHTNode_PingError(t *testing.T) {
	t.Parallel()

	testAddr := testPingAddress
	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Ping(testAddr).Return(assert.AnError)

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	err = node.Ping(testAddr)
	assert.Error(t, err)
}

// TestManagedDHTNode_PingWhenStopped tests Ping when stopped
func TestManagedDHTNode_PingWhenStopped(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Stop the node
	node.Shutdown()

	err = node.Ping(testPingAddress)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT peer is stopped")
}

// TestManagedDHTNode_Get tests the Get method
func TestManagedDHTNode_Get(t *testing.T) {
	t.Parallel()

	testHash := getTestHash()
	expectedContacts := []dht.Contact{
		getTestContact(),
	}

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Get(testHash).Return(expectedContacts, nil)

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	contacts, err := node.Get(testHash)
	assert.NoError(t, err)
	assert.Equal(t, expectedContacts, contacts)
}

// TestManagedDHTNode_GetWhenStopped tests Get when stopped
func TestManagedDHTNode_GetWhenStopped(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Stop the node
	node.Shutdown()

	contacts, err := node.Get(getTestHash())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT peer is stopped")
	assert.Nil(t, contacts)
}

// TestManagedDHTNode_Add tests the Add method
func TestManagedDHTNode_Add(t *testing.T) {
	t.Parallel()

	testHash := getTestHash()
	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Add(testHash)

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	node.Add(testHash)
}

// TestManagedDHTNode_AddWhenStopped tests Add when stopped
func TestManagedDHTNode_AddWhenStopped(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Stop the node
	node.Shutdown()

	// Should not panic or call the underlying DHT
	node.Add(getTestHash())
}

// TestManagedDHTNode_Remove tests the Remove method
func TestManagedDHTNode_Remove(t *testing.T) {
	t.Parallel()

	testHash := getTestHash()
	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Remove(testHash)

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	node.Remove(testHash)
}

// TestManagedDHTNode_RemoveWhenStopped tests Remove when stopped
func TestManagedDHTNode_RemoveWhenStopped(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Stop the node
	node.Shutdown()

	// Should not panic or call the underlying DHT
	node.Remove(getTestHash())
}

// TestManagedDHTNode_IsJoined tests the IsJoined method
func TestManagedDHTNode_IsJoined(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		joined   bool
		expected bool
	}{
		{
			name:     "joined",
			joined:   true,
			expected: true,
		},
		{
			name:     "not joined",
			joined:   false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			node := &managedDHTNode{
				dht:     nil,
				config:  &DHTConfig{},
				stopped: false,
				joined:  tt.joined,
			}

			joined := node.IsJoined()
			assert.Equal(t, tt.expected, joined)
		})
	}
}

// TestManagedDHTNode_GetRoutingTableInfo tests GetRoutingTableInfo
func TestManagedDHTNode_GetRoutingTableInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stopped  bool
		joined   bool
		address  string
		contains string
	}{
		{
			name:     "active node",
			stopped:  false,
			joined:   true,
			address:  "127.0.0.1:4444",
			contains: "Joined: true",
		},
		{
			name:     "stopped node",
			stopped:  true,
			joined:   false,
			address:  "127.0.0.1:4444",
			contains: "DHT Node stopped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockDHT := mocks.NewMockDHT(t)
			if !tt.stopped {
				mockDHT.EXPECT().ID().Return(getTestHash())
			}

			node, err := NewDHTNode(mockDHT, WithDHTAddress(tt.address))
			assert.NoError(t, err)
			assert.NotNil(t, node)

			managedNode := node.(*managedDHTNode)
			managedNode.stopped = tt.stopped
			managedNode.joined = tt.joined

			info := node.GetRoutingTableInfo()
			assert.Contains(t, info, tt.contains)
			assert.Contains(t, info, tt.address)
		})
	}
}

// TestManagedDHTNode_Wait tests the Wait method
func TestManagedDHTNode_Wait(t *testing.T) {
	t.Parallel()

	node := &managedDHTNode{
		dht:     nil,
		config:  &DHTConfig{},
		stopped: false,
		wg:      sync.WaitGroup{},
	}

	// Add a task to the WaitGroup to ensure Wait() has something to wait for
	node.wg.Add(1)
	go func() {
		defer node.wg.Done()
		time.Sleep(10 * time.Millisecond) // Brief work
	}()

	// Test that Wait completes without hanging
	done := make(chan bool)
	go func() {
		node.Wait()
		done <- true
	}()

	select {
	case <-done:
		// Test completed successfully
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Wait method hung")
	}
}

// TestManagedDHTNode_Concurrency tests concurrent access
func TestManagedDHTNode_Concurrency(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	mockDHT.EXPECT().ID().Return(getTestHash()).Times(10)
	mockDHT.EXPECT().Add(getTestHash()).Times(10)
	mockDHT.EXPECT().Remove(getTestHash()).Times(10)

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Test concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node.ID()
			node.Address()
			node.IsJoined()
		}()
	}

	// Test concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			node.Add(getTestHash())
			node.Remove(getTestHash())
		}()
	}

	wg.Wait()

	// Ensure any internal goroutines are also cleaned up
	node.Wait()
}

// TestManagedDHTNode_ParseHashFromString tests the ParseHashFromString utility function
func TestManagedDHTNode_ParseHashFromString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		input         string
		expectError   bool
		errorContains string
	}{
		{
			name:        "valid hex string",
			input:       "1234567890abcdef",
			expectError: false,
		},
		{
			name:        "valid hex with 0x prefix",
			input:       "0x1234567890abcdef",
			expectError: false,
		},
		{
			name:        "valid hex with 0X prefix",
			input:       "0X1234567890abcdef",
			expectError: false,
		},
		{
			name:          "empty string",
			input:         "",
			expectError:   true,
			errorContains: "empty string",
		},
		{
			name:          "invalid hex",
			input:         "xyz",
			expectError:   true,
			errorContains: "invalid hash format",
		},
		{
			name:        "short hex",
			input:       "123",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hash, err := ParseHashFromString(tt.input)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Equal(t, bits.Bitmap{}, hash)
			} else {
				assert.NoError(t, err)
				assert.NotEqual(t, bits.Bitmap{}, hash)
			}
		})
	}
}

// TestManagedDHTNode_HashToString tests the HashToString utility function
func TestManagedDHTNode_HashToString(t *testing.T) {
	t.Parallel()

	testHash := bits.Bitmap{1, 2, 3}
	expected := testHash.Hex()

	result := HashToString(testHash)
	assert.Equal(t, expected, result)
}

// TestManagedDHTNode_HashToStringEmpty tests HashToString with empty bitmap
func TestManagedDHTNode_HashToStringEmpty(t *testing.T) {
	t.Parallel()

	testHash := bits.Bitmap{}
	expected := testHash.Hex() // Use the actual Hex() method result

	result := HashToString(testHash)
	assert.Equal(t, expected, result)
}

// TestManagedDHTNode_isActive tests the isActive method
func TestManagedDHTNode_isActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		stopped  bool
		dht      DHT
		expected bool
	}{
		{
			name:     "active with DHT",
			stopped:  false,
			dht:      mocks.NewMockDHT(t),
			expected: true,
		},
		{
			name:     "stopped with DHT",
			stopped:  true,
			dht:      mocks.NewMockDHT(t),
			expected: false,
		},
		{
			name:     "active without DHT",
			stopped:  false,
			dht:      nil,
			expected: false,
		},
		{
			name:     "stopped without DHT",
			stopped:  true,
			dht:      nil,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// For the "active without DHT" case, we need to ensure the node has a nil dht field
			// We'll create a managedDHTNode directly to control the dht field
			managedNode := &managedDHTNode{
				dht:     tt.dht,
				config:  &DHTConfig{Address: testDHTAddress},
				stopped: tt.stopped,
				joined:  false,
				wg:      sync.WaitGroup{},
			}

			active := managedNode.isActive()
			assert.Equal(t, tt.expected, active)
		})
	}
}

// TestManagedDHTNode_startJoinGoroutine tests the startJoinGoroutine method
func TestManagedDHTNode_startJoinGoroutine(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	// Start will be called when node.Start() is called
	mockDHT.EXPECT().Start().Return(nil)
	mockDHT.EXPECT().WaitUntilJoined()
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Start the node which will internally call startJoinGoroutine
	err = node.Start()
	assert.NoError(t, err)

	// Wait for the join to complete
	node.WaitUntilJoined()

	// The node should now be joined
	assert.True(t, node.IsJoined())

	// Clean up
	node.Shutdown()
}

// TestManagedDHTNode_startJoinGoroutineWithCancel tests startJoinGoroutine with context cancellation
func TestManagedDHTNode_startJoinGoroutineWithCancel(t *testing.T) {
	t.Parallel()

	mockDHT := mocks.NewMockDHT(t)
	// Start will be called when node.Start() is called
	mockDHT.EXPECT().Start().Return(nil)
	// WaitUntilJoined will be called once in startJoinGoroutine but may be cancelled
	mockDHT.EXPECT().WaitUntilJoined()
	mockDHT.EXPECT().Shutdown()

	node, err := NewDHTNode(mockDHT, WithDHTAddress(testDHTAddress))
	assert.NoError(t, err)
	assert.NotNil(t, node)

	// Start the node first
	err = node.Start()
	assert.NoError(t, err)

	// Immediately shutdown to test cancellation
	node.Shutdown()

	// The node should not be joined since we cancelled quickly
	assert.False(t, node.IsJoined())

	// Wait should not hang
	node.Wait()
}
