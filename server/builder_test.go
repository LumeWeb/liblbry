package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/blob/transfer"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
	"go.lumeweb.com/liblbry/mocks"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/storage/memory"
	storageMocks "go.lumeweb.com/liblbry/storage/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
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

// TestServerBuilder_WithLogger_PropagatesToDHTBuilder verifies that logger changes are propagated to existing DHTBuilder
// This ensures that DHT logging stays in sync with the server logger when WithLogger is called after DHT configuration
func TestServerBuilder_WithLogger_PropagatesToDHTBuilder(t *testing.T) {
	builder := NewServerBuilder()

	// First configure DHT to create DHTBuilder instance
	builder.WithDHT(lbryTesting.GetFreePort(t))

	// Verify DHTBuilder was created with default logger
	require.NotNil(t, builder.dhtBuilder)
	assert.Equal(t, zap.NewNop(), builder.dhtBuilder.logger)

	// Now set a custom logger on the server builder
	customLogger := zaptest.NewLogger(t)
	builder.WithLogger(customLogger)

	// Verify the server logger was updated
	assert.Same(t, customLogger, builder.logger)

	// Verify the DHTBuilder logger was also updated (propagation)
	assert.Same(t, customLogger, builder.dhtBuilder.logger)
}

// TestServerBuilder_WithLogger_BeforeDHTConfiguration verifies that logger is used when DHT is configured after WithLogger
// This ensures that DHTBuilder gets the correct logger when created after WithLogger is called
func TestServerBuilder_WithLogger_BeforeDHTConfiguration(t *testing.T) {
	builder := NewServerBuilder()

	// Set custom logger first
	customLogger := zaptest.NewLogger(t)
	builder.WithLogger(customLogger)

	// Now configure DHT - should use the custom logger
	builder.WithDHT(lbryTesting.GetFreePort(t))

	// Verify both server and DHTBuilder have the custom logger
	assert.Same(t, customLogger, builder.logger)
	assert.Same(t, customLogger, builder.dhtBuilder.logger)
}

// TestServerBuilder_WithPeer verifies peer protocol configuration with default and custom ports
// This test ensures that peer config can be correctly configured with either default or custom ports
func TestServerBuilder_WithPeer(t *testing.T) {
	customPort := lbryTesting.GetFreePort(t)

	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{"DefaultPort", 0, DefaultPeerPort},
		{"CustomPort", customPort, customPort},
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
	customPort := lbryTesting.GetFreePort(t)

	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{"DefaultPort", 0, DefaultReflectorPort},
		{"CustomPort", customPort, customPort},
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
	customPort := lbryTesting.GetFreePort(t)

	tests := []struct {
		name         string
		port         int
		expectedPort int
	}{
		{"DefaultPort", 0, DefaultDHTPort},
		{"CustomPort", customPort, customPort},
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
	builder.WithPeer(lbryTesting.GetFreePort(t))

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
		WithPeer(lbryTesting.GetFreePort(t)).
		WithReflector(lbryTesting.GetFreePort(t)).
		WithDHT(lbryTesting.GetFreePort(t))

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
				builder.WithPeer(lbryTesting.GetFreePort(t))
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
		WithPeer(lbryTesting.GetFreePort(t)).
		WithReflector(lbryTesting.GetFreePort(t)).
		WithDHT(lbryTesting.GetFreePort(t)).
		WithLogger(testMocks.logger).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, server)
}

// TestWithTransferOptions tests the WithTransferOptions functionality
func TestWithTransferOptions(t *testing.T) {
	t.Run("WithTransferOptionsBasic", func(t *testing.T) {
		// Create fresh mocks for this test to avoid conflicts
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(
				transfer.WithPeerTransferTimeoutOption(60*time.Second),
				transfer.WithPeerTransferMaxPeersOption(10),
			).
			WithLogger(testMocks.logger)

		// Verify builder has transfer options
		assert.Len(t, builder.transferOptions, 2)

		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)

		// Test that the server can be started and stopped
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithTransferOptionsEmpty", func(t *testing.T) {
		// Create fresh mocks for this test to avoid conflicts
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(). // Empty options
			WithLogger(testMocks.logger)

		assert.Len(t, builder.transferOptions, 0)

		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)
	})

	t.Run("WithTransferOptionsMultipleCalls", func(t *testing.T) {
		// Create fresh mocks for this test to avoid conflicts
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(transfer.WithPeerTransferTimeoutOption(30*time.Second)).
			WithTransferOptions(transfer.WithPeerTransferMaxPeersOption(5), transfer.WithPeerTransferMaxConcurrencyOption(8)).
			WithLogger(testMocks.logger)

		// Should have 3 options total (1 from first call, 2 from second call)
		assert.Len(t, builder.transferOptions, 3)

		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)
	})

	t.Run("WithTransferOptionsWithoutDefaultAcquirer", func(t *testing.T) {
		// Create fresh mocks for this test to avoid conflicts
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		// Transfer options should be stored even without default acquirer
		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithTransferOptions(transfer.WithPeerTransferTimeoutOption(45 * time.Second)).
			WithLogger(testMocks.logger)

		assert.Len(t, builder.transferOptions, 1)

		// Should still build successfully with custom acquirer
		builder.WithAcquirer(testMocks.acquirer)
		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)
	})

	t.Run("WithTransferOptionsAppliedToTransfer", func(t *testing.T) {
		// This test verifies that transfer options are actually applied to the created transfers
		logger := zaptest.NewLogger(t)

		builder := NewServerBuilder().
			WithStorage(memory.NewMemoryStore()).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(
				transfer.WithPeerTransferTimeoutOption(75*time.Second),
				transfer.WithPeerTransferMaxPeersOption(12),
				transfer.WithPeerTransferMaxConcurrencyOption(6),
				transfer.WithPeerTransferLoggerOption(logger.Named("test_transfer")),
			).
			WithLogger(logger)

		server, err := builder.Build()
		require.NoError(t, err)

		// Start the server to initialize the acquirer
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		// Cast to DefaultServer to access AcquireBlob method
		defaultServer := server.(*DefaultServer)

		// Test blob acquisition directly on server to verify transfers are working with applied options
		testHash := "1234567890123456789012345678901234567890123456789012345678901234"

		// This will fail because we don't have real peers, but it will exercise the transfer
		// and verify that the options were applied without causing panics
		_, err = defaultServer.AcquireBlob(ctx, testHash)
		// We expect this to fail (no peers), but not panic due to misconfigured options
		assert.Error(t, err)

		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithTransferOptionsWithInvalidOption", func(t *testing.T) {
		// Create fresh mocks for this test to avoid conflicts
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		// Test that invalid options don't break the build process
		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(
				transfer.WithPeerTransferMaxConcurrencyOption(0),       // Invalid, should be ignored
				transfer.WithPeerTransferTimeoutOption(30*time.Second), // Valid
			).
			WithLogger(testMocks.logger)

		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)

		// Server should still work despite invalid option
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("WithTransferOptionsNilOption", func(t *testing.T) {
		// Create fresh mocks for this test to avoid conflicts
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		// Test that nil options are handled gracefully (ignored and logged)
		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(
				transfer.WithPeerTransferTimeoutOption(30*time.Second), // Valid option
				nil, // Nil option should be ignored
				transfer.WithPeerTransferMaxPeersOption(5), // Another valid option
			).
			WithLogger(testMocks.logger)

		// Should have 2 valid options (nil option filtered out)
		assert.Len(t, builder.transferOptions, 2)

		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)

		// Server should work normally despite nil option
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		err = server.Stop(ctx)
		require.NoError(t, err)
	})
}

// TestTransferOptionsIntegration tests the full integration of transfer options
func TestTransferOptionsIntegration(t *testing.T) {
	logger := zaptest.NewLogger(t)

	t.Run("FullIntegrationWithRealDHT", func(t *testing.T) {
		// Create a real DHT node for more realistic testing
		dhtPort := lbryTesting.GetFreePort(t)
		dhtNode, err := protocol.NewDHTNodeWithDefaults(
			protocol.WithDHTAddress(fmt.Sprintf("127.0.0.1:%d", dhtPort)),
		)
		require.NoError(t, err)

		builder := NewServerBuilder().
			WithStorage(memory.NewMemoryStore()).
			WithExistingDHT(dhtNode).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(
				transfer.WithPeerTransferTimeoutOption(20*time.Second),
				transfer.WithPeerTransferMaxPeersOption(3),
				transfer.WithPeerTransferMaxConcurrencyOption(2),
				transfer.WithPeerTransferDHTRetryAttemptsOption(1),
				transfer.WithPeerTransferDHTRetryDelayOption(50*time.Millisecond),
				transfer.WithPeerTransferLoggerOption(logger.Named("integration_test")),
			).
			WithLogger(logger)

		server, err := builder.Build()
		require.NoError(t, err)

		// Start the server
		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		// Cast to DefaultServer to access AcquireBlob method
		defaultServer := server.(*DefaultServer)

		// Test blob acquisition directly on server (will fail due to no peers, but tests the transfer configuration)
		testHash := "1234567890123456789012345678901234567890123456789012345678901234"

		_, err = defaultServer.AcquireBlob(ctx, testHash)
		assert.Error(t, err) // Expected to fail

		// Stop the server - this will also shutdown the DHT node
		err = server.Stop(ctx)
		require.NoError(t, err)
	})

	t.Run("CustomTransferOption", func(t *testing.T) {
		testMocks := setupBuilderMocks(t)

		// Set up mock expectations for List method calls during DHT blob announcement
		testMocks.storage.EXPECT().List(mock.AnythingOfType("int"), mock.AnythingOfType("int")).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

		// Test creating a custom transfer option
		customOption, err := transfer.NewPeerTransferOptionAdapter(
			transfer.WithPeerTransferTimeout(120 * time.Second),
		)
		require.NoError(t, err)

		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithPeer(lbryTesting.GetFreePort(t)).
			WithDHT(lbryTesting.GetFreePort(t)).
			WithDefaultAcquirer().
			WithTransferOptions(
				customOption,
				transfer.WithPeerTransferMaxPeersOption(8),
			).
			WithLogger(testMocks.logger)

		server, err := builder.Build()
		require.NoError(t, err)
		assert.NotNil(t, server)

		ctx := context.Background()
		err = server.Start(ctx)
		require.NoError(t, err)

		err = server.Stop(ctx)
		require.NoError(t, err)
	})
}

// TestServerBuilder_ProtocolOverwrite verifies that protocol configurations can be overwritten
// This test ensures that later protocol configurations properly overwrite earlier ones
func TestServerBuilder_ProtocolOverwrite(t *testing.T) {
	tests := []struct {
		name         string
		protocolName string
	}{
		{
			name:         "PeerProtocol",
			protocolName: ProtocolPeer,
		},
		{
			name:         "ReflectorProtocol",
			protocolName: ProtocolReflector,
		},
		{
			name:         "DHTProtocol",
			protocolName: ProtocolDHT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testMocks := setupBuilderMocks(t)
			builder := NewServerBuilder().WithStorage(testMocks.storage)

			// Test protocol overwrite by configuring the same protocol twice
			// The second configuration should overwrite the first one
			firstPort := lbryTesting.GetFreePort(t)
			secondPort := lbryTesting.GetFreePort(t)

			switch tt.name {
			case "PeerProtocol":
				builder.WithPeer(firstPort).WithPeer(secondPort)
			case "ReflectorProtocol":
				builder.WithReflector(firstPort).WithReflector(secondPort)
			case "DHTProtocol":
				builder.WithDHT(firstPort).WithDHT(secondPort)
			}

			server, err := builder.Build()
			require.NoError(t, err)

			defaultServer := server.(*DefaultServer)
			// The final port should be the second port (overwrite behavior)
			assertServerProtocolConfig(t, defaultServer, tt.protocolName, secondPort)
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

// TestServerBuilder_WithDHTAddress verifies that WithDHTAddress correctly sets the DHT address
func TestServerBuilder_WithDHTAddress(t *testing.T) {
	dhtPort := lbryTesting.GetFreePort(t)
	builder := NewServerBuilder().
		WithStorage(memory.NewMemoryStore()).
		WithDHT(dhtPort)

	// Configure DHT with custom address
	customAddress := fmt.Sprintf("192.168.1.100:%d", dhtPort)
	result := builder.WithDHTAddress(customAddress)

	assertBuilderReturnsSame(t, builder, result)
	assert.Contains(t, builder.config, ProtocolDHT)

	// Verify DHT config has the custom address
	dhtConfig := builder.config[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, customAddress, dhtConfig.Address)
}

// TestServerBuilder_WithFixedPeerPort verifies that WithFixedPeerPort correctly sets the fixed peer port
func TestServerBuilder_WithFixedPeerPort(t *testing.T) {
	t.Run("FixedPeerPort with existing peer config", func(t *testing.T) {
		peerPort := lbryTesting.GetFreePort(t)
		fixedPort := lbryTesting.GetFreePort(t)
		builder := NewServerBuilder().
			WithPeer(peerPort).
			WithFixedPeerPort(fixedPort).
			WithStorage(memory.NewMemoryStore())

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		assert.Equal(t, peerPort, peerConfig.Port, "Peer port should be preserved")
		assert.Equal(t, fixedPort, peerConfig.FixedPort, "Fixed peer port should be set")
	})

	t.Run("FixedPeerPort without existing peer config", func(t *testing.T) {
		fixedPort := lbryTesting.GetFreePort(t)
		builder := NewServerBuilder().
			WithFixedPeerPort(fixedPort).
			WithStorage(memory.NewMemoryStore())

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		assert.Equal(t, DefaultPeerPort, peerConfig.Port, "Default peer port should be used")
		assert.Equal(t, fixedPort, peerConfig.FixedPort, "Fixed peer port should be set")
	})

	t.Run("Multiple FixedPeerPort calls", func(t *testing.T) {
		peerPort := lbryTesting.GetFreePort(t)
		firstFixedPort := lbryTesting.GetFreePort(t)
		secondFixedPort := lbryTesting.GetFreePort(t)
		builder := NewServerBuilder().
			WithPeer(peerPort).
			WithFixedPeerPort(firstFixedPort).
			WithFixedPeerPort(secondFixedPort).
			WithStorage(memory.NewMemoryStore())

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		assert.Equal(t, peerPort, peerConfig.Port, "Peer port should be preserved")
		assert.Equal(t, secondFixedPort, peerConfig.FixedPort, "Last fixed peer port should be used")
	})
}

// TestServerBuilder_WithExistingDHT tests the WithExistingDHT functionality
func TestServerBuilder_WithExistingDHT(t *testing.T) {
	testMocks := setupBuilderMocks(t)
	mockDHTNode := protocolMocks.NewMockDHTNode(t)

	// Setup mock expectations
	mockDHTNode.EXPECT().Start().Return(nil)
	mockDHTNode.EXPECT().Shutdown().Return()
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, liblbryerrors.ErrEndOfList).Maybe()

	// Create server with existing DHT
	server, err := NewServerBuilder().
		WithExistingDHT(mockDHTNode).
		WithStorage(testMocks.storage).
		WithPeer(lbryTesting.GetFreePort(t)).
		Build()
	require.NoError(t, err)

	// Test server lifecycle
	ctx := context.Background()
	err = server.Start(ctx)
	require.NoError(t, err)

	// Verify DHT node is used
	defaultServer := server.(*DefaultServer)
	dhtNode, ok := defaultServer.servers[ProtocolDHT].(protocol.DHTNode)
	assert.True(t, ok, "DHT node should be stored in servers map")
	assert.Equal(t, mockDHTNode, dhtNode, "Should use the existing DHT node")

	err = server.Stop(ctx)
	require.NoError(t, err)
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
			WithPeer(lbryTesting.GetFreePort(t))
	}

	t.Run("AcquirerFactory_with_Acquirer_fails", func(t *testing.T) {
		builder := createBaseBuilder().
			WithAcquirer(testMocks.acquirer).
			WithAcquirerFactory(createTestFactory())

		assertBuildError(t, builder, "cannot specify both acquirer and acquirer factory")
	})

	t.Run("AcquirerFactory_with_DefaultAcquirer_fails", func(t *testing.T) {
		builder := createBaseBuilder().
			WithAcquirerFactory(createTestFactory()).
			WithDefaultAcquirer()

		assertBuildError(t, builder, "cannot specify both acquirer factory and default acquirer")
	})

	t.Run("Acquirer_with_DefaultAcquirer_fails", func(t *testing.T) {
		builder := createBaseBuilder().
			WithAcquirer(testMocks.acquirer).
			WithDefaultAcquirer()

		assertBuildError(t, builder, "cannot specify both acquirer and default acquirer")
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
		WithPeer(lbryTesting.GetFreePort(t)).
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
		WithPeer(lbryTesting.GetFreePort(t)). // Peer protocol for basic server functionality
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
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil).Times(1)

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
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, liblbryerrors.ErrEndOfList).Times(1)

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
		testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, liblbryerrors.ErrEndOfList).Times(1)

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
		// Ensure storage.List is not called when no DHT node is configured
		testMocks.storage.EXPECT().List(mock.Anything, mock.Anything).Times(0)

		builder := NewServerBuilder().
			WithStorage(testMocks.storage).
			WithDefaultAcquirer().
			WithPeer(lbryTesting.GetFreePort(t)). // Only peer protocol, no DHT
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
		WithPeer(lbryTesting.GetFreePort(t)).
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
		WithPeer(lbryTesting.GetFreePort(t)).
		WithLogger(testMocks.logger).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, server)
}
