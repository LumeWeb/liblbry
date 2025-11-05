package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/mocks"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	storageMocks "go.lumeweb.com/liblbry/storage/mocks"
	"go.uber.org/zap/zaptest"
)

func TestDefaultServer_Start_Success(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer: &PeerConfig{Port: 0}, // Use port 0 for automatic port assignment
		},
		logger: logger,
	}

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.NotNil(t, server.ctx)
	assert.NotNil(t, server.cancel)
	assert.NotNil(t, server.listeners)
	assert.NotNil(t, server.servers)
	assert.Contains(t, server.listeners, ProtocolPeer)

	// Clean up
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_Start_MultipleProtocols(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer:      &PeerConfig{Port: 0},
			ProtocolReflector: &ReflectorConfig{Port: 0},
		},
		logger: logger,
	}

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolPeer)
	assert.Contains(t, server.listeners, ProtocolReflector)

	// Clean up
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_Start_WithDHT(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolDHT: &DHTConfig{Port: 0}, // Use port 0 for automatic port assignment
		},
		logger: logger,
	}

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.Contains(t, server.servers, ProtocolDHT)

	// Clean up
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_Start_AllProtocols(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer:      &PeerConfig{Port: 0},
			ProtocolReflector: &ReflectorConfig{Port: 0},
			ProtocolDHT:       &DHTConfig{Port: 0},
		},
		logger: logger,
	}

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolPeer)
	assert.Contains(t, server.listeners, ProtocolReflector)
	assert.Contains(t, server.servers, ProtocolDHT)

	// Clean up
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_Start_UnknownProtocol(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			"unknown": &PeerConfig{Port: 0},
		},
		logger: logger,
	}

	ctx := context.Background()
	err := server.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown protocol: unknown")
}

func TestDefaultServer_Start_PortAlreadyInUse(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	// Start a listener on a specific port first
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func(listener net.Listener) {
		err := listener.Close()
		require.NoError(t, err)
	}(listener)

	port := listener.Addr().(*net.TCPAddr).Port

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer: &PeerConfig{Port: port},
		},
		logger: logger,
	}

	ctx := context.Background()
	err = server.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start Peer protocol")
}

func TestDefaultServer_Stop_Success(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer: &PeerConfig{Port: 0},
		},
		logger: logger,
	}

	ctx := context.Background()

	// Start the server first
	err := server.Start(ctx)
	require.NoError(t, err)

	// Verify server is running
	assert.NotNil(t, server.listeners)
	assert.Contains(t, server.listeners, ProtocolPeer)

	// Stop the server
	err = server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_Stop_WithTimeout(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer: &PeerConfig{Port: 0},
		},
		logger: logger,
	}

	ctx := context.Background()

	// Start the server first
	err := server.Start(ctx)
	require.NoError(t, err)

	// Stop with a very short timeout to test timeout behavior
	stopCtx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	err = server.Stop(stopCtx)
	// This might succeed or timeout depending on timing, both are acceptable
	// The important thing is that it doesn't panic
	assert.True(t, err == nil || err == context.DeadlineExceeded)
}

func TestDefaultServer_Stop_NotStarted(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols:     map[string]interface{}{},
		logger:        logger,
	}

	// Stop without starting should not panic
	err := server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_startPeer(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols:     map[string]interface{}{},
		logger:        logger,
		listeners:     make(map[string]net.Listener),
		servers:       make(map[string]interface{}),
	}

	// Initialize context to avoid nil pointer in startTCPProtocol
	server.ctx, server.cancel = context.WithCancel(context.Background())

	config := &PeerConfig{Port: 0}
	err := server.startPeer(config)

	assert.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolPeer)
	assert.Contains(t, server.servers, ProtocolPeer)

	// Clean up
	if listener, exists := server.listeners[ProtocolPeer]; exists {
		err := listener.Close()
		require.NoError(t, err)
	}

	// Clean up context
	server.cancel()
}

func TestDefaultServer_startReflector(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols:     map[string]interface{}{},
		logger:        logger,
		listeners:     make(map[string]net.Listener),
		servers:       make(map[string]interface{}),
	}

	// Initialize context to avoid nil pointer in startTCPProtocol
	server.ctx, server.cancel = context.WithCancel(context.Background())

	config := &ReflectorConfig{Port: 0}
	err := server.startReflector(config)

	assert.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolReflector)
	assert.Contains(t, server.servers, ProtocolReflector)

	// Clean up
	if listener, exists := server.listeners[ProtocolReflector]; exists {
		err := listener.Close()
		require.NoError(t, err)
	}

	// Clean up context
	server.cancel()
}

func TestDefaultServer_startTCPProtocol(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols:     map[string]interface{}{},
		logger:        logger,
		listeners:     make(map[string]net.Listener),
		servers:       make(map[string]interface{}),
	}

	// Initialize context to avoid nil pointer in startTCPProtocol
	server.ctx, server.cancel = context.WithCancel(context.Background())

	// Create a mock connection handler
	mockHandler := protocolMocks.NewMockConnectionHandler(t)
	serverFactory := func() protocol.ConnectionHandler {
		return mockHandler
	}

	protocolName := "test"
	port := 0

	err := server.startTCPProtocol(protocolName, port, serverFactory)

	assert.NoError(t, err)
	assert.Contains(t, server.listeners, protocolName)
	assert.Contains(t, server.servers, protocolName)

	// Clean up
	if listener, exists := server.listeners[protocolName]; exists {
		err := listener.Close()
		require.NoError(t, err)
	}

	// Clean up context
	server.cancel()
}

func TestDefaultServer_startTCPProtocol_PortInUse(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	// Start a listener on a specific port first
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func(listener net.Listener) {
		err := listener.Close()
		require.NoError(t, err)
	}(listener)

	port := listener.Addr().(*net.TCPAddr).Port

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols:     map[string]interface{}{},
		logger:        logger,
		listeners:     make(map[string]net.Listener),
		servers:       make(map[string]interface{}),
	}

	// Initialize context to avoid nil pointer in startTCPProtocol
	server.ctx, server.cancel = context.WithCancel(context.Background())

	mockHandler := protocolMocks.NewMockConnectionHandler(t)
	serverFactory := func() protocol.ConnectionHandler {
		return mockHandler
	}

	protocolName := "test"
	err = server.startTCPProtocol(protocolName, port, serverFactory)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to listen")

	// Clean up context
	server.cancel()
}

func TestDefaultServer_startDHT(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols:     map[string]interface{}{},
		logger:        logger,
		listeners:     make(map[string]net.Listener),
		servers:       make(map[string]interface{}),
	}

	// Initialize context to avoid nil pointer
	server.ctx, server.cancel = context.WithCancel(context.Background())

	config := &DHTConfig{Port: 0}
	err := server.startDHT(config)

	require.NoError(t, err)
	assert.Contains(t, server.servers, ProtocolDHT)

	// Verify the server is a DHT node
	dhtNode, ok := server.servers[ProtocolDHT].(protocol.DHTNode)
	require.True(t, ok)
	assert.NotNil(t, dhtNode)

	// Clean up
	if dhtNode != nil {
		dhtNode.Shutdown()
	}
	server.cancel()
}

func TestDefaultServer_ConcurrentStartStop(t *testing.T) {
	mockStorage := storageMocks.NewMockBlobStore(t)
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockAccessControl := storageMocks.NewMockAccessControl(t)
	logger := zaptest.NewLogger(t)

	server := &DefaultServer{
		storage:       mockStorage,
		acquirer:      mockAcquirer,
		accessControl: mockAccessControl,
		protocols: map[string]interface{}{
			ProtocolPeer: &PeerConfig{Port: 0},
		},
		logger: logger,
	}

	var wg sync.WaitGroup
	errors := make(chan error, 2)

	// Start server in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx := context.Background()
		err := server.Start(ctx)
		if err != nil {
			errors <- err
		}
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Stop server in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := server.Stop(context.Background())
		if err != nil {
			errors <- err
		}
	}()

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent start/stop error: %v", err)
	}
}

func TestDefaultServer_ServerInterfaceImplementation(t *testing.T) {
	// Verify that DefaultServer implements the Server interface
	var _ Server = &DefaultServer{}
}

func TestProtocolConstants(t *testing.T) {
	assert.Equal(t, 5567, DefaultPeerPort)
	assert.Equal(t, 5566, DefaultReflectorPort)
	assert.Equal(t, 4444, DefaultDHTPort)
	assert.Equal(t, "peer", ProtocolPeer)
	assert.Equal(t, "reflector", ProtocolReflector)
	assert.Equal(t, "dht", ProtocolDHT)
}
