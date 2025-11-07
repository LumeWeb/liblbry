package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/mocks"
	storageMocks "go.lumeweb.com/liblbry/storage/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Test constants for protocol ports
// These values are chosen to avoid conflicts with standard ports
// and to clearly distinguish between different protocol types
const (
	testPortPeer      = 8080
	testPortReflector = 9090
	testPortDHT       = 4444
	testPortPeer2     = 5567
	testPortReflector2 = 5566
	testPortDHT2      = 5555
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
	assert.Contains(t, builder.protocols, protocolName, "Protocol should be configured")
	
	switch protocolName {
	case ProtocolPeer:
		config := builder.protocols[protocolName].(*PeerConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolReflector:
		config := builder.protocols[protocolName].(*ReflectorConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolDHT:
		config := builder.protocols[protocolName].(*DHTConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	}
}

// assertServerProtocolConfig validates that a built server has the correct protocol configuration
// This ensures that the server construction process properly applies protocol settings
func assertServerProtocolConfig(t *testing.T, server *DefaultServer, protocolName string, expectedPort int) {
	assert.Contains(t, server.protocols, protocolName, "Protocol should be configured")
	
	switch protocolName {
	case ProtocolPeer:
		config := server.protocols[protocolName].(*PeerConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolReflector:
		config := server.protocols[protocolName].(*ReflectorConfig)
		assert.Equal(t, expectedPort, config.Port, "Port should match expected value")
	case ProtocolDHT:
		config := server.protocols[protocolName].(*DHTConfig)
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
	assert.Empty(t, builder.protocols)
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
// This test ensures that peer protocols can be correctly configured with either default or custom ports
func TestServerBuilder_WithPeer(t *testing.T) {
	tests := []struct {
		name        string
		port        int
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
// This test ensures that reflector protocols can be correctly configured with either default or custom ports
func TestServerBuilder_WithReflector(t *testing.T) {
	tests := []struct {
		name        string
		port        int
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
// This test ensures that DHT protocols can be correctly configured with either default or custom ports
func TestServerBuilder_WithDHT(t *testing.T) {
	tests := []struct {
		name        string
		port        int
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
	assert.Contains(t, defaultServer.protocols, ProtocolPeer)
}

// TestServerBuilder_Build_MultipleProtocols verifies server building with multiple protocols
// This test ensures that servers can be constructed with multiple protocols simultaneously
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
	assert.Contains(t, defaultServer.protocols, ProtocolPeer)
	assert.Contains(t, defaultServer.protocols, ProtocolReflector)
	assert.Contains(t, defaultServer.protocols, ProtocolDHT)
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
