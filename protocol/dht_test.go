package protocol

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol/mocks"
)

func TestNewDHTConfig(t *testing.T) {
	cfg, err := NewDHTConfig()
	require.NoError(t, err, "Failed to create DHT config")

	if cfg.Address != "127.0.0.1:4444" {
		t.Errorf("Expected default address '127.0.0.1:4444', got '%s'", cfg.Address)
	}

	if len(cfg.SeedNodes) != 4 {
		t.Errorf("Expected 4 seed nodes, got %d", len(cfg.SeedNodes))
	}

	if cfg.PeerProtocolPort != 3333 {
		t.Errorf("Expected default peer port 3333, got %d", cfg.PeerProtocolPort)
	}
}

func TestDHTOptions(t *testing.T) {
	cfg, err := NewDHTConfig()
	require.NoError(t, err, "Failed to create DHT config")

	// Test WithDHTAddress
	WithDHTAddress("127.0.0.1:5555")(cfg)
	if cfg.Address != "127.0.0.1:5555" {
		t.Errorf("Expected address '127.0.0.1:5555', got '%s'", cfg.Address)
	}

	// Test WithDHTNodeID
	WithDHTNodeID("1234567890abcdef")(cfg)
	if cfg.NodeID != "1234567890abcdef" {
		t.Errorf("Expected node ID '1234567890abcdef', got '%s'", cfg.NodeID)
	}

	// Test WithDHTPeerProtocolPort
	WithDHTPeerProtocolPort(4444)(cfg)
	if cfg.PeerProtocolPort != 4444 {
		t.Errorf("Expected peer port 4444, got %d", cfg.PeerProtocolPort)
	}

	// Test WithDHTAnnounceRate
	WithDHTAnnounceRate(20)(cfg)
	if cfg.AnnounceRate != 20 {
		t.Errorf("Expected announce rate 20, got %d", cfg.AnnounceRate)
	}

	// Test WithDHTReannounceTime
	WithDHTReannounceTime(30 * time.Minute)(cfg)
	if cfg.ReannounceTime != 30*time.Minute {
		t.Errorf("Expected reannounce time 30m, got %v", cfg.ReannounceTime)
	}
}

func TestNewDHTNodeWithMock(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Expect ID() to be called once by GetRoutingTableInfo
	mockDHT.EXPECT().ID().Return(bits.Rand()).Once()

	// Test creating DHT node with mock
	node, err := NewDHTNode(mockDHT)
	require.NoError(t, err, "Failed to create DHT node")

	if node == nil {
		t.Fatal("Expected non-nil node")
	}

	// Test that the node implements the interface
	var _ DHTNode = node

	// Test initial state
	if node.IsJoined() {
		t.Error("Expected node to not be joined initially")
	}

	// Test ID
	id := node.ID()
	if id.String() == "" {
		t.Error("Expected non-empty ID")
	}

	// Test address
	addr := node.Address()
	if addr == "" {
		t.Error("Expected non-empty address")
	}
}

func TestNewDHTNodeWithOptions(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	options := []DHTOption{
		WithDHTAddress("127.0.0.1:6666"),
		WithDHTNodeID("abcdef1234567890"),
		WithDHTPeerProtocolPort(7777),
		WithDHTAnnounceRate(15),
		WithDHTReannounceTime(25 * time.Minute),
	}

	node, err := NewDHTNode(mockDHT, options...)
	require.NoError(t, err, "Failed to create DHT node with options")

	if node.Address() != "127.0.0.1:6666" {
		t.Errorf("Expected address '127.0.0.1:6666', got '%s'", node.Address())
	}
}

func TestNewDHTNodeWithDefaults(t *testing.T) {
	// Test the backward compatibility function
	node, err := NewDHTNodeWithDefaults(
		WithDHTAddress("127.0.0.1:8888"),
	)
	require.NoError(t, err, "Failed to create DHT node with defaults")

	if node.Address() != "127.0.0.1:8888" {
		t.Errorf("Expected address '127.0.0.1:8888', got '%s'", node.Address())
	}
}

func TestParseHashFromString(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"valid hex without prefix", "1234567890abcdef1234567890abcdef12345678", false},
		{"valid hex with 0x prefix", "0x1234567890abcdef1234567890abcdef12345678", false},
		{"valid hex with 0X prefix", "0X1234567890abcdef1234567890abcdef12345678", false},
		{"invalid hex", "xyz", true},
		{"empty string", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHashFromString(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseHashFromString() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestHashToString(t *testing.T) {
	// Create a test hash
	hash, err := ParseHashFromString("1234567890abcdef1234567890abcdef12345678")
	require.NoError(t, err, "Failed to parse test hash")

	str := HashToString(hash)
	if str == "" {
		t.Error("Expected non-empty hash string")
	}

	// Should be a valid hex string
	if len(str) < 2 {
		t.Error("Hash string too short")
	}
}

func TestDHTNodeLifecycle(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	testID := bits.Rand()
	mockDHT.EXPECT().ID().Return(testID).Maybe() // Called by GetRoutingTableInfo
	mockDHT.EXPECT().Shutdown().Once()
	mockDHT.EXPECT().WaitUntilJoined().Maybe() // Called in goroutine, may not complete

	node, err := NewDHTNode(mockDHT,
		WithDHTAddress("127.0.0.1:14444"),
		WithDHTSeedNodes([]string{}),
	)
	require.NoError(t, err, "Failed to create DHT node")

	// Test initial state
	if node.IsJoined() {
		t.Error("Expected node to not be joined initially")
	}

	// Test that ID works before shutdown
	id := node.ID()
	if id.String() == "" {
		t.Error("Expected non-empty ID before shutdown")
	}

	// Test shutdown (should not panic)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Shutdown caused panic: %v", r)
		}
	}()

	node.Shutdown()

	// Verify all goroutines completed
	node.(*managedDHTNode).Wait()

	// Test that after shutdown, IsJoined returns false
	if node.IsJoined() {
		t.Error("Expected node to not be joined after shutdown")
	}

	// Test that GetRoutingTableInfo works after shutdown
	infoAfter := node.GetRoutingTableInfo()
	if infoAfter == "" {
		t.Error("Expected non-empty routing table info after shutdown")
	}
}

func TestDHTNodeGetRoutingTableInfo(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	testID := bits.Rand()
	mockDHT.EXPECT().ID().Return(testID)

	node, err := NewDHTNode(mockDHT)
	require.NoError(t, err, "Failed to create DHT node")

	info := node.GetRoutingTableInfo()
	if info == "" {
		t.Error("Expected non-empty routing table info")
	}

	// Should contain node ID and address
	id := node.ID()
	addr := node.Address()

	if !contains(info, id.HexShort()) {
		t.Errorf("Expected routing table info to contain node ID %s", id.HexShort())
	}

	if !contains(info, addr) {
		t.Errorf("Expected routing table info to contain address %s", addr)
	}
}

func TestDHTNodeStart(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	mockDHT.EXPECT().Start().Return(nil)
	mockDHT.EXPECT().WaitUntilJoined().Maybe() // Called in goroutine, may be called multiple times
	mockDHT.EXPECT().Shutdown().Maybe()        // Called in cleanup

	node, err := NewDHTNode(mockDHT)
	require.NoError(t, err, "Failed to create DHT node")

	// Test start
	err = node.Start()
	require.NoError(t, err, "Expected no error on start")

	// Wait for join to complete
	node.WaitUntilJoined()
	if !node.IsJoined() {
		t.Error("Expected node to be joined")
	}

	// Cleanup
	node.Shutdown()
	node.(*managedDHTNode).Wait()
}

func TestDHTNodeStartError(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	mockDHT.EXPECT().Start().Return(testError)
	mockDHT.EXPECT().Shutdown().Maybe() // Will be called in cleanup

	node, err := NewDHTNode(mockDHT)
	require.NoError(t, err, "Failed to create DHT node")

	// Test start error
	err = node.Start()
	if err == nil {
		t.Error("Expected error on start, got nil")
	}

	// Ensure goroutines are cleaned up
	node.Shutdown()
	node.(*managedDHTNode).Wait()
}

func TestDHTNodePing(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	mockDHT.EXPECT().Ping("127.0.0.1:4444").Return(nil)

	node, err := NewDHTNode(mockDHT)
	require.NoError(t, err, "Failed to create DHT node")

	// Test ping
	err = node.Ping("127.0.0.1:4444")
	if err != nil {
		t.Errorf("Expected no error on ping, got %v", err)
	}
}

func TestDHTNodeGet(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	testHash := bits.Rand()
	testContacts := []dht.Contact{
		{
			ID:       bits.Rand(),
			IP:       net.ParseIP("192.168.1.1"),
			Port:     4444,
			PeerPort: 3333,
		},
	}

	mockDHT.EXPECT().Get(testHash).Return(testContacts, nil)

	node, err := NewDHTNode(mockDHT)
	require.NoError(t, err, "Failed to create DHT node")

	// Test get
	contacts, err := node.Get(testHash)
	if err != nil {
		t.Errorf("Expected no error on get, got %v", err)
	}

	if len(contacts) != 1 {
		t.Errorf("Expected 1 contact, got %d", len(contacts))
	}

	if contacts[0].ID != testContacts[0].ID {
		t.Error("Contact ID mismatch")
	}
}

func TestDHTNodeAddRemove(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	testHash := bits.Rand()

	mockDHT.EXPECT().Add(testHash)
	mockDHT.EXPECT().Remove(testHash)

	node, err := NewDHTNode(mockDHT)
	if err != nil {
		t.Fatalf("Failed to create DHT node: %v", err)
	}

	// Test add and remove
	node.Add(testHash)
	node.Remove(testHash)
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Test error for testing error cases
var testError = &testErrorType{}

type testErrorType struct{}

func (e *testErrorType) Error() string {
	return "test error"
}

func TestDHTNodeRestart(t *testing.T) {
	// Create a mock DHT and set up ALL expectations upfront
	mockDHT := mocks.NewMockDHT(t)
	testID := bits.Rand()

	// Initial expectations during node creation and first GetRoutingTableInfo
	mockDHT.EXPECT().ID().Return(testID).Maybe() // Called during creation and GetRoutingTableInfo

	// Expectations for initial start sequence
	mockDHT.EXPECT().Start().Return(nil).Once()
	mockDHT.EXPECT().WaitUntilJoined().Maybe()
	mockDHT.EXPECT().Shutdown().Once() // First shutdown

	// Expectations for restart sequence
	mockDHT.EXPECT().Start().Return(nil).Once() // Restart start
	mockDHT.EXPECT().WaitUntilJoined().Maybe()
	mockDHT.EXPECT().Start().Return(nil).Once() // Post-restart start
	mockDHT.EXPECT().WaitUntilJoined().Maybe()

	// Final cleanup expectations
	mockDHT.EXPECT().Shutdown().Once() // Final shutdown

	// Create node with mock
	node, err := NewDHTNode(mockDHT)
	if err != nil {
		t.Fatalf("Failed to create DHT node: %v", err)
	}

	// 1. Initial start
	err = node.Start()
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Wait for join to complete
	node.WaitUntilJoined()
	if !node.IsJoined() {
		t.Error("Expected node to be joined after start")
	}

	// 2. Shutdown the node
	node.Shutdown()
	node.Wait()

	// 3. Verify stopped state
	if node.IsJoined() {
		t.Error("Expected node to not be joined after shutdown")
	}

	// 4. Restart the node
	err = node.Restart()
	if err != nil {
		t.Fatalf("Failed to restart node: %v", err)
	}

	// 5. Verify node can be started again
	err = node.Start()
	if err != nil {
		t.Errorf("Expected no error starting after restart, got %v", err)
	}

	// Wait for join to complete
	node.WaitUntilJoined()
	if !node.IsJoined() {
		t.Error("Expected node to be joined after restart")
	}

	// 6. Test error case - restart while running
	err = node.Restart()
	if err == nil {
		t.Error("Expected error when restarting running node")
	}

	// Cleanup
	node.Shutdown()
	node.Wait()
}

func TestDHTNodeGoroutineCleanup(t *testing.T) {
	// Create a mock DHT
	mockDHT := mocks.NewMockDHT(t)

	// Set up mock expectations
	mockDHT.EXPECT().Start().Return(nil).Once()
	mockDHT.EXPECT().WaitUntilJoined().Maybe() // Called in goroutine, may not complete due to shutdown
	mockDHT.EXPECT().Shutdown().Once()

	node, err := NewDHTNode(mockDHT)
	if err != nil {
		t.Fatalf("Failed to create DHT node: %v", err)
	}

	// Start the node
	err = node.Start()
	if err != nil {
		t.Fatalf("Failed to start node: %v", err)
	}

	// Shutdown and wait for cleanup
	node.Shutdown()
	node.(*managedDHTNode).Wait()

	// Verify state
	if node.IsJoined() {
		t.Error("Expected node to not be joined after shutdown")
	}
}
