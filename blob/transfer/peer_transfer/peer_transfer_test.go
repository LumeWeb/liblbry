package peer_transfer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection"
	connectionMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection/mocks"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	coordinatorMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	discoveryMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery/mocks"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/downloader"
	downloaderMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/downloader/mocks"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components
type testSetup struct {
	logger      *zap.Logger
	discovery   *discoveryMocks.MockPeerDiscovery
	connMgr     *connectionMocks.MockConnectionManager
	coordinator *coordinatorMocks.MockRequestCoordinator
	downloader  *downloaderMocks.MockPeerDownloader
	transfer    *PeerTransfer
}

// setupTest creates a standard test environment with all mocked dependencies
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	mockDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	mockConnMgr := connectionMocks.NewMockConnectionManager(t)
	mockCoordinator := coordinatorMocks.NewMockRequestCoordinator(t)
	mockDownloader := downloaderMocks.NewMockPeerDownloader(t)

	transfer := &PeerTransfer{
		discovery:   mockDiscovery,
		connMgr:     mockConnMgr,
		coordinator: mockCoordinator,
		downloader:  mockDownloader,
		logger:      logger,
	}

	return &testSetup{
		logger:      logger,
		discovery:   mockDiscovery,
		connMgr:     mockConnMgr,
		coordinator: mockCoordinator,
		downloader:  mockDownloader,
		transfer:    transfer,
	}
}

// assertTransferError checks common transfer error patterns
// Consolidates repetitive assertions for error test scenarios.
func assertTransferError(t *testing.T, err error, result []byte, expectedErrorContains string) {
	assert.Error(t, err)
	if expectedErrorContains != "" {
		assert.Contains(t, err.Error(), expectedErrorContains)
	}
	assert.Nil(t, result)
}

// assertTransferSuccess checks common successful transfer patterns
// Reduces duplication of success assertions across test cases.
func assertTransferSuccess(t *testing.T, err error, result []byte, expectedData []byte) {
	require.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// setupCoordinatorExpectations creates common coordinator mock expectations
// Eliminates repetitive coordinator setup across test functions.
func setupCoordinatorExpectations(coordinator *coordinatorMocks.MockRequestCoordinator, hash string, request *blob.BlobRequest) {
	coordinator.EXPECT().GetOrCreateRequest(hash).Return(request, true)
	coordinator.EXPECT().RemoveRequest(hash)
}

// newPeerTransferForTesting creates a PeerTransfer with mocked dependencies for testing
// Deprecated: Use setupTest() instead for better consistency
func newPeerTransferForTesting(
	peerDiscovery discovery.PeerDiscovery,
	peerConnMgr connection.ConnectionManager,
	peerCoordinator coordinator.RequestCoordinator,
	peerDownloader downloader.PeerDownloader,
) *PeerTransfer {
	return &PeerTransfer{
		discovery:   peerDiscovery,
		connMgr:     peerConnMgr,
		coordinator: peerCoordinator,
		downloader:  peerDownloader,
		logger:      zap.NewNop(),
	}
}

func TestNewPeerTransfer(t *testing.T) {
	// Test that NewPeerTransfer creates a valid instance
	setup := setupTest(t)

	require.NotNil(t, setup.transfer)
	assert.Equal(t, TransferName, setup.transfer.Name())
}

func TestPeerTransfer_Get_Success(t *testing.T) {
	// Test successful blob retrieval
	setup := setupTest(t)
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	testData := []byte("test data")
	mockRequest := blob.NewBlobRequest()

	// Mock coordinator calls
	setupCoordinatorExpectations(setup.coordinator, hash, mockRequest)

	// Mock successful download
	setup.downloader.EXPECT().DownloadFromPeers(mock.Anything, hash, mock.AnythingOfType("bits.Bitmap"), mockRequest).Return(testData, nil)

	result, err := setup.transfer.Get(context.Background(), hash)

	assertTransferSuccess(t, err, result, testData)
	setup.downloader.AssertExpectations(t)
	setup.coordinator.AssertExpectations(t)
}

func TestPeerTransfer_Get_DownloadError(t *testing.T) {
	// Test handling of errors from downloader
	setup := setupTest(t)
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	mockRequest := blob.NewBlobRequest()

	// Mock coordinator calls
	setupCoordinatorExpectations(setup.coordinator, hash, mockRequest)

	// Mock download failure
	setup.downloader.EXPECT().DownloadFromPeers(mock.Anything, hash, mock.AnythingOfType("bits.Bitmap"), mockRequest).Return(nil, assert.AnError)

	result, err := setup.transfer.Get(context.Background(), hash)

	// Should return error but not panic
	assertTransferError(t, err, result, "")
	setup.downloader.AssertExpectations(t)
	setup.coordinator.AssertExpectations(t)
}

func TestPeerTransfer_Get_ContextCancellation(t *testing.T) {
	// Test that context cancellation is handled properly
	setup := setupTest(t)
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	mockRequest := blob.NewBlobRequest()

	// Mock coordinator call
	setupCoordinatorExpectations(setup.coordinator, hash, mockRequest)

	// Mock download call (should see a canceled context)
	setup.downloader.EXPECT().DownloadFromPeers(mock.Anything, hash, mock.AnythingOfType("bits.Bitmap"), mockRequest).Return(nil, context.Canceled)

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := setup.transfer.Get(ctx, hash)

	assertTransferError(t, err, result, "")
	assert.Equal(t, context.Canceled, err)
	setup.coordinator.AssertExpectations(t)
}

func TestPeerTransfer_Get_EmptyHash(t *testing.T) {
	// Test that empty hash is rejected
	setup := setupTest(t)

	result, err := setup.transfer.Get(context.Background(), "")

	assertTransferError(t, err, result, "invalid hash")
}

func TestPeerTransfer_Name(t *testing.T) {
	// Test that Name method returns expected value
	setup := setupTest(t)

	assert.Equal(t, TransferName, setup.transfer.Name())
}
