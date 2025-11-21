package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/protocol"
	watchdogMocks "go.lumeweb.com/liblbry/protocol/watchdog/mocks"
	"go.uber.org/zap/zaptest"
)

func TestNewDHTBuilder(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)

	require.NoError(t, err)
	require.NotNil(t, builder)
	require.NotNil(t, builder.config)
	require.NotNil(t, builder.options)
	require.Equal(t, logger, builder.logger)

	// Check defaults
	assert.Equal(t, DefaultDHTPort, builder.config.port)
	assert.Empty(t, builder.config.address)
	assert.NotEmpty(t, builder.config.seedNodes) // Should have default seed nodes
	assert.Equal(t, DefaultPeerPort, builder.config.peerProtocolPort)
	assert.Equal(t, 0, builder.config.rpcPort)
	assert.Equal(t, 50*time.Minute, builder.config.reannounceTime)
	assert.Equal(t, 10, builder.config.announceRate)
	assert.True(t, builder.config.validationEnabled)
}

func TestDHTBuilder_WithPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test valid port
	result := builder.WithPort(5555)
	assert.Same(t, builder, result)
	assert.Equal(t, 5555, builder.config.port)

	// Test invalid port (should be ignored with warning)
	result = builder.WithPort(-1)
	assert.Same(t, builder, result)
	assert.Equal(t, 5555, builder.config.port) // Should remain unchanged

	result = builder.WithPort(70000)
	assert.Same(t, builder, result)
	assert.Equal(t, 5555, builder.config.port) // Should remain unchanged
}

func TestDHTBuilder_WithAddress(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test valid address
	result := builder.WithAddress("192.168.1.100:6666")
	assert.Same(t, builder, result)
	assert.Equal(t, "192.168.1.100:6666", builder.config.address)

	// Test empty address (should be ignored)
	result = builder.WithAddress("")
	assert.Same(t, builder, result)
	assert.Equal(t, "192.168.1.100:6666", builder.config.address) // Should remain unchanged

	// Test invalid address (should be ignored with warning)
	result = builder.WithAddress("invalid-address")
	assert.Same(t, builder, result)
	assert.Equal(t, "192.168.1.100:6666", builder.config.address) // Should remain unchanged

	// Test localhost (should be accepted)
	result = builder.WithAddress("localhost:7777")
	assert.Same(t, builder, result)
	assert.Equal(t, "localhost:7777", builder.config.address)

	// Test single-label hostname (should be rejected)
	result = builder.WithAddress("singlehost:8888")
	assert.Same(t, builder, result)
	assert.Equal(t, "localhost:7777", builder.config.address) // Should remain unchanged
}

func TestDHTBuilder_WithSeedNodes(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test valid seed nodes
	nodes := []string{"node1.example.com:4444", "node2.example.com:4444"}
	result := builder.WithSeedNodes(nodes)
	assert.Same(t, builder, result)
	assert.Equal(t, nodes, builder.config.seedNodes)

	// Test nil nodes (should set empty slice)
	result = builder.WithSeedNodes(nil)
	assert.Same(t, builder, result)
	assert.Equal(t, []string{}, builder.config.seedNodes)

	// Test empty nodes (should set empty slice)
	result = builder.WithSeedNodes([]string{})
	assert.Same(t, builder, result)
	assert.Equal(t, []string{}, builder.config.seedNodes)

	// Test nodes with invalid entries (should filter out invalid ones)
	mixedNodes := []string{
		"valid.example.com:4444",
		"",                         // empty - should be filtered
		"invalid-address",          // invalid - should be filtered
		"another.example.com:5555", // valid
	}
	result = builder.WithSeedNodes(mixedNodes)
	assert.Same(t, builder, result)
	expectedNodes := []string{"valid.example.com:4444", "another.example.com:5555"}
	assert.Equal(t, expectedNodes, builder.config.seedNodes)
}

func TestDHTBuilder_WithNodeID(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	nodeID := "1234567890abcdef"
	result := builder.WithNodeID(nodeID)
	assert.Same(t, builder, result)
	assert.Equal(t, nodeID, builder.config.nodeID)
}

func TestDHTBuilder_WithPeerProtocolPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test valid port
	result := builder.WithPeerProtocolPort(4444)
	assert.Same(t, builder, result)
	assert.Equal(t, 4444, builder.config.peerProtocolPort)

	// Test invalid ports (should be ignored with warning)
	result = builder.WithPeerProtocolPort(0)
	assert.Same(t, builder, result)
	assert.Equal(t, 4444, builder.config.peerProtocolPort) // Should remain unchanged

	result = builder.WithPeerProtocolPort(70000)
	assert.Same(t, builder, result)
	assert.Equal(t, 4444, builder.config.peerProtocolPort) // Should remain unchanged
}

func TestDHTBuilder_WithRPCPort(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test valid port
	result := builder.WithRPCPort(5000)
	assert.Same(t, builder, result)
	assert.Equal(t, 5000, builder.config.rpcPort)

	// Test invalid ports (should be ignored with warning)
	result = builder.WithRPCPort(-1)
	assert.Same(t, builder, result)
	assert.Equal(t, 5000, builder.config.rpcPort) // Should remain unchanged

	result = builder.WithRPCPort(70000)
	assert.Same(t, builder, result)
	assert.Equal(t, 5000, builder.config.rpcPort) // Should remain unchanged
}

func TestDHTBuilder_WithReannounceTime(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	interval := 30 * time.Minute
	result := builder.WithReannounceTime(interval)
	assert.Same(t, builder, result)
	assert.Equal(t, interval, builder.config.reannounceTime)

	// Test invalid interval (should be ignored with warning)
	result = builder.WithReannounceTime(0)
	assert.Same(t, builder, result)
	assert.Equal(t, interval, builder.config.reannounceTime) // Should remain unchanged
}

func TestDHTBuilder_WithAnnounceRate(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test valid rate
	result := builder.WithAnnounceRate(20)
	assert.Same(t, builder, result)
	assert.Equal(t, 20, builder.config.announceRate)

	// Test invalid rate (should be ignored with warning)
	result = builder.WithAnnounceRate(0)
	assert.Same(t, builder, result)
	assert.Equal(t, 20, builder.config.announceRate) // Should remain unchanged
}

func TestDHTBuilder_WithOptions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test with valid options
	option1 := protocol.WithDHTNodeID("test123")
	option2 := protocol.WithDHTAnnounceRate(15)

	result := builder.WithOptions(option1, option2)
	assert.Same(t, builder, result)
	assert.Len(t, builder.options, 2)

	// Test with nil options (should be filtered)
	result = builder.WithOptions(option1, nil, option2, nil)
	assert.Same(t, builder, result)
	assert.Len(t, builder.options, 4) // Should only add non-nil options
}

func TestDHTBuilder_WithValidation(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test disabling validation
	result := builder.WithValidation(false)
	assert.Same(t, builder, result)
	assert.False(t, builder.config.validationEnabled)

	// Test enabling validation
	result = builder.WithValidation(true)
	assert.Same(t, builder, result)
	assert.True(t, builder.config.validationEnabled)
}

func TestDHTBuilder_IsConfigured(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Default builder should be configured (has default port and seed nodes)
	assert.True(t, builder.IsConfigured())

	// Create empty builder
	emptyBuilder, err := NewDHTBuilder(logger)
	require.NoError(t, err)
	emptyBuilder.config.port = 0
	emptyBuilder.config.seedNodes = []string{}
	assert.False(t, emptyBuilder.IsConfigured())

	// Add configuration
	emptyBuilder.WithPort(4444)
	assert.True(t, emptyBuilder.IsConfigured())
}

func TestDHTBuilder_Copy(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Configure builder
	builder.WithPort(5555).
		WithAddress("192.168.1.100:6666").
		WithSeedNodes([]string{"node1.example.com:4444"}).
		WithNodeID("test123").
		WithOptions(protocol.WithDHTAnnounceRate(20))

	// Create copy
	copy := builder.Copy()

	// Verify copy is independent
	assert.NotSame(t, builder.config, copy.config)
	assert.NotSame(t, &builder.options, &copy.options)
	assert.Equal(t, builder.config.port, copy.config.port)
	assert.Equal(t, builder.config.address, copy.config.address)
	assert.Equal(t, builder.config.seedNodes, copy.config.seedNodes)
	assert.Equal(t, builder.config.nodeID, copy.config.nodeID)
	assert.Len(t, copy.options, 1)

	// Modify original and verify copy is unchanged
	builder.WithPort(7777)
	assert.Equal(t, 5555, copy.config.port)
}

func TestDHTBuilder_BuildConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Configure builder
	builder.WithPort(5555).
		WithAddress("192.168.1.100:6666").
		WithSeedNodes([]string{"node1.example.com:4444"}).
		WithNodeID("test123").
		WithPeerProtocolPort(7777).
		WithRPCPort(8888).
		WithReannounceTime(30 * time.Minute).
		WithAnnounceRate(20)

	// Build config
	config, err := builder.BuildConfig()
	require.NoError(t, err)
	require.NotNil(t, config)

	// Verify config
	assert.Equal(t, "192.168.1.100:6666", config.Address)
	assert.Equal(t, []string{"node1.example.com:4444"}, config.SeedNodes)
	assert.Equal(t, "test123", config.NodeID)
	assert.Equal(t, 7777, config.PeerProtocolPort)
	assert.Equal(t, 8888, config.RPCPort)
	assert.Equal(t, 30*time.Minute, config.ReannounceTime)
	assert.Equal(t, 20, config.AnnounceRate)
}

func TestDHTBuilder_BuildConfig_ValidationErrors(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		setup   func(*DHTBuilder)
		wantErr string
	}{
		{
			name:    "invalid port",
			setup:   func(b *DHTBuilder) { b.config.port = -1 },
			wantErr: "DHT port must be between 0 and 65535",
		},
		{
			name:    "invalid peer protocol port",
			setup:   func(b *DHTBuilder) { b.config.peerProtocolPort = 0 },
			wantErr: "peer protocol port must be between 1 and 65535",
		},
		{
			name:    "invalid RPC port",
			setup:   func(b *DHTBuilder) { b.config.rpcPort = -1 },
			wantErr: "RPC port must be between 0 and 65535",
		},
		{
			name:    "invalid reannounce time",
			setup:   func(b *DHTBuilder) { b.config.reannounceTime = 0 },
			wantErr: "reannounce time must be positive",
		},
		{
			name:    "invalid announce rate",
			setup:   func(b *DHTBuilder) { b.config.announceRate = 0 },
			wantErr: "announce rate must be positive",
		},
		{
			name:    "invalid address",
			setup:   func(b *DHTBuilder) { b.config.address = "invalid" },
			wantErr: "invalid DHT address",
		},
		{
			name:    "invalid seed node",
			setup:   func(b *DHTBuilder) { b.config.seedNodes = []string{"invalid"} },
			wantErr: "invalid seed node",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder, err := NewDHTBuilder(logger)
			require.NoError(t, err)
			tt.setup(builder)

			config, err := builder.BuildConfig()
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Nil(t, config)
		})
	}
}

func TestDHTBuilder_BuildConfig_ValidationDisabled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Configure with invalid values
	builder.WithPort(-1). // Invalid port
				WithAddress("invalid") // Invalid address

	// Disable validation
	builder.WithValidation(false)

	// Should build without error
	config, err := builder.BuildConfig()
	assert.NoError(t, err)
	assert.NotNil(t, config)
}

func TestDHTBuilder_BuildNode(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Configure builder with minimal settings
	builder.WithPort(5555).
		WithSeedNodes([]string{"127.0.0.1:4444"})

	// Build node - this test just verifies the build process works
	// We don't test actual DHT functionality here as that requires network setup
	node, err := builder.BuildNode()
	require.NoError(t, err)
	require.NotNil(t, node)

	// Start the node to ensure proper initialization
	err = node.Start()
	if err != nil {
		t.Logf("Failed to start DHT node (may be expected in test environment): %v", err)
	}

	// Properly clean up the node
	defer func() {
		if node != nil {
			node.Shutdown()
		}
	}()
}

func TestDHTBuilder_BuildNode_ValidationError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Configure with invalid settings by directly manipulating config
	// since WithPort() has input validation that prevents setting invalid values
	builder.config.port = -1 // Invalid port

	// Should fail validation
	node, err := builder.BuildNode()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DHT configuration validation failed")
	assert.Nil(t, node)
}

func TestDHTBuilder_FluentInterface(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Test method chaining
	result := builder.
		WithPort(5555).
		WithAddress("192.168.1.100:6666").
		WithSeedNodes([]string{"node1.example.com:4444"}).
		WithNodeID("test123").
		WithPeerProtocolPort(7777).
		WithRPCPort(8888).
		WithReannounceTime(30 * time.Minute).
		WithAnnounceRate(20).
		WithValidation(true)

	// Should return same instance throughout chain
	assert.Same(t, builder, result)

	// Verify all settings were applied
	assert.Equal(t, 5555, builder.config.port)
	assert.Equal(t, "192.168.1.100:6666", builder.config.address)
	assert.Equal(t, []string{"node1.example.com:4444"}, builder.config.seedNodes)
	assert.Equal(t, "test123", builder.config.nodeID)
	assert.Equal(t, 7777, builder.config.peerProtocolPort)
	assert.Equal(t, 8888, builder.config.rpcPort)
	assert.Equal(t, 30*time.Minute, builder.config.reannounceTime)
	assert.Equal(t, 20, builder.config.announceRate)
	assert.True(t, builder.config.validationEnabled)
}

func TestValidateAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{
			name:    "valid IP address",
			address: "192.168.1.100:4444",
			wantErr: false,
		},
		{
			name:    "valid localhost",
			address: "localhost:4444",
			wantErr: false,
		},
		{
			name:    "valid FQDN",
			address: "example.com:4444",
			wantErr: false,
		},
		{
			name:    "invalid single-label hostname",
			address: "singlehost:4444",
			wantErr: true,
		},
		{
			name:    "invalid format",
			address: "invalid-address",
			wantErr: true,
		},
		{
			name:    "invalid port",
			address: "192.168.1.100:99999",
			wantErr: true,
		},
		{
			name:    "non-numeric port",
			address: "192.168.1.100:abc",
			wantErr: true,
		},
		{
			name:    "empty address",
			address: "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateAddress(tt.address)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestDHTBuilder_WithWatchdog(t *testing.T) {
	logger := zaptest.NewLogger(t)
	builder, err := NewDHTBuilder(logger)
	require.NoError(t, err)

	// Create mock watchdog using the proper mock from the imported package
	watchdog := watchdogMocks.NewMockDHTWatchdog(t)

	// Set watchdog
	result := builder.WithWatchdog(watchdog)
	assert.Same(t, builder, result)
	assert.Equal(t, watchdog, builder.config.watchdog)
}
