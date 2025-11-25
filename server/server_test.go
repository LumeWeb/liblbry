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
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
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

// runConcurrentTest executes a test function concurrently and collects any errors.
// This helper reduces duplication across concurrent test patterns.
func runConcurrentTest(t *testing.T, numGoroutines int, testFunc func(int) error) {
	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if err := testFunc(index); err != nil {
				errChan <- err
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent test error: %v", err)
	}
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
		peerPort := lbryTesting.GetFreePort(t)
		dhtPort := lbryTesting.GetFreePort(t)
		builder := NewServerBuilder().
			WithPeer(peerPort).
			WithDHT(dhtPort).
			WithStorage(builderMocks.storage).
			WithAcquirer(builderMocks.acquirer)

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		dhtConfig := defaultServer.config[ProtocolDHT].(*DHTConfig)

		assert.Equal(t, peerPort, peerConfig.Port, "Peer port should be configured")
		assert.Equal(t, 0, peerConfig.FixedPort, "Fixed port should be 0 (disabled)")
		assert.Equal(t, dhtPort, dhtConfig.Port, "DHT port should be configured")

		// The DHT announcement logic should align peer port with DHT port when no fixed port
		// This is tested indirectly through the startDHT logic
	})

	t.Run("Fixed port takes precedence", func(t *testing.T) {
		// When fixed port is specified, it should be used for DHT announcements
		peerPort := lbryTesting.GetFreePort(t)
		fixedPort := lbryTesting.GetFreePort(t)
		dhtPort := lbryTesting.GetFreePort(t)
		builder := NewServerBuilder().
			WithPeer(peerPort).
			WithFixedPeerPort(fixedPort).
			WithDHT(dhtPort).
			WithStorage(builderMocks.storage).
			WithAcquirer(builderMocks.acquirer)

		server, err := builder.Build()
		require.NoError(t, err)

		defaultServer := server.(*DefaultServer)
		peerConfig := defaultServer.config[ProtocolPeer].(*PeerConfig)
		dhtConfig := defaultServer.config[ProtocolDHT].(*DHTConfig)

		assert.Equal(t, peerPort, peerConfig.Port, "Peer port should be configured")
		assert.Equal(t, fixedPort, peerConfig.FixedPort, "Fixed port should be set")
		assert.Equal(t, dhtPort, dhtConfig.Port, "DHT port should be configured")
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

	// Pre-register mock expectations to avoid concurrent registration fragility
	testMocks.storage.EXPECT().PutSD(blobHash, blobData).Times(numGoroutines).Return(nil)

	// Test concurrent AddSDBlob operations using helper
	runConcurrentTest(t, numGoroutines, func(index int) error {
		return server.AddSDBlob(blobHash, blobData)
	})
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

	numGoroutines := 10

	// Pre-register mock expectations for all goroutines
	for i := 0; i < numGoroutines; i++ {
		blobHash := generateTestBlobHash(i)
		blobData := generateSimpleTestBlobData(i)
		testMocks.storage.EXPECT().Put(blobHash, blobData).Return(nil)
	}

	// Test concurrent AddBlob operations using helper
	runConcurrentTest(t, numGoroutines, func(index int) error {
		blobHash := generateTestBlobHash(index)
		blobData := generateSimpleTestBlobData(index)
		return server.AddBlob(blobHash, blobData)
	})
}

// TestDefaultServer_ConcurrentRemoveBlob tests concurrent RemoveBlob operations
// Validates thread safety and concurrent operation handling for blob removal
func TestDefaultServer_ConcurrentRemoveBlob(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	numGoroutines := 10

	// Pre-register mock expectations for all goroutines
	for i := 0; i < numGoroutines; i++ {
		blobHash := generateTestBlobHash(i)
		testMocks.storage.EXPECT().Delete(blobHash).Return(nil)
	}

	// Test concurrent RemoveBlob operations using helper
	runConcurrentTest(t, numGoroutines, func(index int) error {
		blobHash := generateTestBlobHash(index)
		return server.RemoveBlob(blobHash)
	})
}

// TestDefaultServer_ConcurrentBlobOperations tests mixed concurrent blob operations
// Validates thread safety and proper handling of mixed concurrent add/remove operations
func TestDefaultServer_ConcurrentBlobOperations(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	numOperations := 10 // 10 adds + 10 removes = 20 total operations

	// Pre-register mock expectations for all add operations
	for i := 0; i < numOperations; i++ {
		addBlobHash := generateNamedTestBlobHash("add_hash", i)
		addBlobData := generateTestBlobData("add data", i)
		testMocks.storage.EXPECT().Put(addBlobHash, addBlobData).Return(nil)
	}

	// Pre-register mock expectations for all remove operations
	for i := 0; i < numOperations; i++ {
		removeBlobHash := generateNamedTestBlobHash("remove_hash", i)
		testMocks.storage.EXPECT().Delete(removeBlobHash).Return(nil)
	}

	// Test concurrent Add operations using helper
	runConcurrentTest(t, numOperations, func(index int) error {
		blobHash := generateNamedTestBlobHash("add_hash", index)
		blobData := generateTestBlobData("add data", index)
		return server.AddBlob(blobHash, blobData)
	})

	// Test concurrent Remove operations using helper
	runConcurrentTest(t, numOperations, func(index int) error {
		blobHash := generateNamedTestBlobHash("remove_hash", index)
		return server.RemoveBlob(blobHash)
	})
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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	blobHash := TestBlobHash
	expectedData := []byte(TestBlobData)

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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	blobHash := TestBlobHash

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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	blobHash := TestBlobHash
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

	// Start the server to enable blob acquisition
	ctx, cancel := context.WithCancel(context.Background())
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	blobHash := TestBlobHash

	// Cancel the context after starting the server
	cancel()

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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

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

	// Setup mock expectations with explicit call ordering
	// First, acquire the SD blob
	testMocks.acquirer.EXPECT().Acquire(mock.Anything, sdBlobHash).Return(sdBlobData, nil)
	// Check if content blob exists in storage - it doesn't
	testMocks.storage.EXPECT().Has(contentBlobHash).Return(false, nil)
	// Mock the store Name() method call
	testMocks.storage.EXPECT().Name().Return("mock-store")
	// Then, acquire the content blob - use Anything for context to be more flexible
	testMocks.acquirer.EXPECT().Acquire(mock.Anything, contentBlobHash).Return(contentBlobData, nil)
	// Store the acquired content blob
	testMocks.storage.EXPECT().Put(contentBlobHash, contentBlobData).Return(nil)

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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	sdBlobHash := TestBlobHash
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

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	sdBlobHash := TestBlobHash
	invalidJSON := []byte("{ invalid json")

	// Setup mock expectations
	testMocks.acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return(invalidJSON, nil)

	// Test invalid JSON handling
	result, err := server.AcquireSDBlob(ctx, sdBlobHash)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SD blob")
	assert.Contains(t, err.Error(), sdBlobHash)
	assert.Nil(t, result)
}

// TestDefaultServer_AcquireSDBlob_StorageHit tests recursive SD blob acquisition when content blobs exist in storage
// Validates that existing blobs are retrieved from storage without calling the acquirer
func TestDefaultServer_AcquireSDBlob_StorageHit(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	sdBlobHash := TestBlobHash
	contentBlobHash := "e28ce752b41c8434050f1f5181c8781ac817c975afc918b73eb0b3d8a90d0a06161f53048153b2c2b1029a4007477c26"
	contentBlobData := []byte("existing content blob data")
	sdBlobData := []byte(fmt.Sprintf(`{
		"blobs": [
			{"length": %d, "blob_num": 0, "blob_hash": "%s", "iv": "1234567890123456"},
			{"length": 0, "blob_num": 1, "blob_hash": "", "iv": ""}
		],
		"stream_type": "lbryfile",
		"stream_hash": "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"
	}`, len(contentBlobData), contentBlobHash))

	expectedStreamHash := "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"

	// Setup mock expectations - SD blob acquisition, then storage hit for content blob
	testMocks.acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return(sdBlobData, nil)
	// Content blob exists in storage
	testMocks.storage.EXPECT().Has(contentBlobHash).Return(true, nil)
	// Retrieve from storage (acquirer should NOT be called for content blob)
	testMocks.storage.EXPECT().Get(contentBlobHash).Return(contentBlobData, nil)

	// Test successful recursive SD blob acquisition with storage hit
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

// TestDefaultServer_AcquireSDBlob_ContentBlobAcquisitionFailure tests recursive SD blob acquisition when content blob acquisition fails
// Validates that content blob acquisition failures are properly wrapped and propagated
func TestDefaultServer_AcquireSDBlob_ContentBlobAcquisitionFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Start the server to enable blob acquisition
	ctx := context.Background()
	err := server.Start(ctx)
	require.NoError(t, err)
	defer server.Stop(context.Background())

	sdBlobHash := TestBlobHash
	contentBlobHash := "e28ce752b41c8434050f1f5181c8781ac817c975afc918b73eb0b3d8a90d0a06161f53048153b2c2b1029a4007477c26"
	sdBlobData := []byte(fmt.Sprintf(`{
		"blobs": [
			{"length": 100, "blob_num": 0, "blob_hash": "%s", "iv": "1234567890123456"},
			{"length": 0, "blob_num": 1, "blob_hash": "", "iv": ""}
		],
		"stream_type": "lbryfile",
		"stream_hash": "38b060a751ac96384cd9327eb1b1e36a21fdb71114be07434c0cc7bf63f6e1da274edebfe76f65fbd51ad2f14898b95b"
	}`, contentBlobHash))

	acquirerError := errors.New("content blob acquisition failed")

	// Setup mock expectations - SD blob acquisition succeeds, but content blob acquisition fails
	testMocks.acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return(sdBlobData, nil)
	// Content blob doesn't exist in storage
	testMocks.storage.EXPECT().Has(contentBlobHash).Return(false, nil)
	// Content blob acquisition fails
	testMocks.acquirer.EXPECT().Acquire(mock.Anything, contentBlobHash).Return(nil, acquirerError)

	// Test content blob acquisition failure
	result, err := server.AcquireSDBlob(ctx, sdBlobHash, WithAcquireRecursive(true))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to acquire content blob")
	assert.Contains(t, err.Error(), contentBlobHash)
	assert.Contains(t, err.Error(), "index 0")
	assert.Nil(t, result)
}

// TestDefaultServer_GetBlob_Success tests successful blob retrieval from storage
// Validates that blobs can be successfully retrieved directly from the underlying store
func TestDefaultServer_GetBlob_Success(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	expectedData := []byte(TestBlobData)

	// Setup mock expectations
	testMocks.storage.EXPECT().Get(blobHash).Return(expectedData, nil)

	// Test successful blob retrieval
	result, err := server.GetBlob(blobHash)
	assert.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// TestDefaultServer_GetBlob_EmptyHash tests error handling for empty hash
// Validates that blob retrieval properly rejects empty hash values
func TestDefaultServer_GetBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Test with empty hash
	result, err := server.GetBlob("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash cannot be empty")
	assert.Nil(t, result)
}

// TestDefaultServer_GetBlob_StorageFailure tests error handling when storage.Get fails
// Validates that blob retrieval properly propagates storage errors
func TestDefaultServer_GetBlob_StorageFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	blobHash := TestBlobHash
	storageError := errors.New("storage error")

	// Setup mock expectations
	testMocks.storage.EXPECT().Get(blobHash).Return(nil, storageError)

	// Test storage failure
	result, err := server.GetBlob(blobHash)
	assert.Error(t, err)
	assert.Equal(t, storageError, err)
	assert.Nil(t, result)
}

// TestDefaultServer_GetSDBlob_Success tests successful SD blob retrieval from storage
// Validates that SD blobs can be successfully retrieved directly from the underlying store
func TestDefaultServer_GetSDBlob_Success(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	sdBlobHash := TestBlobHash
	expectedData := []byte(TestBlobData)

	// Setup mock expectations
	testMocks.storage.EXPECT().Get(sdBlobHash).Return(expectedData, nil)

	// Test successful SD blob retrieval
	result, err := server.GetSDBlob(sdBlobHash)
	assert.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// TestDefaultServer_GetSDBlob_EmptyHash tests error handling for empty hash
// Validates that SD blob retrieval properly rejects empty hash values
func TestDefaultServer_GetSDBlob_EmptyHash(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	// Test with empty hash
	result, err := server.GetSDBlob("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hash cannot be empty")
	assert.Nil(t, result)
}

// TestDefaultServer_GetSDBlob_StorageFailure tests error handling when storage.Get fails
// Validates that SD blob retrieval properly propagates storage errors
func TestDefaultServer_GetSDBlob_StorageFailure(t *testing.T) {
	t.Parallel()

	testMocks := setupMocks(t)
	server := setupServer(t, testMocks, map[string]any{})

	sdBlobHash := TestBlobHash
	storageError := errors.New("storage error")

	// Setup mock expectations
	testMocks.storage.EXPECT().Get(sdBlobHash).Return(nil, storageError)

	// Test storage failure
	result, err := server.GetSDBlob(sdBlobHash)
	assert.Error(t, err)
	assert.Equal(t, storageError, err)
	assert.Nil(t, result)
}

// TestExtractDHTHost tests the host extraction functionality
func TestExtractDHTHost(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		address  string
		expected string
	}{
		// Valid addresses that should work with SplitHostPort
		{
			name:     "valid IPv4 with port",
			address:  "192.168.1.1:4444",
			expected: "192.168.1.1",
		},
		{
			name:     "valid IPv6 with brackets and port",
			address:  "[::1]:4444",
			expected: "::1",
		},
		{
			name:     "valid IPv6 with zone and port",
			address:  "[fe80::1%eth0]:4444",
			expected: "fe80::1%eth0",
		},
		{
			name:     "valid hostname with port",
			address:  "example.com:4444",
			expected: "example.com",
		},
		{
			name:     "valid localhost with port",
			address:  "localhost:4444",
			expected: "localhost",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDHTHost(tt.address, logger)
			assert.Equal(t, tt.expected, result, "Host extraction failed for address: %s", tt.address)
		})
	}
}

// TestExtractDHTHostInvalidAddresses tests that invalid addresses return empty string
func TestExtractDHTHostInvalidAddresses(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name    string
		address string
	}{
		{
			name:    "malformed IPv6 without brackets",
			address: "::1:4444",
		},
		{
			name:    "malformed IPv6 with incomplete brackets",
			address: "[::1:4444",
		},
		{
			name:    "IPv6 address without port",
			address: "::1",
		},
		{
			name:    "IPv4 address without port",
			address: "192.168.1.1",
		},
		{
			name:    "hostname without port",
			address: "example.com",
		},
		{
			name:    "localhost without port",
			address: "localhost",
		},
		{
			name:    "complex IPv6 without brackets",
			address: "2001:db8::1:4444",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDHTHost(tt.address, logger)
			assert.Equal(t, "", result, "Expected empty string for invalid address: %s", tt.address)
		})
	}
}
