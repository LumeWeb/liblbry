package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/mocks"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/storage/memory"
	storageMocks "go.lumeweb.com/liblbry/storage/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Test constants for protocol ports
// These values are chosen to avoid conflicts with standard ports
// and to clearly distinguish between different protocol types
const (
	testPortPeer       = 8080
	testPortReflector  = 9090
	testPortDHT        = 4444
	testPortPeer2      = 5567
	testPortReflector2 = 5566
	testPortDHT2       = 5555
)

// builderTestMocks holds all common mocks used in builder tests
type builderTestMocks struct {
	storage       *storageMocks.MockBlobStore
	acquirer      *mocks.MockBlobAcquirer
	accessControl *storageMocks.MockAccessControl
	logger        *zap.Logger
}

// setupBuilderMocks initializes all standard mocks for builder testing
func setupBuilderMocks(t *testing.T) *builderTestMocks {
	return &builderTestMocks{
		storage:       storageMocks.NewMockBlobStore(t),
		acquirer:      mocks.NewMockBlobAcquirer(t),
		accessControl: storageMocks.NewMockAccessControl(t),
		logger:        zaptest.NewLogger(t),
	}
}

// setupBuilderWithMocks creates a builder with all mocks configured
func setupBuilderWithMocks(t *testing.T) (*ServerBuilder, *builderTestMocks) {
	testMocks := setupBuilderMocks(t)
	builder := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithAcquirer(testMocks.acquirer).
		WithAccessControl(testMocks.accessControl).
		WithLogger(testMocks.logger)
	return builder, testMocks
}

// assertBuilderReturnsSame verifies that a builder method returns the same instance
// This ensures method chaining works correctly and maintains immutability
func assertBuilderReturnsSame(t *testing.T, originalBuilder *ServerBuilder, resultBuilder *ServerBuilder) {
	assert.Same(t, originalBuilder, resultBuilder, "Builder method should return the same instance")
}

// assertProtocolConfig validates that a protocol is correctly configured with the expected port
// This helper function ensures that protocol configurations are properly set during testing
func assertProtocolConfig(t *testing.T, builder *ServerBuilder, protocolName string, expectedPort int) {
	assert.Contains(t, builder.config, protocolName, "Protocol should be configured")

	switch protocolName {
	case ProtocolPeer:
		config := builder.config[protocolName].(*PeerConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolReflector:
		config := builder.config[protocolName].(*ReflectorConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolDHT:
		config := builder.config[protocolName].(*DHTConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	}
}

// assertServerProtocolConfig validates that a built server has the correct protocol configuration
// This ensures that the server construction process properly applies protocol settings
func assertServerProtocolConfig(t *testing.T, server *DefaultServer, protocolName string, expectedPort int) {
	assert.Contains(t, server.config, protocolName, "Protocol should be configured")

	switch protocolName {
	case ProtocolPeer:
		config := server.config[protocolName].(*PeerConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolReflector:
		config := server.config[protocolName].(*ReflectorConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolDHT:
		config := server.config[protocolName].(*DHTConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	}
}

// assertBuildError verifies that server building fails with the expected error message
// This helper ensures that validation logic in the builder properly rejects invalid configurations
func assertBuildError(t *testing.T, builder *ServerBuilder, expectedError string) {
	server, err := builder.Build()
	assert.Error(t, err, "Build should return an error")
	assert.Nil(t, server, "Server should be nil when build fails")
	assert.Contains(t, err.Error(), expectedError, "Error message should contain expected text")
}

// TestNewServerBuilder verifies that a new server builder is properly initialized
// This ensures the builder starts with clean state and default logger
func TestNewServerBuilder(t *testing.T) {
	builder := NewServerBuilder()

	assert.NotNil(t, builder)
	assert.Empty(t, builder.config)
	assert.NotNil(t, builder.logger)
}

// TestServerBuilder_WithStorage verifies that storage can be properly set on the builder
// This ensures the builder correctly stores and references the provided storage component
func TestServerBuilder_WithStorage(t *testing.T) {
	builder := NewServerBuilder()
	testMocks := setupBuilderMocks(t)

	result := builder.WithStorage(testMocks.storage)

	assertBuilderReturnsSame(t, builder, result)
	assert.Same(t, testMocks.storage, builder.storage)
}

// TestServerBuilder_WithAcquirer verifies that blob acquirer can be properly set on the builder
// This ensures the builder correctly stores and references the provided acquirer component
func TestServerBuilder_WithAcquirer(t *testing.T) {
	builder := NewServerBuilder()
	testMocks := setupBuilderMocks(t)

	result := builder.WithAcquirer(testMocks.acquirer)

	assertBuilderReturnsSame(t, builder, result)
	assert.Same(t, testMocks.acquirer, builder.acquirer)
}

// TestServerBuilder_WithAccessControl verifies that access control can be properly set on the builder
// This ensures the builder correctly stores and references the provided access control component
func TestServerBuilder_WithAccessControl(t *testing.T) {
	builder := NewServerBuilder()
	testMocks := setupBuilderMocks(t)

	result := builder.WithAccessControl(testMocks.accessControl)

	assertBuilderReturnsSame(t, builder, result)
	assert.Same(t, testMocks.accessControl, builder.accessControl)
}

// TestServerBuilder_WithLogger verifies that a custom logger can be properly set on the builder
// This ensures the builder correctly stores and references the provided logger component
func TestServerBuilder_WithLogger(t *testing.T) {
	builder := NewServerBuilder()
	testMocks := setupBuilderMocks(t)

	result := builder.WithLogger(testMocks.logger)

	assertBuilderReturnsSame(t, builder, result)
	assert.Same(t, testMocks.logger, builder.logger)
}

// TestServerBuilder_WithPeer verifies peer protocol configuration with default and custom ports
// This test ensures that peer config can be correctly configured with either default or custom ports
func TestServerBuilder_WithPeer(t *testing.T) {
	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{"DefaultPort", 0, DefaultPeerPort},
		{"CustomPort", testPortPeer, testPortPeer},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServerBuilder()

			var result *ServerBuilder
			if tt.port == 0 {
				result = builder.WithPeer()
			} else {
				result = builder.WithPeer(tt.port)
			}

			assertBuilderReturnsSame(t, builder, result)
			assertProtocolConfig(t, builder, ProtocolPeer, tt.expectedPort)
		})
	}
}

// TestServerBuilder_WithReflector verifies reflector protocol configuration with default and custom ports
// This test ensures that reflector config can be correctly configured with either default or custom ports
func TestServerBuilder_WithReflector(t *testing.T) {
	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{"DefaultPort", 0, DefaultReflectorPort},
		{"CustomPort", testPortReflector, testPortReflector},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServerBuilder()

			var result *ServerBuilder
			if tt.port == 0 {
				result = builder.WithReflector()
			} else {
				result = builder.WithReflector(tt.port)
			}

			assertBuilderReturnsSame(t, builder, result)
			assertProtocolConfig(t, builder, ProtocolReflector, tt.expectedPort)
		})
	}
}

// TestServerBuilder_WithDHT verifies DHT protocol configuration with default and custom ports
// This test ensures that DHT config can be correctly configured with either default or custom ports
func TestServerBuilder_WithDHT(t *testing.T) {
	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{"DefaultPort", 0, DefaultDHTPort},
		{"CustomPort", testPortDHT, testPortDHT},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServerBuilder()

			var result *ServerBuilder
			if tt.port == 0 {
				result = builder.WithDHT()
			} else {
				result = builder.WithDHT(tt.port)
			}

			assertBuilderReturnsSame(t, builder, result)
			assertProtocolConfig(t, builder, ProtocolDHT, tt.expectedPort)
		})
	}
}

// TestServerBuilder_Build_Success verifies successful server building with valid configuration
// This test ensures that a server can be properly constructed when all required components are provided
func TestServerBuilder_Build_Success(t *testing.T) {
	builder, testMocks := setupBuilderWithMocks(t)
	builder.WithPeer(testPortPeer2)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify the server is of the correct type
	defaultServer, ok := server.(*DefaultServer)
	require.True(t, ok)
	assert.Same(t, testMocks.storage, defaultServer.storage)
	assert.Same(t, testMocks.acquirer, defaultServer.acquirer)
	assert.Same(t, testMocks.accessControl, defaultServer.accessControl)
	assert.Equal(t, "server", defaultServer.logger.Name())
	assert.Contains(t, defaultServer.config, ProtocolPeer)
}

// TestServerBuilder_Build_MultipleProtocols verifies server building with multiple config
// This test ensures that servers can be constructed with multiple config simultaneously
func TestServerBuilder_Build_MultipleProtocols(t *testing.T) {
	builder, _ := setupBuilderWithMocks(t)
	builder.
		WithPeer(testPortPeer2).
		WithReflector(testPortReflector2).
		WithDHT(testPortDHT2)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	defaultServer := server.(*DefaultServer)
	assert.Contains(t, defaultServer.config, ProtocolPeer)
	assert.Contains(t, defaultServer.config, ProtocolReflector)
	assert.Contains(t, defaultServer.config, ProtocolDHT)
}

// TestServerBuilder_Build_Errors verifies that server building properly handles various error conditions
// This test ensures that the builder correctly validates its configuration and provides helpful error messages
func TestServerBuilder_Build_Errors(t *testing.T) {
	tests := []struct {
		name          string
		setupBuilder  func(t *testing.T) *ServerBuilder
		expectedError string
	}{
		{
			name: "NoStorage",
			setupBuilder: func(t *testing.T) *ServerBuilder {
				builder := NewServerBuilder()
				builder.WithPeer(testPortPeer2)
				return builder
			},
			expectedError: "storage is required",
		},
		{
			name: "NoProtocols",
			setupBuilder: func(t *testing.T) *ServerBuilder {
				mocks := setupBuilderMocks(t)
				builder := NewServerBuilder()
				builder.WithStorage(mocks.storage)
				return builder
			},
			expectedError: "at least one protocol must be configured",
		},
		{
			name: "NoStorageAndNoProtocols",
			setupBuilder: func(t *testing.T) *ServerBuilder {
				return NewServerBuilder()
			},
			expectedError: "storage is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := tt.setupBuilder(t)
			assertBuildError(t, builder, tt.expectedError)
		})
	}
}

// TestServerBuilder_ChainedMethods verifies that builder method chaining works correctly
// This test ensures that all builder methods can be properly chained together for fluent API usage
func TestServerBuilder_ChainedMethods(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	// Test method chaining works correctly
	server, err := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithAcquirer(testMocks.acquirer).
		WithAccessControl(testMocks.accessControl).
		WithPeer(testPortPeer).
		WithReflector(testPortReflector).
		WithDHT(testPortDHT).
		WithLogger(testMocks.logger).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, server)
}

// TestServerBuilder_ProtocolOverwrite verifies that protocol configurations can be overwritten
// This test ensures that later protocol configurations properly overwrite earlier ones
func TestServerBuilder_ProtocolOverwrite(t *testing.T) {
	tests := []struct {
		name         string
		protocolName string
		setupFunc    func(*ServerBuilder)
		expectedPort int
	}{
		{
			name:         "PeerProtocol",
			protocolName: ProtocolPeer,
			setupFunc: func(b *ServerBuilder) {
				b.WithPeer(testPortPeer2).WithPeer(testPortPeer)
			},
			expectedPort: testPortPeer,
		},
		{
			name:         "ReflectorProtocol",
			protocolName: ProtocolReflector,
			setupFunc: func(b *ServerBuilder) {
				b.WithReflector(testPortReflector2).WithReflector(testPortReflector)
			},
			expectedPort: testPortReflector,
		},
		{
			name:         "DHTProtocol",
			protocolName: ProtocolDHT,
			setupFunc: func(b *ServerBuilder) {
				b.WithDHT(testPortDHT2).WithDHT(testPortDHT)
			},
			expectedPort: testPortDHT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testMocks := setupBuilderMocks(t)
			builder := NewServerBuilder().WithStorage(testMocks.storage)

			tt.setupFunc(builder)

			server, err := builder.Build()
			require.NoError(t, err)

			defaultServer := server.(*DefaultServer)
			assertServerProtocolConfig(t, defaultServer, tt.protocolName, tt.expectedPort)
		})
	}
}

func TestDefaultServer_InterfaceImplementation(t *testing.T) {
	// Verify that DefaultServer implements the Server interface
	var _ Server = &DefaultServer{}
}

// TestServerBuilder_DefaultLogger verifies that a default no-op logger is used when none is provided
// This test ensures that the builder has a sensible fallback for logging when no custom logger is specified
func TestServerBuilder_DefaultLogger(t *testing.T) {
	builder := NewServerBuilder()

	// Should have a default no-op logger
	assert.NotNil(t, builder.logger)

	// The default logger should be a no-op logger
	assert.Equal(t, zap.NewNop(), builder.logger)
}

// TestServerBuilder_WithDHTSeedNodes verifies that WithDHTSeedNodes correctly sets seed nodes on the DHT config
func TestServerBuilder_WithDHTSeedNodes(t *testing.T) {
	tests := []struct {
		name              string
		seedNodes         []string
		expectedSeedNodes []string
	}{
		{
			name:              "SingleSeedNode",
			seedNodes:         []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1"},
			expectedSeedNodes: []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1"},
		},
		{
			name:              "MultipleSeedNodes",
			seedNodes:         []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1", "/ip4/127.0.0.1/tcp/4445/p2p/QmSeedNode2"},
			expectedSeedNodes: []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1", "/ip4/127.0.0.1/tcp/4445/p2p/QmSeedNode2"},
		},
		{
			name:              "EmptySeedNodes",
			seedNodes:         []string{},
			expectedSeedNodes: []string{},
		},
		{
			name:              "NilSeedNodes",
			seedNodes:         nil,
			expectedSeedNodes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServerBuilder()

			// Configure DHT with seed nodes
			result := builder.WithDHTSeedNodes(tt.seedNodes...)

			// Verify builder returns same instance
			assertBuilderReturnsSame(t, builder, result)

			// Verify DHT config exists and has correct seed nodes
			assert.Contains(t, builder.config, ProtocolDHT)

			dhtConfig := builder.config[ProtocolDHT].(*DHTConfig)
			assert.Equal(t, tt.expectedSeedNodes, dhtConfig.SeedNodes)
		})
	}
}

// TestServerBuilder_WithDHTSeedNodesBeforeWithDHT verifies that calling WithDHTSeedNodes before WithDHT works correctly
func TestServerBuilder_WithDHTSeedNodesBeforeWithDHT(t *testing.T) {
	builder := NewServerBuilder()

	// Call WithDHTSeedNodes before WithDHT - should not crash
	seedNodes := []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1"}
	result := builder.WithDHTSeedNodes(seedNodes...)

	// Verify builder returns same instance
	assertBuilderReturnsSame(t, builder, result)

	// Verify DHT config exists and has correct seed nodes
	assert.Contains(t, builder.config, ProtocolDHT)

	dhtConfig := builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, seedNodes, dhtConfig.SeedNodes)

	// Now configure DHT - should not overwrite seed nodes
	builder.WithDHT()

	// Verify seed nodes are still there
	dhtConfig = builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, seedNodes, dhtConfig.SeedNodes)
}

// TestServerBuilder_WithDHTSeedNodesAfterWithDHT verifies that calling WithDHTSeedNodes after WithDHT works correctly
func TestServerBuilder_WithDHTSeedNodesAfterWithDHT(t *testing.T) {
	builder := NewServerBuilder()

	// First configure DHT
	builder.WithDHT()

	// Verify DHT config exists with empty seed nodes initially
	assert.Contains(t, builder.config, ProtocolDHT)
	dhtConfig := builder.config[ProtocolDHT].(*DHTConfig)
	assert.Empty(t, dhtConfig.SeedNodes)

	// Then set seed nodes
	seedNodes := []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1"}
	result := builder.WithDHTSeedNodes(seedNodes...)

	// Verify builder returns same instance
	assertBuilderReturnsSame(t, builder, result)

	// Verify seed nodes were set correctly
	dhtConfig = builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, seedNodes, dhtConfig.SeedNodes)
}

// TestServerBuilder_WithDHTAddress verifies that WithDHTAddress correctly sets the DHT address
func TestServerBuilder_WithDHTAddress(t *testing.T) {
	// Test with custom DHT address
	builder := NewServerBuilder().
		WithStorage(memory.NewMemoryStore()).
		WithDHT()

	// Configure DHT with custom address
	result := builder.WithDHTAddress("192.168.1.100:4444")

	// Verify builder returns same instance
	assertBuilderReturnsSame(t, builder, result)

	// Verify DHT config exists and has correct address
	assert.Contains(t, builder.config, ProtocolDHT)

	dhtConfig := builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, "192.168.1.100:4444", dhtConfig.Address, "DHT address should match custom address")

	// Test default behavior (no custom address)
	builder2 := NewServerBuilder().
		WithStorage(memory.NewMemoryStore()).
		WithDHT()

	// Build without setting address - should use default empty string
	server2, err := builder2.Build()
	assert.NoError(t, err)
	assert.NotNil(t, server2)

	// Verify default DHT address is empty (will be set to default in startDHT)
	dhtConfig2, exists2 := builder2.config[ProtocolDHT]
	assert.True(t, exists2, "DHT protocol should be configured")

	if dhtConfig2 != nil {
		dhtCfg2, ok := dhtConfig2.(*DHTConfig)
		assert.True(t, ok, "DHT config should be of correct type")
		// Default address should be empty string
		assert.Equal(t, "", dhtCfg2.Address, "DHT address should be empty by default")
	}
}

// TestServerBuilder_WithFixedPeerPort verifies that WithFixedPeerPort correctly sets the fixed peer port
func TestServerBuilder_WithFixedPeerPort(t *testing.T) {
	t.Run("FixedPeerPort with existing peer config", func(t *testing.T) {
		builder := NewServerBuilder().
			WithPeer(testPortPeer).
			WithFixedPeerPort(testPortPeer2).
			WithStorage(memory.NewMemoryStore())

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		assert.Equal(t, testPortPeer, peerConfig.Port, "Peer port should be preserved")
		assert.Equal(t, testPortPeer2, peerConfig.FixedPort, "Fixed peer port should be set")
	})

	t.Run("FixedPeerPort without existing peer config", func(t *testing.T) {
		builder := NewServerBuilder().
			WithFixedPeerPort(testPortPeer2).
			WithStorage(memory.NewMemoryStore())

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		assert.Equal(t, DefaultPeerPort, peerConfig.Port, "Default peer port should be used")
		assert.Equal(t, testPortPeer2, peerConfig.FixedPort, "Fixed peer port should be set")
	})

	t.Run("Multiple FixedPeerPort calls", func(t *testing.T) {
		builder := NewServerBuilder().
			WithPeer(testPortPeer).
			WithFixedPeerPort(testPortPeer2).
			WithFixedPeerPort(testPortDHT2).
			WithStorage(memory.NewMemoryStore())

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		assert.Equal(t, testPortPeer, peerConfig.Port, "Peer port should be preserved")
		assert.Equal(t, testPortDHT2, peerConfig.FixedPort, "Last fixed peer port should be used")
	})
}

// TestServerBuilder_WithDHTSeedNodesMultipleCalls verifies that calling WithDHTSeedNodes multiple times overwrites previous values
func TestServerBuilder_WithDHTSeedNodesMultipleCalls(t *testing.T) {
	builder := NewServerBuilder()

	// First call
	seedNodes1 := []string{"/ip4/127.0.0.1/tcp/4444/p2p/QmSeedNode1"}
	builder.WithDHTSeedNodes(seedNodes1...)

	// Verify first seed nodes
	assert.Contains(t, builder.config, ProtocolDHT)
	dhtConfig := builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, seedNodes1, dhtConfig.SeedNodes)

	// Second call - should overwrite
	seedNodes2 := []string{"/ip4/127.0.0.1/tcp/4445/p2p/QmSeedNode2"}
	builder.WithDHTSeedNodes(seedNodes2...)

	// Verify second seed nodes
	dhtConfig = builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, seedNodes2, dhtConfig.SeedNodes)

	// Third call - should overwrite again
	seedNodes3 := []string{"/ip4/127.0.0.1/tcp/4446/p2p/QmSeedNode3"}
	builder.WithDHTSeedNodes(seedNodes3...)

	// Verify third seed nodes
	dhtConfig = builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, seedNodes3, dhtConfig.SeedNodes)
}

// TestServerBuilder_WithExistingDHT_Tests tests the WithExistingDHT functionality
func TestServerBuilder_WithExistingDHT_Tests(t *testing.T) {
	t.Run("WithExistingDHT_stores_DHT_instance", func(t *testing.T) {
		// Test that WithExistingDHT properly stores the DHT instance in the config map
		mocks := setupBuilderMocks(t)

		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)

		// Create a server builder with existing DHT
		builder := NewServerBuilder().
			WithExistingDHT(mockDHTNode).
			WithStorage(mocks.storage).
			WithAcquirer(mocks.acquirer).
			WithAccessControl(mocks.accessControl)

		// Build the server
		server, err := builder.Build()
		require.NoError(t, err)

		// Verify that the DHT node is stored in config
		defaultServer := server.(*DefaultServer)
		_, exists := defaultServer.config[ProtocolDHT]
		assert.True(t, exists, "DHT protocol should be present in config map")
	})

	t.Run("WithExistingDHT_uses_existing_DHT_instance", func(t *testing.T) {
		// Test that the server uses the existing DHT instance instead of creating a new one
		mocks := setupBuilderMocks(t)

		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)

		// Setup mock expectations for DHT node methods
		mockDHTNode.EXPECT().Start().Return(nil)
		mockDHTNode.EXPECT().Shutdown().Return()

		// Create a server builder with existing DHT
		builder := NewServerBuilder().
			WithExistingDHT(mockDHTNode).
			WithStorage(mocks.storage).
			WithAcquirer(mocks.acquirer).
			WithAccessControl(mocks.accessControl)

		// Build the server
		server, err := builder.Build()
		require.NoError(t, err)

		// Setup mock expectations for storage.List to avoid unexpected calls
		mocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil).Times(1)

		// Start the server to trigger proper initialization
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		// Verify that the server uses the existing DHT node
		defaultServer := server.(*DefaultServer)

		// Check that the DHT node is properly stored in servers map after Start
		dhtNode, ok := defaultServer.servers[ProtocolDHT].(protocol.DHTNode)
		assert.True(t, ok, "DHT node should be properly stored in servers map")
		assert.Equal(t, mockDHTNode, dhtNode, "Should use the existing DHT node")

		// Stop the server to trigger Shutdown
		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithExistingDHT_integration_with_announcer_and_notifier", func(t *testing.T) {
		// Test that the existing DHT instance is properly integrated with announcer and notifier
		mocks := setupBuilderMocks(t)

		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)

		// Setup mock expectations for DHT node methods
		mockDHTNode.EXPECT().Start().Return(nil)
		mockDHTNode.EXPECT().Shutdown().Return()

		// Create a server builder with existing DHT
		builder := NewServerBuilder().
			WithExistingDHT(mockDHTNode).
			WithStorage(mocks.storage).
			WithAcquirer(mocks.acquirer).
			WithAccessControl(mocks.accessControl)

		// Build the server
		server, err := builder.Build()
		require.NoError(t, err)

		// Setup mock expectations for storage.List to avoid unexpected calls
		mocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil).Times(1)

		// Start the server to trigger proper initialization
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		// Verify that the server has the DHT announcer set up
		defaultServer := server.(*DefaultServer)

		// Check that the DHT announcer is created
		assert.NotNil(t, defaultServer.dhtAnnouncer, "DHT announcer should be created")

		// Verify that the notifier is set up
		assert.NotNil(t, defaultServer.notifier, "Notifier should be set up")

		// Stop the server to trigger Shutdown
		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithExistingDHT_server_lifecycle", func(t *testing.T) {
		// Test that the existing DHT instance works correctly with the server lifecycle (Start/Stop)
		mocks := setupBuilderMocks(t)

		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)

		// Setup mock expectations for DHT node methods
		mockDHTNode.EXPECT().Start().Return(nil)
		mockDHTNode.EXPECT().Shutdown().Return()

		// Create a server builder with existing DHT
		builder := NewServerBuilder().
			WithExistingDHT(mockDHTNode).
			WithStorage(mocks.storage).
			WithAcquirer(mocks.acquirer).
			WithAccessControl(mocks.accessControl)

		// Build the server
		server, err := builder.Build()
		require.NoError(t, err)

		// Setup mock expectations for storage.List to avoid unexpected calls
		mocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil).Times(1)

		// Test Start
		ctx := context.Background()

		// Start the server
		err = server.Start(ctx)
		require.NoError(t, err)

		// Verify the DHT node is now in the servers map
		defaultServer := server.(*DefaultServer)
		dhtNode, ok := defaultServer.servers[ProtocolDHT].(protocol.DHTNode)
		assert.True(t, ok, "DHT node should be in servers map after Start")
		assert.Equal(t, mockDHTNode, dhtNode, "Should use the existing DHT node")

		// Test Stop
		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithExistingDHT_with_other_protocols", func(t *testing.T) {
		// Test WithExistingDHT with different config
		mocks := setupBuilderMocks(t)

		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)

		// Setup mock expectations for DHT node methods
		mockDHTNode.EXPECT().Start().Return(nil)
		mockDHTNode.EXPECT().Shutdown().Return()

		// Create a server builder with existing DHT and other config
		builder := NewServerBuilder().
			WithExistingDHT(mockDHTNode).
			WithPeer(testPortPeer).
			WithStorage(mocks.storage).
			WithAcquirer(mocks.acquirer).
			WithAccessControl(mocks.accessControl)

		// Build the server
		server, err := builder.Build()
		require.NoError(t, err)

		// Setup mock expectations for storage.List to avoid unexpected calls
		mocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil).Times(1)

		// Start the server to trigger proper initialization
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		// Verify that the server has the expected config
		defaultServer := server.(*DefaultServer)
		_, hasPeer := defaultServer.config[ProtocolPeer]
		_, hasDHT := defaultServer.config[ProtocolDHT]

		// Both should be present - peer protocol and DHT (existing node)
		assert.True(t, hasPeer, "Peer protocol should be present")
		assert.True(t, hasDHT, "DHT protocol should be present")

		// Verify the DHT node is now in the servers map
		dhtNode, ok := defaultServer.servers[ProtocolDHT].(protocol.DHTNode)
		assert.True(t, ok, "DHT node should be in servers map after Start")
		assert.Equal(t, mockDHTNode, dhtNode, "Should use the existing DHT node")

		// Stop the server to trigger Shutdown
		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithExistingDHT_error_handling", func(t *testing.T) {
		// Test error handling when existing DHT is provided alongside DHT configuration
		mocks := setupBuilderMocks(t)

		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)

		// Create a server builder with both existing DHT and DHT config
		builder := NewServerBuilder().
			WithExistingDHT(mockDHTNode).
			WithDHT(testPortDHT).
			WithStorage(mocks.storage).
			WithAcquirer(mocks.acquirer).
			WithAccessControl(mocks.accessControl)

		// Build the server - this should work as WithExistingDHT should take precedence
		// or the builder should handle the conflict properly
		server, err := builder.Build()
		require.NoError(t, err)

		// Verify the server was built successfully
		assert.NotNil(t, server)
	})
}

// TestServerBuilder_WithAcquirerFactory verifies that acquirer factory can be properly set on the builder
// This ensures the builder correctly stores and references the provided acquirer factory function
func TestServerBuilder_WithAcquirerFactory(t *testing.T) {
	builder := NewServerBuilder()
	testMocks := setupBuilderMocks(t)

	// Create a mock acquirer factory function
	factory := func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
		return testMocks.acquirer, nil
	}

	result := builder.WithAcquirerFactory(factory)

	assertBuilderReturnsSame(t, builder, result)
	assert.NotNil(t, builder.acquirerFactory)
}

// TestServerBuilder_WithDefaultAcquirer verifies that default acquirer flag can be properly set on the builder
// This ensures the builder correctly enables the default acquirer creation
func TestServerBuilder_WithDefaultAcquirer(t *testing.T) {
	builder := NewServerBuilder()

	result := builder.WithDefaultAcquirer()

	assertBuilderReturnsSame(t, builder, result)
	assert.True(t, builder.useDefaultAcquirer)
}

// TestServerBuilder_Build_AcquirerFactoryValidation tests the validation logic for acquirer factory configuration
func TestServerBuilder_Build_AcquirerFactoryValidation(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	// Helper function to create a test factory
	createTestFactory := func() AcquirerFactory {
		return func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
			return testMocks.acquirer, nil
		}
	}

	// Helper function to create a base builder with common configuration
	createBaseBuilder := func() *ServerBuilder {
		return NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(testPortPeer)
	}

	// Helper function to assert build failure with specific error message
	assertBuildFailure := func(t *testing.T, builder *ServerBuilder, expectedError string) {
		server, err := builder.Build()
		require.Error(t, err)
		assert.Nil(t, server)
		assert.Contains(t, err.Error(), expectedError)
	}

	t.Run("AcquirerFactory_with_Acquirer_fails", func(t *testing.T) {
		builder := createBaseBuilder().
			WithAcquirer(testMocks.acquirer).
			WithAcquirerFactory(createTestFactory())

		assertBuildFailure(t, builder, "cannot specify both acquirer and acquirer factory")
	})

	t.Run("AcquirerFactory_with_DefaultAcquirer_fails", func(t *testing.T) {
		builder := createBaseBuilder().
			WithAcquirerFactory(createTestFactory()).
			WithDefaultAcquirer()

		assertBuildFailure(t, builder, "cannot specify both acquirer factory and default acquirer")
	})

	t.Run("Acquirer_with_DefaultAcquirer_fails", func(t *testing.T) {
		builder := createBaseBuilder().
			WithAcquirer(testMocks.acquirer).
			WithDefaultAcquirer()

		assertBuildFailure(t, builder, "cannot specify both acquirer and default acquirer")
	})
}

// TestServerBuilder_Build_AcquirerFactorySuccess tests successful server building with acquirer factory
func TestServerBuilder_Build_AcquirerFactorySuccess(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	// Create a mock acquirer factory function
	factory := func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
		return testMocks.acquirer, nil
	}

	builder := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithAcquirerFactory(factory).
		WithPeer(testPortPeer).
		WithAccessControl(testMocks.accessControl).
		WithLogger(testMocks.logger)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify the server is of the correct type
	defaultServer, ok := server.(*DefaultServer)
	require.True(t, ok)
	assert.Same(t, testMocks.storage, defaultServer.storage)
	assert.Same(t, testMocks.accessControl, defaultServer.accessControl)
	assert.NotNil(t, defaultServer.acquirerFactory)
	assert.Nil(t, defaultServer.acquirer) // Should be nil until initialized
}

// TestServerBuilder_Build_DefaultAcquirerSuccess tests successful server building with default acquirer
func TestServerBuilder_Build_DefaultAcquirerSuccess(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	builder := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithDefaultAcquirer().
		WithPeer(testPortPeer). // Peer protocol for basic server functionality
		WithAccessControl(testMocks.accessControl).
		WithLogger(testMocks.logger)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify the server is of the correct type
	defaultServer, ok := server.(*DefaultServer)
	require.True(t, ok)
	assert.Same(t, testMocks.storage, defaultServer.storage)
	assert.Same(t, testMocks.accessControl, defaultServer.accessControl)
	assert.NotNil(t, defaultServer.acquirerFactory)
	assert.Nil(t, defaultServer.acquirer) // Should be nil until initialized
}

// TestServerBuilder_Build_AcquirerFactoryIntegration tests the integration of acquirer factory with server lifecycle
func TestServerBuilder_Build_AcquirerFactoryIntegration(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	// Create a mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)
	mockDHTNode.EXPECT().Start().Return(nil)
	mockDHTNode.EXPECT().Shutdown().Return()

	// Create a mock acquirer factory function that tracks calls
	var factoryCalled bool
	var factoryDHTNode protocol.DHTNode
	var factoryStore storage.BlobStore

	factory := func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
		factoryCalled = true
		factoryDHTNode = dhtNode
		factoryStore = store
		return testMocks.acquirer, nil
	}

	// Mock the storage.List call that happens during DHT blob announcement
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil)

	builder := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithAcquirerFactory(factory).
		WithExistingDHT(mockDHTNode).
		WithAccessControl(testMocks.accessControl).
		WithLogger(testMocks.logger)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify the server is of the correct type
	defaultServer, ok := server.(*DefaultServer)
	require.True(t, ok)
	assert.NotNil(t, defaultServer.acquirerFactory)

	// Start the server to initialize the DHT node
	ctx := context.Background()
	err = server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(ctx)

	// Test that the factory is called when ensuring acquirer
	err = defaultServer.ensureAcquirer()
	require.NoError(t, err)

	// Verify factory was called with correct parameters
	assert.True(t, factoryCalled, "Factory should have been called")
	assert.Equal(t, mockDHTNode, factoryDHTNode, "Factory should receive the correct DHT node")
	assert.Equal(t, testMocks.storage, factoryStore, "Factory should receive the correct storage")
	assert.Equal(t, testMocks.acquirer, defaultServer.acquirer, "Acquirer should be set from factory")
}

// TestServerBuilder_Build_DefaultAcquirerIntegration tests the integration of default acquirer with server lifecycle
// and verifies that DHT-backed transfers are properly created when the server is started
func TestServerBuilder_Build_DefaultAcquirerIntegration(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	// Create a mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)
	mockDHTNode.EXPECT().Start().Return(nil)
	mockDHTNode.EXPECT().Shutdown().Return()

	// Add mock expectation for storage.List() called during DHT blob announcement
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, liblbryerrors.ErrEndOfList)

	builder := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithDefaultAcquirer().
		WithExistingDHT(mockDHTNode).
		WithAccessControl(testMocks.accessControl).
		WithLogger(testMocks.logger)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify the server is of the correct type
	defaultServer, ok := server.(*DefaultServer)
	require.True(t, ok)
	assert.NotNil(t, defaultServer.acquirerFactory)

	// Start the server to initialize the DHT node
	ctx := context.Background()
	err = server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(ctx)

	// Test that the default factory creates an acquirer with DHT-backed transfers
	err = defaultServer.ensureAcquirer()
	require.NoError(t, err)

	// Verify acquirer was created
	assert.NotNil(t, defaultServer.acquirer, "Default acquirer should be created")

	// Verify that the DHT node is properly initialized in the server
	dhtNode := defaultServer.getDhtNodeUnsafe()
	assert.NotNil(t, dhtNode, "DHT node should be initialized after server.Start()")
	assert.Equal(t, mockDHTNode, dhtNode, "DHT node should match the mock provided to builder")

	// Verify that the acquirer was created with the expected DHT node
	// Since we're using the default factory, we can verify this indirectly
	// by ensuring the acquirer is non-nil when DHT is provided
	assert.NotNil(t, defaultServer.acquirer, "Acquirer should be created when DHT node is provided")
}

// TestServerBuilder_Build_DefaultAcquirerDHTUsage verifies that the default acquirer factory
// properly utilizes the provided DHT node to create DHT-backed transfers
func TestServerBuilder_Build_DefaultAcquirerDHTUsage(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	t.Run("With DHT node", func(t *testing.T) {
		// Create a mock DHT node
		mockDHTNode := protocolMocks.NewMockDHTNode(t)
		mockDHTNode.EXPECT().Start().Return(nil)
		mockDHTNode.EXPECT().Shutdown().Return()

		// Add mock expectation for storage.List() called during DHT blob announcement
		testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, liblbryerrors.ErrEndOfList)

		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithDefaultAcquirer().
			WithExistingDHT(mockDHTNode).
			WithAccessControl(testMocks.accessControl).
			WithLogger(testMocks.logger)

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)

		// Start the server to initialize the DHT node
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)
		defer server.Stop(ctx)

		// Verify that the DHT node is properly initialized
		dhtNode := defaultServer.getDhtNodeUnsafe()
		assert.NotNil(t, dhtNode, "DHT node should be initialized after server.Start()")
		assert.Equal(t, mockDHTNode, dhtNode, "DHT node should match the mock provided to builder")

		// Verify that the default factory creates an acquirer when DHT is provided
		err = defaultServer.ensureAcquirer()
		require.NoError(t, err)
		assert.NotNil(t, defaultServer.acquirer, "Acquirer should be created when DHT node is provided")
	})

	t.Run("Without DHT node", func(t *testing.T) {
		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithDefaultAcquirer().
			WithPeer(testPortPeer). // Only peer protocol, no DHT
			WithAccessControl(testMocks.accessControl).
			WithLogger(testMocks.logger)

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)

		// Start the server to follow the documented contract for ensureAcquirer()
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)
		defer server.Stop(ctx)

		// Verify that the default factory still creates an acquirer even without DHT
		// (it will have no transfers, but should still be created)
		err = defaultServer.ensureAcquirer()
		require.NoError(t, err)
		assert.NotNil(t, defaultServer.acquirer, "Acquirer should still be created even without DHT node")

		// Verify that no DHT node is available when not configured
		dhtNode := defaultServer.getDhtNodeUnsafe()
		assert.Nil(t, dhtNode, "DHT node should be nil when not configured")
	})
}

// TestServerBuilder_ChainedMethodsWithAcquirerFactory verifies that builder method chaining works correctly with acquirer factory
func TestServerBuilder_ChainedMethodsWithAcquirerFactory(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	factory := func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
		return testMocks.acquirer, nil
	}

	// Test method chaining works correctly with acquirer factory
	server, err := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithAcquirerFactory(factory).
		WithAccessControl(testMocks.accessControl).
		WithPeer(testPortPeer).
		WithLogger(testMocks.logger).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, server)
}

// TestServerBuilder_ChainedMethodsWithDefaultAcquirer verifies that builder method chaining works correctly with default acquirer
func TestServerBuilder_ChainedMethodsWithDefaultAcquirer(t *testing.T) {
	testMocks := setupBuilderMocks(t)

	// Test method chaining works correctly with default acquirer
	server, err := NewServerBuilder().
		WithStorage(testMocks.storage).
		WithDefaultAcquirer().
		WithAccessControl(testMocks.accessControl).
		WithPeer(testPortPeer).
		WithLogger(testMocks.logger).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, server)
}
