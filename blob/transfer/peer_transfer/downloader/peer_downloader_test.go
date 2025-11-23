package downloader

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/lbry-dht/bits"
	phaseMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/phase/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
	connectionMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection/mocks"
	coordinatorMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	discoveryMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery/mocks"
	executorMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/executor/mocks"
)

func TestPeerDownloader_DownloadFromPeers_Stopped(t *testing.T) {
	setup := setupTestWithDefaults(t)
	pd := setup.downloader
	pd.Stop()

	_, err := pd.DownloadFromPeers(context.Background(), "testhash", bits.Bitmap{}, blob.NewBlobRequest())
	assertStoppedDownloader(t, pd, err, nil)
}

func TestPeerDownloader_DownloadFromPeers_DiscoveryFails(t *testing.T) {
	mockPhaseManager := phaseMocks.NewMockPhaseManager(t)
	setup := setupTestWithCustomMocks(t, mockPhaseManager)
	pd := setup.downloader

	// Mock the phase manager to simulate discovery failure
	mockPhaseManager.EXPECT().Start().Return().Once()
	mockPhaseManager.EXPECT().IsStopped().Return(false).Once()
	mockPhaseManager.EXPECT().ExecutePhases(mock.Anything, "testhash", mock.AnythingOfType("bits.Bitmap"), mock.AnythingOfType("*blob.BlobRequest")).Return(nil, fmt.Errorf("discovery failed")).Once()

	pd.Start()

	_, err := pd.DownloadFromPeers(context.Background(), "testhash", bits.Bitmap{}, blob.NewBlobRequest())
	assertErrorContains(t, err, "discovery failed")
}

func TestPeerDownloader_DownloadFromPeers_SuccessfulDHTDownload(t *testing.T) {
	setup := setupTest(t)
	pd := setup.downloader
	mockPhaseManager := setup.mockPhaseManager

	// Mock expectations for successful DHT download
	mockPhaseManager.EXPECT().Start().Return().Once()
	mockPhaseManager.EXPECT().IsStopped().Return(false).Once()
	mockPhaseManager.EXPECT().ExecutePhases(mock.Anything, "testhash", mock.AnythingOfType("bits.Bitmap"), mock.AnythingOfType("*blob.BlobRequest")).Return([]byte("test data"), nil).Once()

	pd.Start()

	result, err := pd.DownloadFromPeers(context.Background(), "testhash", bits.Bitmap{}, blob.NewBlobRequest())
	assert.NoError(t, err)
	assert.Equal(t, []byte("test data"), result)
}

func TestPeerDownloader_SetTimeout(t *testing.T) {
	setup := setupTestWithDefaults(t)
	pd := setup.downloader

	// Verify initial timeout
	initialTimeout := pd.GetTimeout()
	assert.Equal(t, 30*time.Second, initialTimeout)

	// Set new timeout
	newTimeout := 60 * time.Second
	pd.SetTimeout(newTimeout)

	// Verify timeout was updated
	updatedTimeout := pd.GetTimeout()
	assert.Equal(t, newTimeout, updatedTimeout)
}

func TestPeerDownloader_SetMaxPeers(t *testing.T) {
	setup := setupTest(t)
	pd := setup.downloader
	mockPhaseManager := setup.mockPhaseManager

	// Mock the phase manager SetMaxPeers call
	mockPhaseManager.EXPECT().SetMaxPeers(10).Return()
	mockRaceCoordinator := coordinatorMocks.NewMockPeerRaceCoordinator(t)
	mockTaskExecutor := executorMocks.NewMockPeerTaskExecutor(t)
	mockRaceCoordinator.EXPECT().GetTaskExecutor().Return(mockTaskExecutor)
	mockTaskExecutor.EXPECT().SetMaxConcurrency(10).Return()
	mockPhaseManager.EXPECT().GetRaceCoordinator().Return(mockRaceCoordinator)

	// Verify initial max peers
	initialMaxPeers := pd.GetMaxPeers()
	assert.Equal(t, 5, initialMaxPeers)

	// Set new max peers
	newMaxPeers := 10
	pd.SetMaxPeers(newMaxPeers)

	// Verify max peers was updated
	updatedMaxPeers := pd.GetMaxPeers()
	assert.Equal(t, newMaxPeers, updatedMaxPeers)
}

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components for downloader tests
type testSetup struct {
	logger           *zap.Logger
	mockConnMgr      *connectionMocks.MockConnectionManager
	mockDiscovery    *discoveryMocks.MockPeerDiscovery
	mockCoordinator  *coordinatorMocks.MockRequestCoordinator
	mockPhaseManager *phaseMocks.MockPhaseManager
	downloader       *DefaultPeerDownloader
}

// setupTest creates a standard test environment with logger, mocks, and downloader
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	mockConnMgr := connectionMocks.NewMockConnectionManager(t)
	mockDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	mockCoordinator := coordinatorMocks.NewMockRequestCoordinator(t)
	mockPhaseManager := phaseMocks.NewMockPhaseManager(t)

	downloader := NewPeerDownloader(mockConnMgr, mockDiscovery, mockCoordinator, mockPhaseManager, 5, 30*time.Second, logger)

	return &testSetup{
		logger:           logger,
		mockConnMgr:      mockConnMgr,
		mockDiscovery:    mockDiscovery,
		mockCoordinator:  mockCoordinator,
		mockPhaseManager: mockPhaseManager,
		downloader:       downloader,
	}
}

// setupTestWithDefaults creates a test environment using NewPeerDownloaderWithDefaults
// This helper eliminates the repetitive pattern of creating default downloader instances.
func setupTestWithDefaults(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	mockConnMgr := connectionMocks.NewMockConnectionManager(t)
	mockDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	mockCoordinator := coordinatorMocks.NewMockRequestCoordinator(t)

	downloader := NewPeerDownloaderWithDefaults(mockConnMgr, mockDiscovery, mockCoordinator, 5, 30*time.Second, logger)

	return &testSetup{
		logger:          logger,
		mockConnMgr:     mockConnMgr,
		mockDiscovery:   mockDiscovery,
		mockCoordinator: mockCoordinator,
		downloader:      downloader,
	}
}

// setupTestWithCustomMocks creates a test environment with specific mock instances
// This helper eliminates the repetitive pattern of creating custom mock setups.
func setupTestWithCustomMocks(t *testing.T, mockPhaseManager *phaseMocks.MockPhaseManager) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	mockConnMgr := connectionMocks.NewMockConnectionManager(t)
	mockDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	mockCoordinator := coordinatorMocks.NewMockRequestCoordinator(t)

	downloader := NewPeerDownloader(mockConnMgr, mockDiscovery, mockCoordinator, mockPhaseManager, 5, 30*time.Second, logger)

	return &testSetup{
		logger:           logger,
		mockConnMgr:      mockConnMgr,
		mockDiscovery:    mockDiscovery,
		mockCoordinator:  mockCoordinator,
		mockPhaseManager: mockPhaseManager,
		downloader:       downloader,
	}
}

// assertStoppedDownloader checks common stopped downloader behavior
// Consolidates repetitive assertions for stopped downloader test scenarios.
func assertStoppedDownloader(t *testing.T, downloader *DefaultPeerDownloader, err error, result interface{}) {
	assert.True(t, downloader.IsStopped())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "peer downloader is stopped")
	assert.Nil(t, result)
}

// assertSuccessfulOperation checks common successful operation patterns
// Reduces duplication of success assertions across test cases.
func assertSuccessfulOperation(t *testing.T, err error, result interface{}) {
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// assertErrorContains checks that an error contains expected text
// Reduces duplication of error assertion patterns.
func assertErrorContains(t *testing.T, err error, expectedText string) {
	assert.Error(t, err)
	assert.Contains(t, err.Error(), expectedText)
}
