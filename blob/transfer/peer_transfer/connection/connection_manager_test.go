package connection

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/protocol"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestNewConnectionManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockFactory := mockPeerClientFactory(t)

	t.Run("Valid factory", func(t *testing.T) {
		cm, err := NewConnectionManager(mockFactory, logger)

		assertSuccessfulOperation(t, err, cm)
		assert.NotNil(t, cm.clientFactory)
		assert.Equal(t, logger, cm.logger)
		assert.False(t, cm.IsStopped())
		assert.NotNil(t, cm.clientPool)
	})

	t.Run("Nil factory", func(t *testing.T) {
		cm, err := NewConnectionManager(nil, logger)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "clientFactory cannot be nil")
		assert.Nil(t, cm)
	})
}

func TestConnectionManager_StartStop(t *testing.T) {
	setup := setupTest(t)
	cm := setup.manager

	// Initially not stopped
	assert.False(t, cm.IsStopped())

	// Stop it
	cm.Stop()
	assert.True(t, cm.IsStopped())
	assert.Nil(t, cm.clientPool)

	// Start it again
	cm.Start()
	assert.False(t, cm.IsStopped())
	assert.NotNil(t, cm.clientPool)
}

func TestConnectionManager_GetClient(t *testing.T) {
	setup := setupTest(t)
	cm := setup.manager

	t.Run("Successful get", func(t *testing.T) {
		client, err := cm.GetClient()

		assertSuccessfulOperation(t, err, client)
		assert.Implements(t, (*protocol.PeerClient)(nil), client)
	})

	t.Run("Stopped manager", func(t *testing.T) {
		cm.Stop()

		client, err := cm.GetClient()

		assertStoppedManager(t, cm, err, client)
	})
}

func TestConnectionManager_ReturnClient(t *testing.T) {
	setup := setupTest(t)
	cm := setup.manager

	t.Run("Return valid client", func(t *testing.T) {
		// Create a connection manager with a controlled mock
		mockClient := protocolMocks.NewMockPeerClient(t)
		mockClient.EXPECT().Reset().Return(nil)

		testSetup := setupTestWithMock(t, mockClient)
		testCM := testSetup.manager

		client, err := testCM.GetClient()
		require.NoError(t, err)

		// Return client should not panic
		testCM.ReturnClient(client)
	})

	t.Run("Return nil client", func(t *testing.T) {
		// Should not panic
		cm.ReturnClient(nil)
	})

	t.Run("Return client to stopped manager", func(t *testing.T) {
		client, err := cm.GetClient()
		require.NoError(t, err)

		cm.Stop()
		// Should not panic and should discard client
		cm.ReturnClient(client)
	})
}

func TestConnectionManager_DownloadFromPeer(t *testing.T) {
	setup := setupTest(t)
	cm := setup.manager
	peerAddr := "127.0.0.1:3333"
	hash := "testhash"
	testData := []byte("test data")

	t.Run("Successful download", func(t *testing.T) {
		mockClient := protocolMocks.NewMockPeerClient(t)
		mockClient.EXPECT().Connect(mock.Anything, peerAddr).Return(nil)
		mockClient.EXPECT().GetBlob(mock.Anything, hash).Return(testData, nil)
		mockClient.EXPECT().Reset().Return(nil)

		testSetup := setupTestWithMock(t, mockClient)

		data, err := testSetup.manager.DownloadFromPeer(context.Background(), peerAddr, hash)

		require.NoError(t, err)
		assert.Equal(t, testData, data)
	})

	t.Run("Connection failure", func(t *testing.T) {
		connErr := errors.New("connection failed")
		mockClient := createMockClientForDownload(t, connErr, nil)
		testSetup := setupTestWithMock(t, mockClient)

		data, err := testSetup.manager.DownloadFromPeer(context.Background(), peerAddr, hash)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "connect to peer")
		assert.Nil(t, data)
	})

	t.Run("Download failure", func(t *testing.T) {
		downloadErr := errors.New("download failed")
		mockClient := createMockClientForDownload(t, nil, downloadErr)
		testSetup := setupTestWithMock(t, mockClient)

		data, err := testSetup.manager.DownloadFromPeer(context.Background(), peerAddr, hash)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fetch blob from peer")
		assert.Nil(t, data)
	})

	t.Run("Context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		mockClient := createMockClientForDownload(t, context.Canceled, nil)
		testSetup := setupTestWithMock(t, mockClient)

		data, err := testSetup.manager.DownloadFromPeer(ctx, peerAddr, hash)

		assert.Error(t, err)
		assert.Equal(t, context.Canceled, err)
		assert.Nil(t, data)
	})

	t.Run("Stopped manager", func(t *testing.T) {
		cm.Stop()

		data, err := cm.DownloadFromPeer(context.Background(), peerAddr, hash)

		assertStoppedManager(t, cm, err, data)
	})
}

func TestConnectionManager_returnClientToPool(t *testing.T) {
	setup := setupTest(t)
	cm := setup.manager

	t.Run("Successful reset", func(t *testing.T) {
		mockClient := protocolMocks.NewMockPeerClient(t)
		mockClient.EXPECT().Reset().Return(nil)

		cm.returnClientToPool(mockClient)
	})

	t.Run("Reset failure", func(t *testing.T) {
		resetErr := errors.New("reset failed")
		mockClient := createMockClientForReset(t, resetErr)

		// Should not panic and should discard client
		cm.returnClientToPool(mockClient)
	})

	t.Run("Stopped manager", func(t *testing.T) {
		mockClient := protocolMocks.NewMockPeerClient(t)
		cm.Stop()

		// Reset should not be called
		cm.returnClientToPool(mockClient)
	})
}

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components
type testSetup struct {
	logger     *zap.Logger
	factory    protocol.PeerClientFactory
	manager    *DefaultConnectionManager
	mockClient *protocolMocks.MockPeerClient
}

// setupTest creates a standard test environment with logger, factory, and connection manager
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	factory := mockPeerClientFactory(t)
	manager, err := NewConnectionManager(factory, logger)
	require.NoError(t, err)

	return &testSetup{
		logger:  logger,
		factory: factory,
		manager: manager,
	}
}

// setupTestWithMock creates a test environment with a specific mock client
// This helper eliminates the repetitive pattern of creating custom factories for tests
// that need to control specific mock client behavior.
func setupTestWithMock(t *testing.T, mockClient *protocolMocks.MockPeerClient) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	factory := func() protocol.PeerClient {
		return mockClient
	}
	manager, err := NewConnectionManager(factory, logger)
	require.NoError(t, err)

	return &testSetup{
		logger:     logger,
		factory:    factory,
		manager:    manager,
		mockClient: mockClient,
	}
}

// assertStoppedManager checks common stopped manager behavior
// Consolidates repetitive assertions for stopped manager test scenarios.
func assertStoppedManager(t *testing.T, manager *DefaultConnectionManager, err error, result interface{}) {
	assert.True(t, manager.IsStopped())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection manager is stopped")
	assert.Nil(t, result)
}

// assertSuccessfulOperation checks common successful operation patterns
// Reduces duplication of success assertions across test cases.
func assertSuccessfulOperation(t *testing.T, err error, result interface{}) {
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// createMockClientForDownload creates a mock client for download scenarios
// Only sets up expectations that will actually be called based on error parameters.
func createMockClientForDownload(t *testing.T, connectErr error, getBlobErr error) *protocolMocks.MockPeerClient {
	mockClient := protocolMocks.NewMockPeerClient(t)

	if connectErr != nil {
		mockClient.EXPECT().Connect(mock.Anything, mock.Anything).Return(connectErr)
		mockClient.EXPECT().Reset().Return(nil)
	} else {
		mockClient.EXPECT().Connect(mock.Anything, mock.Anything).Return(nil)
		if getBlobErr != nil {
			mockClient.EXPECT().GetBlob(mock.Anything, mock.Anything).Return(nil, getBlobErr)
		} else {
			mockClient.EXPECT().GetBlob(mock.Anything, mock.Anything).Return([]byte("test data"), nil)
		}
		mockClient.EXPECT().Reset().Return(nil)
	}

	return mockClient
}

// createMockClientForReset creates a mock client for reset testing scenarios
func createMockClientForReset(t *testing.T, resetErr error) *protocolMocks.MockPeerClient {
	mockClient := protocolMocks.NewMockPeerClient(t)

	if resetErr != nil {
		mockClient.EXPECT().Reset().Return(resetErr)
	} else {
		mockClient.EXPECT().Reset().Return(nil)
	}

	return mockClient
}

// Mock implementations for testing

// mockPeerClientFactory creates a mock PeerClientFactory that returns scoped MockPeerClient instances
func mockPeerClientFactory(t *testing.T) protocol.PeerClientFactory {
	return func() protocol.PeerClient {
		return protocolMocks.NewMockPeerClient(t)
	}
}
