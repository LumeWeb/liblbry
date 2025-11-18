package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/mocks"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	storageMocks "go.lumeweb.com/liblbry/storage/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Test constants for blob data
// These constants provide standardized test data for consistent testing across multiple test functions
const (
	// TestBlobHash is a standard test blob hash used across multiple tests
	TestBlobHash = "68c0ff52fca66bc20c736e49967760d6378ce73aaf4b0a870f1c2142455629ab50dc49dae0b03c56a9bff7f270a2edf3"
	// TestBlobData is standard test blob data
	TestBlobData = "test blob data"
	// TestNotificationHash is the expected notification hash for TestBlobHash
	TestNotificationHash = "bafksamdiyd7vf7fgnpbay43ojglhoygwg6gooovpjmfiody4efbekvrjvninyso24cydyvvjx737e4fc5xzq"
)

// generateTestBlobHash generates a consistent test blob hash for a given index.
// This ensures predictable test data generation for consistent testing.
func generateTestBlobHash(index int) string {
	return fmt.Sprintf("68c0ff52fca66bc20c736e49967760d6378ce73aaf4b0a870f1c2142455629ab50dc49dae0b03c56a9bff7f270a2ed%02x", index)
}

// generateTestBlobData generates test blob data with the specified prefix and index.
// This ensures predictable test data generation for consistent testing.
func generateTestBlobData(prefix string, index int) []byte {
	return []byte(fmt.Sprintf("%s %d", prefix, index))
}

// generateSimpleTestBlobData generates simple test blob data with a standard prefix and index.
// This ensures predictable test data generation for consistent testing.
func generateSimpleTestBlobData(index int) []byte {
	return generateTestBlobData("test blob data", index)
}

// generateNamedTestBlobHash generates a named test blob hash with the specified name and index.
// This ensures predictable test data generation for consistent testing.
func generateNamedTestBlobHash(name string, index int) string {
	return fmt.Sprintf("%s_%02x", name, index)
}

// testMocks holds all common mocks used in tests
type testMocks struct {
	storage       *storageMocks.MockBlobStore
	acquirer      *mocks.MockBlobAcquirer
	accessControl *storageMocks.MockAccessControl
	logger        *zap.Logger
}

// setupMocks initializes all standard mocks for testing
func setupMocks(t *testing.T) *testMocks {
	return &testMocks{
		storage:       storageMocks.NewMockBlobStore(t),
		acquirer:      mocks.NewMockBlobAcquirer(t),
		accessControl: storageMocks.NewMockAccessControl(t),
		logger:        zaptest.NewLogger(t),
	}
}

// setupServer creates a server instance with the provided mocks and config
func setupServer(t *testing.T, testMocks *testMocks, config map[string]any) *DefaultServer {
	server := &DefaultServer{
		storage:       testMocks.storage,
		acquirer:      testMocks.acquirer,
		accessControl: testMocks.accessControl,
		config:        config,
		logger:        testMocks.logger,
		listeners:     make(map[string]net.Listener),
		servers:       make(map[string]any),
	}
	// Initialize context to avoid nil pointer
	server.ctx, server.cancel = context.WithCancel(context.Background())

	// Register cleanup function to be called automatically when test completes
	t.Cleanup(func() {
		cleanupServer(server)
	})

	return server
}

// setupServerWithDefaults creates a server with common default config
func setupServerWithDefaults(t *testing.T, testMocks *testMocks) *DefaultServer {
	return setupServer(t, testMocks, map[string]any{
		ProtocolPeer: &PeerConfig{Port: 0},
	})
}

// cleanupServer handles common server cleanup patterns
func cleanupServer(server *DefaultServer) {
	if server.cancel != nil {
		server.cancel()
	}
}

func TestDefaultServer_Start_Success(t *testing.T) {
	// Test that server starts successfully with minimal configuration
	// This validates the core startup functionality works correctly
	testMocks := setupMocks(t)
	server := setupServerWithDefaults(t, testMocks)

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.NotNil(t, server.ctx)
	assert.NotNil(t, server.cancel)
	assert.NotNil(t, server.listeners)
	assert.NotNil(t, server.servers)
	assert.Contains(t, server.listeners, ProtocolPeer)
}

func TestDefaultServer_Start_MultipleProtocols(t *testing.T) {
	// Test server startup with multiple config enabled
	// Validates that the server can handle multiple concurrent config
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{
		ProtocolPeer:      &PeerConfig{Port: 0},
		ProtocolReflector: &ReflectorConfig{Port: 0},
	})

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolPeer)
	assert.Contains(t, server.listeners, ProtocolReflector)
}

func TestDefaultServer_Start_WithDHT(t *testing.T) {
	// Tests DHT protocol initialization and blob announcement functionality
	// Validates that server correctly sets up DHT node and announces blobs
	testMocks := setupMocks(t)

	// Setup mock expectations for List method (called during DHT blob announcement)
	// When storage is empty, List(0, batchSize) returns empty slice and loop breaks
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil)

	// Create a mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)

	// Setup mock expectations for DHT node methods
	mockDHTNode.EXPECT().Start().Return(nil)

	server := setupServer(t, testMocks, map[string]any{
		ProtocolDHT: mockDHTNode, // Pass the mock DHT node directly
	})
	server.dhtBatchSize = DefaultDHTAnnouncementBatchSize

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.Contains(t, server.servers, ProtocolDHT)

	// Verify that the mock DHT node was actually used
	dhtNode, ok := server.servers[ProtocolDHT].(protocol.DHTNode)
	assert.True(t, ok)
	assert.Equal(t, mockDHTNode, dhtNode)
}

func TestDefaultServer_Start_AllProtocols(t *testing.T) {
	// Tests server startup with all supported config enabled
	// Validates comprehensive config initialization and resource management
	testMocks := setupMocks(t)

	// Setup mock expectations for List method (called during DHT blob announcement)
	// When storage is empty, List(0, batchSize) returns empty slice and loop breaks
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil)

	// Create a mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)

	// Setup mock expectations for DHT node methods
	mockDHTNode.EXPECT().Start().Return(nil)

	server := setupServer(t, testMocks, map[string]any{
		ProtocolPeer:      &PeerConfig{Port: 0},
		ProtocolReflector: &ReflectorConfig{Port: 0},
		ProtocolDHT:       mockDHTNode, // Pass the mock DHT node directly
	})
	server.dhtBatchSize = DefaultDHTAnnouncementBatchSize

	ctx := context.Background()
	err := server.Start(ctx)

	require.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolPeer)
	assert.Contains(t, server.listeners, ProtocolReflector)
	assert.Contains(t, server.servers, ProtocolDHT)

	// Verify that the mock DHT node was actually used
	dhtNode, ok := server.servers[ProtocolDHT].(protocol.DHTNode)
	assert.True(t, ok)
	assert.Equal(t, mockDHTNode, dhtNode)
}

func TestDefaultServer_Start_UnknownProtocol(t *testing.T) {
	// Tests server startup with unknown protocol configuration
	// Validates proper error handling for unsupported protocol types
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{
		"unknown": &PeerConfig{Port: 0},
	})

	ctx := context.Background()
	err := server.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown protocol: unknown")
}

func TestDefaultServer_Start_PortAlreadyInUse(t *testing.T) {
	// Test server startup with port conflict scenario
	// Validates proper error handling when attempting to bind to an in-use port
	testMocks := setupMocks(t)

	// Start a listener on a specific port first
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func(listener net.Listener) {
		err := listener.Close()
		require.NoError(t, err)
	}(listener)

	port := listener.Addr().(*net.TCPAddr).Port

	server := setupServer(t, testMocks, map[string]any{
		ProtocolPeer: &PeerConfig{Port: port},
	})

	ctx := context.Background()
	err = server.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start Peer protocol")
}

func TestDefaultServer_Stop_Success(t *testing.T) {
	// Test successful server shutdown
	// Validates proper cleanup and resource release during server stop
	testMocks := setupMocks(t)
	server := setupServerWithDefaults(t, testMocks)

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
	// Test server shutdown with timeout constraint
	// Validates graceful handling of timeout scenarios during server shutdown
	testMocks := setupMocks(t)
	server := setupServerWithDefaults(t, testMocks)

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
	// Test server stop when server hasn't been started
	// Validates robustness against invalid state transitions
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Stop without starting should not panic
	err := server.Stop(context.Background())
	assert.NoError(t, err)
}

func TestDefaultServer_startPeer(t *testing.T) {
	// Test peer protocol initialization
	// Validates that peer protocol can be successfully started and registered
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	config := &PeerConfig{Port: 0}
	err := server.startPeer(config)

	assert.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolPeer)
	assert.Contains(t, server.servers, ProtocolPeer)
}

func TestDefaultServer_startReflector(t *testing.T) {
	// Test reflector protocol initialization
	// Validates that reflector protocol can be successfully started and registered
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	config := &ReflectorConfig{Port: 0}
	err := server.startReflector(config)

	assert.NoError(t, err)
	assert.Contains(t, server.listeners, ProtocolReflector)
	assert.Contains(t, server.servers, ProtocolReflector)
}

func TestDefaultServer_startTCPProtocol(t *testing.T) {
	// Test generic TCP protocol initialization
	// Validates that TCP-based config can be successfully started and registered
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

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
}

func TestDefaultServer_startTCPProtocol_PortInUse(t *testing.T) {
	// Test TCP protocol initialization with port conflict
	// Validates proper error handling when attempting to bind to an in-use port
	testMocks := setupMocks(t)

	// Start a listener on a specific port first
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func(listener net.Listener) {
		err := listener.Close()
		require.NoError(t, err)
	}(listener)

	port := listener.Addr().(*net.TCPAddr).Port

	server := setupServer(t, testMocks, map[string]any{})

	mockHandler := protocolMocks.NewMockConnectionHandler(t)
	serverFactory := func() protocol.ConnectionHandler {
		return mockHandler
	}

	protocolName := "test"
	err = server.startTCPProtocol(protocolName, port, serverFactory)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to listen")
}

func TestDefaultServer_startDHT(t *testing.T) {
	// Test DHT protocol initialization
	// Validates that startDHT properly handles DHT node setup
	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Create a mock DHT node to simulate an existing node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)

	// Setup mock expectations for DHT node start
	mockDHTNode.EXPECT().Start().Return(nil)

	// Simulate that the DHT node is already in the servers map (as would happen with WithExistingDHT)
	server.servers[ProtocolDHT] = mockDHTNode

	// Test that startDHT properly detects and uses the existing DHT node
	// This simulates the path taken when WithExistingDHT is used
	config := &DHTConfig{Port: DefaultDHTPort}

	// This should not fail and should properly handle the existing node
	err := server.startDHT(config)

	// The method should not fail even with existing node
	assert.NoError(t, err)

	// Verify that the existing DHT node is still in place
	dhtNode, exists := server.servers[ProtocolDHT]
	assert.True(t, exists, "DHT node should still be in servers map")

	if dhtNode != nil {
		// Should be the same mock node we set up
		assert.Equal(t, mockDHTNode, dhtNode, "Should still have the existing DHT node")
	}
}

func TestDefaultServer_ConcurrentStartStop(t *testing.T) {
	// Test concurrent server start and stop operations
	// Validates thread safety and proper handling of concurrent operations
	testMocks := setupMocks(t)
	server := setupServerWithDefaults(t, testMocks)

	var wg sync.WaitGroup
	errs := make(chan error, 2)

	// Start server in goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx := context.Background()
		err := server.Start(ctx)
		if err != nil {
			errs <- err
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
			errs <- err
		}
	}()

	wg.Wait()
	close(errs)

	// Check for any errors
	for err := range errs {
		t.Errorf("Concurrent start/stop error: %v", err)
	}
}

func TestDefaultServer_ServerInterfaceImplementation(t *testing.T) {
	// Verify that DefaultServer implements the Server interface
	// Ensures compatibility with Server interface contract
	var _ Server = &DefaultServer{}
}

func TestProtocolConstants(t *testing.T) {
	// Verify protocol constant values are set correctly
	// Ensures consistent protocol naming and default port assignments
	assert.Equal(t, 5567, DefaultPeerPort)
	assert.Equal(t, 5566, DefaultReflectorPort)
	assert.Equal(t, 4444, DefaultDHTPort)
	assert.Equal(t, "peer", ProtocolPeer)
	assert.Equal(t, "reflector", ProtocolReflector)
	assert.Equal(t, "dht", ProtocolDHT)
}

func TestPeerPortDHTAlignment(t *testing.T) {
	builderMocks := setupBuilderMocks(t)

	t.Run("DHT port alignment when no fixed port", func(t *testing.T) {
		// When no fixed port is specified, DHT should announce peer port = DHT port
		builder := NewServerBuilder().
			WithPeer(testPortPeer).
			WithDHT(testPortDHT).
			WithStorage(builderMocks.storage).
			WithAcquirer(builderMocks.acquirer)

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		dhtConfig := defaultServer.config[ProtocolDHT].(*DHTConfig)

		assert.Equal(t, testPortPeer, peerConfig.Port, "Peer port should be configured")
		assert.Equal(t, 0, peerConfig.FixedPort, "Fixed port should be 0 (disabled)")
		assert.Equal(t, testPortDHT, dhtConfig.Port, "DHT port should be configured")

		// The DHT announcement logic should align peer port with DHT port when no fixed port
		// This is tested indirectly through the startDHT logic
	})

	t.Run("Fixed port takes precedence", func(t *testing.T) {
		// When fixed port is specified, it should be used for DHT announcements
		builder := NewServerBuilder().
			WithPeer(testPortPeer).
			WithFixedPeerPort(testPortPeer2).
			WithDHT(testPortDHT).
			WithStorage(builderMocks.storage).
			WithAcquirer(builderMocks.acquirer)

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		dhtConfig := defaultServer.config[ProtocolDHT].(*DHTConfig)

		assert.Equal(t, testPortPeer, peerConfig.Port, "Peer port should be configured")
		assert.Equal(t, testPortPeer2, peerConfig.FixedPort, "Fixed port should be set")
		assert.Equal(t, testPortDHT, dhtConfig.Port, "DHT port should be configured")
	})
}

// TestDefaultServer_AddBlob_Success tests successful blob addition
// Validates that blobs can be successfully stored in the blob store
func TestDefaultServer_AddBlob_Success(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	blobData := []byte(TestBlobData)

	// Setup mock expectations
	testMocks.storage.EXPECT().Put(blobHash, blobData).Return(nil)

	// Test successful blob addition
	err := server.AddBlob(blobHash, blobData)
	assert.NoError(t, err)
}

// TestDefaultServer_AddBlob_EmptyHash tests error handling for empty hash
// Validates that blob addition properly rejects empty hash values
func TestDefaultServer_AddBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobData := []byte(TestBlobData)

	// Test with empty hash
	err := server.AddBlob("", blobData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash cannot be empty")
}

// TestDefaultServer_AddBlob_EmptyData tests error handling for empty data
// Validates that blob addition properly rejects empty data values
func TestDefaultServer_AddBlob_EmptyData(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash

	// Test with empty data
	err := server.AddBlob(blobHash, []byte{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob data cannot be empty")
}

// TestDefaultServer_AddBlob_StorageFailure tests error handling when storage.Put fails
// Validates that blob addition properly propagates storage errors
func TestDefaultServer_AddBlob_StorageFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	blobData := []byte(TestBlobData)
	storageError := errors.New("storage error")

	// Setup mock expectations
	testMocks.storage.EXPECT().Put(blobHash, blobData).Return(storageError)

	// Test storage failure
	err := server.AddBlob(blobHash, blobData)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to store blob")
	assert.Contains(t, err.Error(), blobHash)
}

// TestDefaultServer_AddBlob_WithNotifier tests that notification is sent
// Validates that blob addition properly triggers notifications when notifier is configured
func TestDefaultServer_AddBlob_WithNotifier(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	mockNotifier := protocolMocks.NewMockNotifier(t)
	server := setupServer(t, testMocks, map[string]any{})
	server.notifier = mockNotifier

	blobHash := TestBlobHash
	blobData := []byte(TestBlobData)

	// Setup mock expectations
	testMocks.storage.EXPECT().Put(blobHash, blobData).Return(nil)
	mockNotifier.EXPECT().Notify(protocol.NOTIFY_BLOB_ADDED, TestNotificationHash).Return(nil)

	// Test blob addition with notification
	err := server.AddBlob(blobHash, blobData)
	assert.NoError(t, err)
}

// TestDefaultServer_AddSDBlob tests SD blob addition with various scenarios
// Validates that SD blobs can be successfully stored and handles error cases properly
func TestDefaultServer_AddSDBlob(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		hash           string
		data           []byte
		setupMocks     func(*testMocks, *DefaultServer)
		expectErr      bool
		expectedErrMsg string
		description    string
	}{
		{
			name: "success",
			hash: TestBlobHash,
			data: []byte(TestBlobData),
			setupMocks: func(m *testMocks, s *DefaultServer) {
				m.storage.EXPECT().PutSD(TestBlobHash, []byte(TestBlobData)).Return(nil)
			},
			expectErr:   false,
			description: "Successfully stores SD blob",
		},
		{
			name: "empty hash",
			hash: "",
			data: []byte(TestBlobData),
			setupMocks: func(m *testMocks, s *DefaultServer) {
				// No storage mock needed since validation fails first
			},
			expectErr:      true,
			expectedErrMsg: "hash cannot be empty",
			description:    "Rejects empty hash",
		},
		{
			name: "empty data",
			hash: TestBlobHash,
			data: []byte{},
			setupMocks: func(m *testMocks, s *DefaultServer) {
				// No storage mock needed since validation fails first
			},
			expectErr:      true,
			expectedErrMsg: "blob data cannot be empty",
			description:    "Rejects empty data",
		},
		{
			name: "storage failure",
			hash: TestBlobHash,
			data: []byte(TestBlobData),
			setupMocks: func(m *testMocks, s *DefaultServer) {
				storageError := errors.New("storage error")
				m.storage.EXPECT().PutSD(TestBlobHash, []byte(TestBlobData)).Return(storageError)
			},
			expectErr:      true,
			expectedErrMsg: "failed to store SD blob",
			description:    "Propagates storage errors",
		},
		{
			name: "with notifier",
			hash: TestBlobHash,
			data: []byte(TestBlobData),
			setupMocks: func(m *testMocks, s *DefaultServer) {
				mockNotifier := protocolMocks.NewMockNotifier(t)
				s.notifier = mockNotifier
				m.storage.EXPECT().PutSD(TestBlobHash, []byte(TestBlobData)).Return(nil)
				mockNotifier.EXPECT().Notify(protocol.NOTIFY_BLOB_ADDED, TestNotificationHash).Return(nil)
			},
			expectErr:   false,
			description: "Sends notification when notifier is configured",
		},
	}

	for _, tc := range testCases {
		tc := tc // Capture loop variable to avoid race condition
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			testMocks := setupMocks(t)
			server := setupServer(t, testMocks, map[string]any{})

			// Setup test-specific mocks
			tc.setupMocks(testMocks, server)

			err := server.AddSDBlob(tc.hash, tc.data)

			if tc.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErrMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDefaultServer_ConcurrentAddSDBlob tests concurrent AddSDBlob operations
// Validates thread safety and concurrent operation handling for SD blob addition
func TestDefaultServer_ConcurrentAddSDBlob(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	blobData := []byte(TestBlobData)
	numGoroutines := 10
	errChan := make(chan error, numGoroutines)

	var wg sync.WaitGroup

	// Test concurrent AddSDBlob operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			// Setup mock expectations for each goroutine
			testMocks.storage.EXPECT().PutSD(blobHash, blobData).Return(nil)

			err := server.AddSDBlob(blobHash, blobData)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent AddSDBlob error: %v", err)
	}
}

// TestDefaultServer_RemoveBlob_Success tests successful blob removal
// Validates that blobs can be successfully removed from the blob store
func TestDefaultServer_RemoveBlob_Success(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash

	// Setup mock expectations
	testMocks.storage.EXPECT().Delete(blobHash).Return(nil)

	// Test successful blob removal
	err := server.RemoveBlob(blobHash)
	assert.NoError(t, err)
}

// TestDefaultServer_RemoveBlob_EmptyHash tests error handling for empty hash
// Validates that blob removal properly rejects empty hash values
func TestDefaultServer_RemoveBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Test with empty hash
	err := server.RemoveBlob("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash cannot be empty")
}

// TestDefaultServer_RemoveBlob_StorageFailure tests error handling when storage.Delete fails
// Validates that blob removal properly propagates storage errors
func TestDefaultServer_RemoveBlob_StorageFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	storageError := errors.New("storage error")

	// Setup mock expectations
	testMocks.storage.EXPECT().Delete(blobHash).Return(storageError)

	// Test storage failure
	err := server.RemoveBlob(blobHash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete blob")
	assert.Contains(t, err.Error(), blobHash)
}

// TestDefaultServer_RemoveBlob_WithNotifier tests that notification is sent
// Validates that blob removal properly triggers notifications when notifier is configured
func TestDefaultServer_RemoveBlob_WithNotifier(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	mockNotifier := protocolMocks.NewMockNotifier(t)
	server := setupServer(t, testMocks, map[string]any{})
	server.notifier = mockNotifier

	blobHash := TestBlobHash

	// Setup mock expectations
	testMocks.storage.EXPECT().Delete(blobHash).Return(nil)
	mockNotifier.EXPECT().Notify(protocol.NOTIFY_BLOB_REMOVED, TestNotificationHash).Return(nil)

	// Test blob removal with notification
	err := server.RemoveBlob(blobHash)
	assert.NoError(t, err)
}

// TestDefaultServer_BlobManagerInterface verifies DefaultServer implements BlobManager
// Validates that DefaultServer properly implements the BlobManager interface contract
func TestDefaultServer_BlobManagerInterface(t *testing.T) {
	t.Parallel()

	// Verify that DefaultServer implements the BlobManager interface
	var _ BlobManager = &DefaultServer{}
}

// TestDefaultServer_ConcurrentAddBlob tests concurrent AddBlob operations
// Validates thread safety and concurrent operation handling for blob addition
func TestDefaultServer_ConcurrentAddBlob(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	var wg sync.WaitGroup
	numGoroutines := 10
	errChan := make(chan error, numGoroutines)

	// Test concurrent AddBlob operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			blobHash := generateTestBlobHash(index)
			blobData := generateSimpleTestBlobData(index)

			// Setup mock expectations for each goroutine
			testMocks.storage.EXPECT().Put(blobHash, blobData).Return(nil)

			err := server.AddBlob(blobHash, blobData)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent AddBlob error: %v", err)
	}
}

// TestDefaultServer_ConcurrentRemoveBlob tests concurrent RemoveBlob operations
// Validates thread safety and concurrent operation handling for blob removal
func TestDefaultServer_ConcurrentRemoveBlob(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	var wg sync.WaitGroup
	numGoroutines := 10
	errChan := make(chan error, numGoroutines)

	// Test concurrent RemoveBlob operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			blobHash := generateTestBlobHash(index)

			// Setup mock expectations for each goroutine
			testMocks.storage.EXPECT().Delete(blobHash).Return(nil)

			err := server.RemoveBlob(blobHash)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent RemoveBlob error: %v", err)
	}
}

// TestDefaultServer_ConcurrentBlobOperations tests mixed concurrent blob operations
// Validates thread safety and proper handling of mixed concurrent add/remove operations
func TestDefaultServer_ConcurrentBlobOperations(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	var wg sync.WaitGroup
	numOperations := 20 // 10 adds + 10 removes
	errChan := make(chan error, numOperations)

	// Test concurrent Add and Remove operations
	for i := 0; i < 10; i++ {
		// Add operation
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			blobHash := generateNamedTestBlobHash("add_hash", index)
			blobData := generateTestBlobData("add data", index)

			testMocks.storage.EXPECT().Put(blobHash, blobData).Return(nil)

			err := server.AddBlob(blobHash, blobData)
			if err != nil {
				errChan <- err
			}
		}(i)

		// Remove operation
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			blobHash := generateNamedTestBlobHash("remove_hash", index)

			testMocks.storage.EXPECT().Delete(blobHash).Return(nil)

			err := server.RemoveBlob(blobHash)
			if err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent blob operation error: %v", err)
	}
}

// TestDefaultServer_announceBlobsToDHT_UsesLBRYHash tests that DHT blob announcements
// use LBRY hash format directly, not multihash format. This is a regression test
// for a bug where LBRY hashes were incorrectly converted to multihash before
// being passed to the DHT announcer, causing "invalid hash format" errors.
func TestDefaultServer_announceBlobsToDHT_UsesLBRYHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Create a mock DHT announcer to capture the hash format
	mockDHTAnnouncer := protocolMocks.NewMockDHTAnnouncer(t)
	server.dhtAnnouncer = mockDHTAnnouncer

	// Test blob hashes (valid LBRY SHA-384 hashes)
	testBlobHashes := []string{
		"68c0ff52fca66bc20c736e49967760d6378ce73aaf4b0a870f1c2142455629ab50dc49dae0b03c56a9bff7f270a2edf3",
		"e28ce752b41c8434050f1f5181c8781ac817c975afc918b73eb0b3d8a90d0a06161f53048153b2c2b1029a4007477c26",
		"38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b",
	}

	// Set up mock expectations for storage List calls
	// Note: offset increments by batchSize, not by actual number of blobs processed
	testMocks.storage.EXPECT().List(0, DefaultDHTAnnouncementBatchSize).Return(testBlobHashes, nil)
	testMocks.storage.EXPECT().List(DefaultDHTAnnouncementBatchSize, DefaultDHTAnnouncementBatchSize).Return([]string{}, nil)

	// Set up mock expectations for DHT announcer - expect LBRY hashes, not multihashes
	for _, hash := range testBlobHashes {
		mockDHTAnnouncer.EXPECT().AnnounceBlob(hash).Return(nil)
	}

	// Set batch size to ensure all blobs are processed in one batch
	server.dhtBatchSize = DefaultDHTAnnouncementBatchSize

	// Call announceBlobsToDHT with default worker count and batch size
	server.announceBlobsToDHT(DefaultDHTAnnouncerWorkers, DefaultDHTAnnouncementBatchSize)

	// Verify all mock expectations were met
	// This ensures that LBRY hashes were passed directly to AnnounceBlob,
	// not converted to multihash format first
}

// TestDefaultServer_AcquireBlob_Success tests successful blob acquisition
// Validates that blobs can be successfully acquired using the configured acquirer
func TestDefaultServer_AcquireBlob_Success(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	expectedData := []byte(TestBlobData)
	ctx := context.Background()

	// Setup mock expectations
	testMocks.acquirer.EXPECT().Acquire(ctx, blobHash).Return(expectedData, nil)

	// Test successful blob acquisition
	result, err := server.AcquireBlob(ctx, blobHash)
	assert.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// TestDefaultServer_AcquireBlob_EmptyHash tests error handling for empty hash
// Validates that blob acquisition properly rejects empty hash values
func TestDefaultServer_AcquireBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	ctx := context.Background()

	// Test with empty hash
	result, err := server.AcquireBlob(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash cannot be empty")
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireBlob_NoAcquirer tests error handling when no acquirer is configured
// Validates that blob acquisition properly handles missing acquirer configuration
func TestDefaultServer_AcquireBlob_NoAcquirer(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})
	server.acquirer = nil // Explicitly set acquirer to nil

	blobHash := TestBlobHash
	ctx := context.Background()

	// Test with no acquirer configured
	result, err := server.AcquireBlob(ctx, blobHash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no acquirer configured")
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireBlob_AcquirerFailure tests error handling when acquirer fails
// Validates that blob acquisition properly propagates acquirer errors
func TestDefaultServer_AcquireBlob_AcquirerFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	ctx := context.Background()
	acquirerError := errors.New("acquirer error")

	// Setup mock expectations
	testMocks.acquirer.EXPECT().Acquire(ctx, blobHash).Return(nil, acquirerError)

	// Test acquirer failure
	result, err := server.AcquireBlob(ctx, blobHash)
	assert.Error(t, err)
	assert.Equal(t, acquirerError, err)
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireBlob_ContextCancellation tests context cancellation handling
// Validates that blob acquisition properly respects context cancellation
func TestDefaultServer_AcquireBlob_ContextCancellation(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Setup mock expectations (acquirer is called and should return context.Canceled when given a cancelled context)
	testMocks.acquirer.EXPECT().Acquire(ctx, blobHash).Return(nil, context.Canceled)

	// Test with cancelled context
	result, err := server.AcquireBlob(ctx, blobHash)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireSDBlob_Success tests successful SD blob acquisition (non-recursive)
// Validates that SD blobs can be successfully acquired without fetching content blobs
func TestDefaultServer_AcquireSDBlob_Success(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	sdBlobHash := TestBlobHash
	sdBlobData := []byte(`{
		"blobs": [
			{"length": 100, "blob_num": 0, "blob_hash": "68c0ff52fca66bc20c736e49967760d6378ce73aaf4b0a870f1c2142455629ab50dc49dae0b03c56a9bff7f270a2edf3", "iv": "1234567890123456"},
			{"length": 0, "blob_num": 1, "blob_hash": "", "iv": ""}
		],
		"stream_type": "lbryfile",
		"stream_hash": "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"
	}`)
	expectedStreamHash := "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"
	ctx := context.Background()

	// Setup mock expectations for SD blob acquisition
	testMocks.acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return([]byte(sdBlobData), nil)

	// Test successful SD blob acquisition (non-recursive)
	result, err := server.AcquireSDBlob(ctx, sdBlobHash, WithAcquireRecursive(false))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, sdBlobHash, result.SDBlobHash)
	assert.Equal(t, expectedStreamHash, result.StreamHash)
	assert.NotNil(t, result.SDBlob)
	assert.Equal(t, []byte(sdBlobData), result.SDBlobData)
	assert.Nil(t, result.ContentBlobs)     // Should be nil for non-recursive
	assert.Nil(t, result.ContentHashes)    // Should be nil for non-recursive
	assert.Equal(t, 0, result.TotalChunks) // Should be 0 for non-recursive
}

// TestDefaultServer_AcquireSDBlob_RecursiveSuccess tests successful recursive SD blob acquisition
// Validates that SD blobs can be successfully acquired with all content blobs
func TestDefaultServer_AcquireSDBlob_RecursiveSuccess(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	sdBlobHash := TestBlobHash
	contentBlobHash := "e28ce752b41c8434050f1f5181c8781ac817c975afc918b73eb0b3d8a90d0a06161f53048153b2c2b1029a4007477c26"
	contentBlobData := []byte("content blob data")
	sdBlobData := []byte(fmt.Sprintf(`{
		"blobs": [
			{"length": %d, "blob_num": 0, "blob_hash": "%s", "iv": "1234567890123456"},
			{"length": 0, "blob_num": 1, "blob_hash": "", "iv": ""}
		],
		"stream_type": "lbryfile",
		"stream_hash": "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"
	}`, len(contentBlobData), contentBlobHash))

	expectedStreamHash := "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"
	ctx := context.Background()

	// Setup mock expectations with explicit call ordering
	// First, acquire the SD blob
	testMocks.acquirer.EXPECT().Acquire(mock.Anything, sdBlobHash).Return(sdBlobData, nil)
	// Check if content blob exists in storage - it doesn't
	testMocks.storage.EXPECT().Has(contentBlobHash).Return(false, nil)
	// Then, acquire the content blob - use Anything for context to be more flexible
	testMocks.acquirer.EXPECT().Acquire(mock.Anything, contentBlobHash).Return(contentBlobData, nil)

	// Test successful recursive SD blob acquisition
	result, err := server.AcquireSDBlob(ctx, sdBlobHash, WithAcquireRecursive(true))
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, sdBlobHash, result.SDBlobHash)
	assert.Equal(t, expectedStreamHash, result.StreamHash)
	assert.NotNil(t, result.SDBlob)
	assert.Equal(t, sdBlobData, result.SDBlobData)
	assert.NotNil(t, result.ContentBlobs)
	assert.Equal(t, 1, len(result.ContentBlobs))

	assert.Equal(t, contentBlobData, result.ContentBlobs[0])
	assert.NotNil(t, result.ContentHashes)
	assert.Equal(t, 1, len(result.ContentHashes))
	assert.Equal(t, contentBlobHash, result.ContentHashes[0])
	assert.Equal(t, 1, result.TotalChunks)
	assert.NotNil(t, result.ChunkSizes)
	assert.Equal(t, 1, len(result.ChunkSizes))
	assert.Equal(t, len(contentBlobData), result.ChunkSizes[0])
}

// TestDefaultServer_AcquireSDBlob_EmptyHash tests error handling for empty hash
// Validates that SD blob acquisition properly rejects empty hash values
func TestDefaultServer_AcquireSDBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	ctx := context.Background()

	// Test with empty hash
	result, err := server.AcquireSDBlob(ctx, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash cannot be empty")
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireSDBlob_SDBlobAcquisitionFailure tests error handling when SD blob acquisition fails
// Validates that SD blob acquisition properly propagates acquirer errors for SD blob
func TestDefaultServer_AcquireSDBlob_SDBlobAcquisitionFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	sdBlobHash := TestBlobHash
	ctx := context.Background()
	acquirerError := errors.New("acquirer error")

	// Setup mock expectations
	testMocks.acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return(nil, acquirerError)

	// Test SD blob acquisition failure
	result, err := server.AcquireSDBlob(ctx, sdBlobHash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire SD blob")
	assert.Contains(t, err.Error(), sdBlobHash)
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireSDBlob_InvalidJSON tests error handling for invalid SD blob JSON
// Validates that SD blob acquisition properly handles malformed JSON data
func TestDefaultServer_AcquireSDBlob_InvalidJSON(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	sdBlobHash := TestBlobHash
	invalidJSON := []byte("{ invalid json")
	ctx := context.Background()

	// Setup mock expectations
	testMocks.acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return(invalidJSON, nil)

	// Test invalid JSON handling
	result, err := server.AcquireSDBlob(ctx, sdBlobHash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse SD blob")
	assert.Contains(t, err.Error(), sdBlobHash)
	assert.Nil(t, result)
}
