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

func TestNewServerBuilder(t *testing.T) {
	builder := NewServerBuilder()

	assert.NotNil(t, builder)
	assert.Empty(t, builder.protocols)
	assert.NotNil(t, builder.logger)
}

func TestServerBuilder_WithStorage(t *testing.T) {
	builder := NewServerBuilder()
	mockStorage := storageMocks.NewMockBlobStore(t)

	result := builder.WithStorage(mockStorage)

	assert.Same(t, builder, result) // Should return the same builder instance
	assert.Same(t, mockStorage, builder.storage)
}

func TestServerBuilder_WithAcquirer(t *testing.T) {
	builder := NewServerBuilder()
	mockAcquirer := mocks.NewMockBlobAcquirer(t)

	result := builder.WithAcquirer(mockAcquirer)

	assert.Same(t, builder, result) // Should return the same builder instance
	assert.Same(t, mockAcquirer, builder.acquirer)
}

func TestServerBuilder_WithAccessControl(t *testing.T) {
	builder := NewServerBuilder()
	mockAccessControl := storageMocks.NewMockAccessControl(t)

	result := builder.WithAccessControl(mockAccessControl)

	assert.Same(t, builder, result) // Should return the same builder instance
	assert.Same(t, mockAccessControl, builder.accessControl)
}

func TestServerBuilder_WithLogger(t *testing.T) {
	builder := NewServerBuilder()
	logger := zaptest.NewLogger(t)

	result := builder.WithLogger(logger)

	assert.Same(t, builder, result) // Should return the same builder instance
	assert.Same(t, logger, builder.logger)
}

func TestServerBuilder_WithPeer_DefaultPort(t *testing.T) {
	builder := NewServerBuilder()

	result := builder.WithPeer()

	assert.Same(t, builder, result)
	assert.Contains(t, builder.protocols, ProtocolPeer)

	config := builder.protocols[ProtocolPeer].(*PeerConfig)
	assert.Equal(t, DefaultPeerPort, config.Port)
}

func TestServerBuilder_WithPeer_CustomPort(t *testing.T) {
	builder := NewServerBuilder()
	customPort := 8080

	result := builder.WithPeer(customPort)

	assert.Same(t, builder, result)
	assert.Contains(t, builder.protocols, ProtocolPeer)

	config := builder.protocols[ProtocolPeer].(*PeerConfig)
	assert.Equal(t, customPort, config.Port)
}

func TestServerBuilder_WithReflector_DefaultPort(t *testing.T) {
	builder := NewServerBuilder()

	result := builder.WithReflector()

	assert.Same(t, builder, result)
	assert.Contains(t, builder.protocols, ProtocolReflector)

	config := builder.protocols[ProtocolReflector].(*ReflectorConfig)
	assert.Equal(t, DefaultReflectorPort, config.Port)
}

func TestServerBuilder_WithReflector_CustomPort(t *testing.T) {
	builder := NewServerBuilder()
	customPort := 9090

	result := builder.WithReflector(customPort)

	assert.Same(t, builder, result)
	assert.Contains(t, builder.protocols, ProtocolReflector)

	config := builder.protocols[ProtocolReflector].(*ReflectorConfig)
	assert.Equal(t, customPort, config.Port)
}

func TestServerBuilder_WithDHT_DefaultPort(t *testing.T) {
	builder := NewServerBuilder()

	result := builder.WithDHT()

	assert.Same(t, builder, result)
	assert.Contains(t, builder.protocols, ProtocolDHT)

	config := builder.protocols[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, DefaultDHTPort, config.Port)
}

func TestServerBuilder_WithDHT_CustomPort(t *testing.T) {
	builder := NewServerBuilder()
	customPort := 8080

	result := builder.WithDHT(customPort)

	assert.Same(t, builder, result)
	assert.Contains(t, builder.protocols, ProtocolDHT)

	config := builder.protocols[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, customPort, config.Port)
}

func TestServerBuilder_Build_Success(t *testing.T) {
	builder := NewServerBuilder()
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	builder.
		WithStorage(mockStorage).
		WithAcquirer(mockAcquirer).
		WithAccessControl(mockAccessControl).
		WithPeer(5567).
		WithLogger(logger)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	// Verify the server is of the correct type
	defaultServer, ok := server.(*DefaultServer)
	require.True(t, ok)
	assert.Same(t, mockStorage, defaultServer.storage)
	assert.Same(t, mockAcquirer, defaultServer.acquirer)
	assert.Same(t, mockAccessControl, defaultServer.accessControl)
	assert.Equal(t, "server", defaultServer.logger.Name())
	assert.Contains(t, defaultServer.protocols, ProtocolPeer)
}

func TestServerBuilder_Build_MultipleProtocols(t *testing.T) {
	builder := NewServerBuilder()
	mockStorage := storageMocks.NewMockBlobStore(t)

	builder.
		WithStorage(mockStorage).
		WithPeer(5567).
		WithReflector(5566).
		WithDHT(4444)

	server, err := builder.Build()

	require.NoError(t, err)
	assert.NotNil(t, server)

	defaultServer := server.(*DefaultServer)
	assert.Contains(t, defaultServer.protocols, ProtocolPeer)
	assert.Contains(t, defaultServer.protocols, ProtocolReflector)
	assert.Contains(t, defaultServer.protocols, ProtocolDHT)
}

func TestServerBuilder_Build_Error_NoStorage(t *testing.T) {
	builder := NewServerBuilder()
	builder.WithPeer(5567) // Add protocol but no storage

	server, err := builder.Build()

	assert.Error(t, err)
	assert.Nil(t, server)
	assert.Contains(t, err.Error(), "storage is required")
}

func TestServerBuilder_Build_Error_NoProtocols(t *testing.T) {
	builder := NewServerBuilder()
	mockStorage := storageMocks.NewMockBlobStore(t)
	builder.WithStorage(mockStorage) // Add storage but no protocols

	server, err := builder.Build()

	assert.Error(t, err)
	assert.Nil(t, server)
	assert.Contains(t, err.Error(), "at least one protocol must be configured")
}

func TestServerBuilder_Build_Error_NoStorageAndNoProtocols(t *testing.T) {
	builder := NewServerBuilder() // No storage, no protocols

	server, err := builder.Build()

	assert.Error(t, err)
	assert.Nil(t, server)
	// Should return the first error encountered (storage check comes first)
	assert.Contains(t, err.Error(), "storage is required")
}

func TestServerBuilder_ChainedMethods(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	// Test method chaining works correctly
	server, err := NewServerBuilder().
		WithStorage(mockStorage).
		WithAcquirer(mockAcquirer).
		WithAccessControl(mockAccessControl).
		WithPeer(8080).
		WithReflector(9090).
		WithDHT(4444).
		WithLogger(logger).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, server)
}

func TestServerBuilder_ProtocolOverwrite(t *testing.T) {
	builder := NewServerBuilder()
	mockStorage := storageMocks.NewMockBlobStore(t)

	// Add peer protocol twice with different ports
	builder.
		WithStorage(mockStorage).
		WithPeer(5567).
		WithPeer(8080) // This should overwrite the previous peer config

	server, err := builder.Build()

	require.NoError(t, err)

	defaultServer := server.(*DefaultServer)
	config := defaultServer.protocols[ProtocolPeer].(*PeerConfig)
	assert.Equal(t, 8080, config.Port) // Should have the last port
}

func TestServerBuilder_DHTProtocolOverwrite(t *testing.T) {
	builder := NewServerBuilder()
	mockStorage := storageMocks.NewMockBlobStore(t)

	// Add DHT protocol twice with different ports
	builder.
		WithStorage(mockStorage).
		WithDHT(4444).
		WithDHT(5555) // This should overwrite the previous DHT config

	server, err := builder.Build()

	require.NoError(t, err)

	defaultServer := server.(*DefaultServer)
	config := defaultServer.protocols[ProtocolDHT].(*DHTConfig)
	assert.Equal(t, 5555, config.Port) // Should have the last port
}

func TestDefaultServer_InterfaceImplementation(t *testing.T) {
	// Verify that DefaultServer implements the Server interface
	var _ Server = &DefaultServer{}
}

func TestServerBuilder_DefaultLogger(t *testing.T) {
	builder := NewServerBuilder()

	// Should have a default no-op logger
	assert.NotNil(t, builder.logger)

	// The default logger should be a no-op logger
	assert.Equal(t, zap.NewNop(), builder.logger)
}
