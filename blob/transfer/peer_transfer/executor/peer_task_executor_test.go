package executor

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection"
	connectionMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection/mocks"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	discoveryMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery/mocks"
	executorMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/executor/mocks"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
	protocolMocks "go.lumeweb.com/liblbry/protocol/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components for executor tests
type testSetup struct {
	logger            *zap.Logger
	connMgr           *connectionMocks.MockConnectionManager
	completionHandler *executorMocks.MockCompletionHandler
	peerDiscovery     *discoveryMocks.MockPeerDiscovery
	executor          PeerTaskExecutor
	maxConcurrency    int
}

// setupTest creates a standard test environment with logger, mocks, and executor
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	connMgr := connectionMocks.NewMockConnectionManager(t)
	completionHandler := executorMocks.NewMockCompletionHandler(t)
	peerDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	maxConcurrency := 5

	executor := newPeerTaskExecutorForTesting(connMgr, peerDiscovery, completionHandler, maxConcurrency, logger)

	return &testSetup{
		logger:            logger,
		connMgr:           connMgr,
		completionHandler: completionHandler,
		peerDiscovery:     peerDiscovery,
		executor:          executor,
		maxConcurrency:    maxConcurrency,
	}
}

// setupTestWithConcurrency creates a test environment with custom concurrency
func setupTestWithConcurrency(t *testing.T, maxConcurrency int) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	connMgr := connectionMocks.NewMockConnectionManager(t)
	completionHandler := executorMocks.NewMockCompletionHandler(t)
	peerDiscovery := discoveryMocks.NewMockPeerDiscovery(t)

	executor := newPeerTaskExecutorForTesting(connMgr, peerDiscovery, completionHandler, maxConcurrency, logger)

	return &testSetup{
		logger:            logger,
		connMgr:           connMgr,
		completionHandler: completionHandler,
		peerDiscovery:     peerDiscovery,
		executor:          executor,
		maxConcurrency:    maxConcurrency,
	}
}

// newPeerTaskExecutorForTesting creates a PeerTaskExecutor with mocked dependencies
func newPeerTaskExecutorForTesting(
	connMgr connection.ConnectionManager,
	peerDiscovery discovery.PeerDiscovery,
	completionHandler CompletionHandler,
	maxConcurrency int,
	logger *zap.Logger,
) PeerTaskExecutor {
	return NewPeerTaskExecutor(connMgr, peerDiscovery, completionHandler, maxConcurrency, logger)
}

// createTestContacts creates commonly used test contact structures
// Eliminates duplication of contact creation across multiple test functions.
func createTestContacts() []dht.Contact {
	peer1ID := bits.Bitmap{1}
	peer1ID.Set(0, true)
	peer2ID := bits.Bitmap{2}
	peer2ID.Set(1, true)
	peer3ID := bits.Bitmap{3}
	peer3ID.Set(2, true)

	return []dht.Contact{
		{ID: peer1ID, IP: net.ParseIP("192.168.1.1"), PeerPort: 6347},
		{ID: peer2ID, IP: net.ParseIP("192.168.1.2"), PeerPort: 6347},
		{ID: peer3ID, IP: net.ParseIP("192.168.1.3"), PeerPort: 6347},
	}
}

// createTestContact creates a single test contact with the specified IP and port
// Provides flexibility for tests that need custom contact configurations.
func createTestContact(ip string, port int) dht.Contact {
	contactID := bits.Bitmap{1}
	contactID.Set(0, true)
	return dht.Contact{
		ID:       contactID,
		IP:       net.ParseIP(ip),
		PeerPort: port,
	}
}

// createTestBlobRequest creates a standard blob request for testing
// Eliminates duplication of blob request setup across test functions.
func createTestBlobRequest() *blob.BlobRequest {
	req := blob.NewBlobRequest()
	req.SetWaiters(1)
	return req
}

// createTestHashBitmap creates a test hash bitmap with the specified number of bits
func createTestHashBitmap(bitCount int) bits.Bitmap {
	hashBitmap := bits.Bitmap{}
	for i := 0; i < bitCount; i++ {
		hashBitmap.Set(i, true)
	}
	return hashBitmap
}

// assertStoppedExecutor checks common stopped executor behavior
// Consolidates repetitive assertions for stopped executor test scenarios.
func assertStoppedExecutor(t *testing.T, executor PeerTaskExecutor, err error, result interface{}) {
	assert.True(t, executor.IsStopped())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "peer task executor is stopped")
	assert.Nil(t, result)
}

// assertSuccessfulOperation checks common successful operation patterns
// Reduces duplication of success assertions across test cases.
func assertSuccessfulOperation(t *testing.T, err error, result interface{}) {
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// assertTaskStats verifies task statistics match expected values
// Consolidates repetitive task stats verification across test functions.
func assertTaskStats(t *testing.T, executor PeerTaskExecutor, submitted, completed, pending int64) {
	actualSubmitted, actualCompleted, actualPending := executor.GetTaskStats()
	assert.Equal(t, submitted, actualSubmitted, "Submitted tasks count mismatch")
	assert.Equal(t, completed, actualCompleted, "Completed tasks count mismatch")
	assert.Equal(t, pending, actualPending, "Pending tasks count mismatch")
}

// assertWorkerPoolStats verifies worker pool statistics match expected values
func assertWorkerPoolStats(t *testing.T, executor PeerTaskExecutor, size, waitingQueueSize int, stopped bool) {
	actualSize, actualWaitingQueueSize, actualStopped := executor.GetWorkerPoolStats()
	assert.Equal(t, size, actualSize, "Worker pool size mismatch")
	assert.Equal(t, waitingQueueSize, actualWaitingQueueSize, "Waiting queue size mismatch")
	assert.Equal(t, stopped, actualStopped, "Stopped status mismatch")
}

func TestNewPeerTaskExecutor(t *testing.T) {
	setup := setupTest(t)
	executor := setup.executor

	assertSuccessfulOperation(t, nil, executor)
	assert.Equal(t, setup.maxConcurrency, executor.GetMaxConcurrency())
	assert.False(t, executor.IsStopped())
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_Success(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:2] // Use first 2 contacts
	hashBitmap := createTestHashBitmap(2)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peers)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[1]).Return(false).Once()

	// Mock successful download with peer address string
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return([]byte("test data"), nil)
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.2:6347", hash).Return([]byte("test data"), nil)

	// Mock coordinator completion
	setup.completionHandler.EXPECT().CompleteWithData(req, mock.Anything, mock.AnythingOfType("context.CancelFunc"), hash).Times(2)

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})

	require.NoError(t, err)

	// Wait for tasks to complete
	err = executor.WaitForCompletion(500 * time.Millisecond)
	require.NoError(t, err)

	// Verify task stats
	assertTaskStats(t, executor, 2, 2, 0)

	setup.connMgr.AssertExpectations(t)
	setup.completionHandler.AssertExpectations(t)
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_ConnectionError(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	// Create mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)
	// Expect DHTNode() call since there's a peer failure
	setup.peerDiscovery.EXPECT().DHTNode().Return(mockDHTNode)

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()

	// Mock RemoveBadPeerFromHash to be called
	mockDHTNode.EXPECT().RemoveBadPeerFromHash(hashBitmap, contacts[0]).Return().Once()

	// Mock connection error
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return(nil, assert.AnError)

	// Mock failed peer tracking
	setup.completionHandler.EXPECT().TrackFailedPeer(req, mock.Anything).Return(int32(1), false)

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})

	require.NoError(t, err) // Should not return error, just track failed peer

	// Wait for tasks to complete
	err = executor.WaitForCompletion(500 * time.Millisecond)
	require.NoError(t, err)

	// Verify task stats
	assertTaskStats(t, executor, 1, 1, 0)

	setup.connMgr.AssertExpectations(t)
	setup.completionHandler.AssertExpectations(t)
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_DownloadError(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	// Create mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)
	// Expect DHTNode() call since there's a peer failure
	setup.peerDiscovery.EXPECT().DHTNode().Return(mockDHTNode)

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()

	// Mock RemoveBadPeerFromHash to be called
	mockDHTNode.EXPECT().RemoveBadPeerFromHash(hashBitmap, contacts[0]).Return().Once()

	// Mock successful download but with error
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return(nil, assert.AnError)

	// Mock failed peer tracking
	setup.completionHandler.EXPECT().TrackFailedPeer(req, mock.Anything).Return(int32(1), false)

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})

	require.NoError(t, err) // Should not return error, just track failed peer

	// Wait for tasks to complete
	err = executor.WaitForCompletion(500 * time.Millisecond)
	require.NoError(t, err)

	// Verify task stats
	assertTaskStats(t, executor, 1, 1, 0)

	setup.connMgr.AssertExpectations(t)
	setup.completionHandler.AssertExpectations(t)
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_AlreadyCompleted(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mark the request as done to simulate completion
	req.MarkDone()

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})

	require.NoError(t, err)
	// No connection attempts should be made since request is already completed
	setup.connMgr.AssertNotCalled(t, "DownloadFromPeer")

	// Verify task stats - should be 0 since no tasks were actually submitted
	assertTaskStats(t, executor, 0, 0, 0)
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_ContextCancellation(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Don't expect IsFixedPeer to be called since context is cancelled before task submission
	// The IsFixedPeer call happens inside createPeerTask, which is only called if the task is submitted

	err := executor.ExecutePeerTasks(ctx, hash, contacts, hashBitmap, req, func() {})

	require.NoError(t, err)
	// No connection attempts should be made due to context cancellation
	setup.connMgr.AssertNotCalled(t, "DownloadFromPeer")

	// Verify task stats - should be 0 since no tasks were actually submitted
	assertTaskStats(t, executor, 0, 0, 0)
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_Concurrency(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts() // Use all 3 contacts
	hashBitmap := createTestHashBitmap(3)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peers)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[1]).Return(false).Once()
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[2]).Return(false).Once()

	// Mock downloads with peer address strings
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return([]byte("test data"), nil).Once()
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.2:6347", hash).Return([]byte("test data"), nil).Once()
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.3:6347", hash).Return([]byte("test data"), nil).Once()

	// Mock coordinator completion - all 3 should succeed
	setup.completionHandler.EXPECT().CompleteWithData(req, mock.Anything, mock.AnythingOfType("context.CancelFunc"), hash).Times(3)

	start := time.Now()
	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})
	duration := time.Since(start)

	require.NoError(t, err)

	// Should complete quickly due to race condition (not waiting for all peers)
	assert.Less(t, duration, 500*time.Millisecond)

	// Wait for tasks to complete
	err = executor.WaitForCompletion(500 * time.Millisecond)
	require.NoError(t, err)

	// Verify task stats
	assertTaskStats(t, executor, 3, 3, 0)

	setup.connMgr.AssertExpectations(t)
	setup.completionHandler.AssertExpectations(t)
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_FinalPeerFailure(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	// Create mock DHT node
	mockDHTNode := protocolMocks.NewMockDHTNode(t)
	// Expect DHTNode() call since there's a peer failure
	setup.peerDiscovery.EXPECT().DHTNode().Return(mockDHTNode)

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()

	// Mock RemoveBadPeerFromHash to be called
	mockDHTNode.EXPECT().RemoveBadPeerFromHash(hashBitmap, contacts[0]).Return().Once()

	// Mock connection error
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return(nil, assert.AnError)

	// Mock failed peer tracking - this is the final peer
	setup.completionHandler.EXPECT().TrackFailedPeer(req, mock.Anything).Return(int32(1), true)
	setup.completionHandler.EXPECT().CompleteWithErrorAndCancel(req, mock.Anything, mock.AnythingOfType("context.CancelFunc"), hash)

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})

	require.NoError(t, err)

	// Wait for tasks to complete
	err = executor.WaitForCompletion(500 * time.Millisecond)
	require.NoError(t, err)

	// Verify task stats
	assertTaskStats(t, executor, 1, 1, 0)

	setup.connMgr.AssertExpectations(t)
	setup.completionHandler.AssertExpectations(t)
}

func TestDefaultPeerTaskExecutor_Lifecycle(t *testing.T) {
	setup := setupTest(t)
	executor := setup.executor

	// Initial state
	assert.False(t, executor.IsStopped())

	// Stop
	executor.Stop()
	assert.True(t, executor.IsStopped())

	// Start
	executor.Start()
	assert.False(t, executor.IsStopped())
}

func TestDefaultPeerTaskExecutor_SetMaxConcurrency(t *testing.T) {
	setup := setupTest(t)
	executor := setup.executor

	newMaxConcurrency := 10
	executor.SetMaxConcurrency(newMaxConcurrency)

	assert.Equal(t, newMaxConcurrency, executor.GetMaxConcurrency())
}

func TestDefaultPeerTaskExecutor_ExecutePeerTasks_Stopped(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor
	executor.Stop()

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})

	assertStoppedExecutor(t, executor, err, nil)
}

func TestDefaultPeerTaskExecutor_WaitForCompletionWithContext(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()

	// Mock successful download
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return([]byte("test data"), nil)
	setup.completionHandler.EXPECT().CompleteWithData(req, mock.Anything, mock.AnythingOfType("context.CancelFunc"), hash)

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})
	require.NoError(t, err)

	// Test successful completion with context
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = executor.WaitForCompletionWithContext(ctx)
	assert.NoError(t, err)
}

func TestDefaultPeerTaskExecutor_WaitForCompletionWithContext_Cancelled_LongRunning(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()

	// Mock a long-running task that will still be running when context times out
	// This will complete successfully after 300ms, but the context timeout is 200ms
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return([]byte("test data"), nil).After(300 * time.Millisecond)

	// Mock the successful completion that will happen after the context times out
	setup.completionHandler.EXPECT().CompleteWithData(req, mock.Anything, mock.AnythingOfType("context.CancelFunc"), hash).Maybe()

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})
	require.NoError(t, err)

	// Test context cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err = executor.WaitForCompletionWithContext(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestDefaultPeerTaskExecutor_GetWorkerPoolStats(t *testing.T) {
	setup := setupTest(t)
	executor := setup.executor

	// Test stats when running
	assertWorkerPoolStats(t, executor, setup.maxConcurrency, 0, false) // Should be empty initially

	// Test stats when stopped
	executor.Stop()
	assertWorkerPoolStats(t, executor, 0, 0, true) // Should be 0 when stopped
}

func TestDefaultPeerTaskExecutor_WaitAllTasksComplete(t *testing.T) {
	setup := setupTestWithConcurrency(t, 2)
	executor := setup.executor

	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	contacts := createTestContacts()[:1] // Use first contact
	hashBitmap := createTestHashBitmap(1)
	req := createTestBlobRequest()

	// Mock IsFixedPeer to return false (non-fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(contacts[0]).Return(false).Once()

	// Mock successful download
	setup.connMgr.EXPECT().DownloadFromPeer(mock.Anything, "192.168.1.1:6347", hash).Return([]byte("test data"), nil)
	setup.completionHandler.EXPECT().CompleteWithData(req, mock.Anything, mock.AnythingOfType("context.CancelFunc"), hash)

	err := executor.ExecutePeerTasks(context.Background(), hash, contacts, hashBitmap, req, func() {})
	require.NoError(t, err)

	// Wait for task to complete naturally first
	err = executor.WaitForCompletion(100 * time.Millisecond)
	require.NoError(t, err)

	// Test WaitAllTasksComplete - should work fine even when no tasks are pending
	err = executor.WaitAllTasksComplete()
	assert.NoError(t, err)

	// Verify executor is still functional after WaitAllTasksComplete
	assert.False(t, executor.IsStopped())
}
