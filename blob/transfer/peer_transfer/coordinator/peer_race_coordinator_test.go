package coordinator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
	connectionMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection/mocks"
	discoveryMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery/mocks"
	executorMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/executor/mocks"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
)

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components
type testSetup struct {
	logger             *zap.Logger
	connMgr            *connectionMocks.MockConnectionManager
	discovery          *discoveryMocks.MockPeerDiscovery
	requestCoordinator *MockRequestCoordinator
	taskExecutor       *executorMocks.MockPeerTaskExecutor
	timeout            time.Duration
	coordinator        PeerRaceCoordinator
}

// setupTest creates a standard test environment with all required mocks and coordinator
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t).Named("test")
	connMgr := connectionMocks.NewMockConnectionManager(t)
	discovery := discoveryMocks.NewMockPeerDiscovery(t)
	requestCoordinator := NewMockRequestCoordinator(t)
	taskExecutor := executorMocks.NewMockPeerTaskExecutor(t)
	timeout := 5 * time.Second

	coordinator := NewPeerRaceCoordinator(connMgr, discovery, requestCoordinator, taskExecutor, timeout, logger)

	return &testSetup{
		logger:             logger,
		connMgr:            connMgr,
		discovery:          discovery,
		requestCoordinator: requestCoordinator,
		taskExecutor:       taskExecutor,
		timeout:            timeout,
		coordinator:        coordinator,
	}
}

// testDataBuilder helps build common test data with fluent interface
// This builder pattern eliminates repetitive test data setup and makes tests more readable.
type testDataBuilder struct {
	hash       string
	contacts   []dht.Contact
	hashBitmap bits.Bitmap
	req        *blob.BlobRequest
}

// newTestDataBuilder creates a new test data builder with sensible defaults
func newTestDataBuilder() *testDataBuilder {
	return &testDataBuilder{
		hash:       lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1],
		contacts:   []dht.Contact{{ID: bits.Bitmap{1}}},
		hashBitmap: bits.Bitmap{1},
		req:        blob.NewBlobRequest(),
	}
}

// withHash sets the hash for the test data
func (b *testDataBuilder) withHash(hash string) *testDataBuilder {
	b.hash = hash
	return b
}

// withContacts sets the contacts for the test data
func (b *testDataBuilder) withContacts(contacts []dht.Contact) *testDataBuilder {
	b.contacts = contacts
	return b
}

// withWaiters sets the number of waiters on the request
func (b *testDataBuilder) withWaiters(waiters int) *testDataBuilder {
	b.req.SetWaiters(waiters)
	return b
}

// withResult sets a result on the request
func (b *testDataBuilder) withResult(data []byte, err error) *testDataBuilder {
	taskResult := blob.NewTaskResult(data, err)
	b.req.SetResult(taskResult)
	return b
}

// markDone marks the request as done
func (b *testDataBuilder) markDone() *testDataBuilder {
	b.req.MarkDone()
	return b
}

// build returns the constructed test data
func (b *testDataBuilder) build() (string, []dht.Contact, bits.Bitmap, *blob.BlobRequest) {
	return b.hash, b.contacts, b.hashBitmap, b.req
}

// assertRaceError checks common race error patterns
// Consolidates repetitive assertions for race error test scenarios.
func assertRaceError(t *testing.T, err error, expectedMsg string) {
	t.Helper()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), expectedMsg)
}

// assertSuccessfulRace checks common successful race execution patterns
// Reduces duplication of success assertions across race test cases.
func assertSuccessfulRace(t *testing.T, err error, result []byte, expectedData []byte) {
	t.Helper()
	require.NoError(t, err)
	assert.Equal(t, expectedData, result)
}

// setupMockRaceSuccess configures mocks for successful race execution
// This helper eliminates repetitive mock setup for success scenarios.
func setupMockRaceSuccess(setup *testSetup, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, testData []byte) {
	totalPeers := int32(len(contacts))
	setup.requestCoordinator.EXPECT().InitializeRequest(req, mock.AnythingOfType("context.CancelFunc"), totalPeers).Return()
	setup.taskExecutor.EXPECT().ExecutePeerTasks(mock.Anything, hash, contacts, hashBitmap, req, mock.AnythingOfType("context.CancelFunc")).Return(nil)
	setup.requestCoordinator.EXPECT().WaitForResult(mock.Anything, req).Return(testData, nil)
}

// setupMockRaceTaskError configures mocks for task execution error scenarios
// This helper eliminates repetitive mock setup for task error scenarios.
func setupMockRaceTaskError(setup *testSetup, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest) {
	totalPeers := int32(len(contacts))
	setup.requestCoordinator.EXPECT().InitializeRequest(req, mock.AnythingOfType("context.CancelFunc"), totalPeers).Return()
	setup.taskExecutor.EXPECT().ExecutePeerTasks(mock.Anything, hash, contacts, hashBitmap, req, mock.AnythingOfType("context.CancelFunc")).Return(assert.AnError)
}

// setupMockRaceStateCheck configures mocks for request state checking scenarios
// This helper eliminates repetitive mock setup for state checking scenarios.
func setupMockRaceStateCheck(setup *testSetup, req *blob.BlobRequest, completed, total int32, err error) {
	setup.requestCoordinator.EXPECT().GetRequestState(req).Return(completed, total, err).Once()
}

// Test functions

func TestNewPeerRaceCoordinator(t *testing.T) {
	setup := setupTest(t)

	require.NotNil(t, setup.coordinator)
	assert.False(t, setup.coordinator.IsStopped())

	// Set up mock expectations for lifecycle methods
	setup.connMgr.EXPECT().Start().Once()
	setup.discovery.EXPECT().Start().Once()
	setup.requestCoordinator.EXPECT().Start().Once()
	setup.taskExecutor.EXPECT().Start().Once()
	setup.connMgr.EXPECT().Stop().Once()
	setup.discovery.EXPECT().Stop().Once()
	setup.requestCoordinator.EXPECT().Stop().Once()
	setup.taskExecutor.EXPECT().Stop().Once()

	// Test that the coordinator can be started and stopped (basic lifecycle)
	setup.coordinator.Start()
	assert.False(t, setup.coordinator.IsStopped())

	setup.coordinator.Stop()
	assert.True(t, setup.coordinator.IsStopped())
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_Success(t *testing.T) {
	setup := setupTest(t)
	hash, contacts, hashBitmap, req := newTestDataBuilder().withWaiters(1).build()
	testData := []byte("test data")

	setupMockRaceSuccess(setup, hash, contacts, hashBitmap, req, testData)

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, true)

	assertSuccessfulRace(t, err, result, testData)
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_NoPeers(t *testing.T) {
	setup := setupTest(t)
	hash, contacts, hashBitmap, req := newTestDataBuilder().withContacts([]dht.Contact{}).build()

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, true)

	assertRaceError(t, err, "no peers available")
	assert.Nil(t, result)
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_Stopped(t *testing.T) {
	setup := setupTest(t)
	hash, contacts, hashBitmap, req := newTestDataBuilder().build()

	// Set up mock expectations for Stop methods
	setup.connMgr.EXPECT().Stop().Once()
	setup.discovery.EXPECT().Stop().Once()
	setup.requestCoordinator.EXPECT().Stop().Once()
	setup.taskExecutor.EXPECT().Stop().Once()
	setup.coordinator.Stop()

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, true)

	assertRaceError(t, err, "peer race coordinator is stopped")
	assert.Nil(t, result)
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_TaskExecutionError(t *testing.T) {
	setup := setupTest(t)
	hash, contacts, hashBitmap, req := newTestDataBuilder().build()

	setupMockRaceTaskError(setup, hash, contacts, hashBitmap, req)

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, true)

	assertRaceError(t, err, "failed to execute peer tasks")
	assert.Nil(t, result)
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_NonFinalPhase_Success(t *testing.T) {
	setup := setupTest(t)
	testData := []byte("test data")
	hash, contacts, hashBitmap, req := newTestDataBuilder().withWaiters(1).withResult(testData, nil).markDone().build()

	// Mock coordinator initialization
	setup.requestCoordinator.EXPECT().InitializeRequest(req, mock.AnythingOfType("context.CancelFunc"), int32(1)).Return()

	// Mock task execution
	setup.taskExecutor.EXPECT().ExecutePeerTasks(mock.Anything, hash, contacts, hashBitmap, req, mock.AnythingOfType("context.CancelFunc")).Return(nil)

	// Mock request state - not completed yet
	setupMockRaceStateCheck(setup, req, 0, 1, nil)

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, false)

	assertSuccessfulRace(t, err, result, testData)
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_NonFinalPhase_AllPeersFailed(t *testing.T) {
	setup := setupTest(t)
	hash, contacts, hashBitmap, req := newTestDataBuilder().withWaiters(2).build()

	// Mock coordinator initialization
	setup.requestCoordinator.EXPECT().InitializeRequest(req, mock.AnythingOfType("context.CancelFunc"), int32(1)).Return()

	// Mock task execution
	setup.taskExecutor.EXPECT().ExecutePeerTasks(mock.Anything, hash, contacts, hashBitmap, req, mock.AnythingOfType("context.CancelFunc")).Return(nil)

	// Mock request state - all peers completed but no success (multiple peers)
	setupMockRaceStateCheck(setup, req, 2, 2, nil)

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, false)

	assert.Error(t, err)
	assert.EqualError(t, err, "all peers in this phase failed")
	assert.Nil(t, result)
}

func TestDefaultPeerRaceCoordinator_ExecuteRace_NonFinalPhase_SinglePeerError(t *testing.T) {
	setup := setupTest(t)
	hash, contacts, hashBitmap, req := newTestDataBuilder().withWaiters(1).build()
	testErr := assert.AnError

	// Mock coordinator initialization
	setup.requestCoordinator.EXPECT().InitializeRequest(req, mock.AnythingOfType("context.CancelFunc"), int32(1)).Return()

	// Mock task execution
	setup.taskExecutor.EXPECT().ExecutePeerTasks(mock.Anything, hash, contacts, hashBitmap, req, mock.AnythingOfType("context.CancelFunc")).Return(nil)

	// Mock request state - single peer failed
	setupMockRaceStateCheck(setup, req, 1, 1, testErr)

	result, err := setup.coordinator.ExecuteRace(context.Background(), hash, contacts, hashBitmap, req, false)

	assert.Error(t, err)
	assert.ErrorIs(t, err, testErr)
	assert.Nil(t, result)
}

func TestDefaultPeerRaceCoordinator_Lifecycle(t *testing.T) {
	setup := setupTest(t)

	// Set up mock expectations for lifecycle methods
	setup.connMgr.EXPECT().Stop().Once()
	setup.discovery.EXPECT().Stop().Once()
	setup.requestCoordinator.EXPECT().Stop().Once()
	setup.taskExecutor.EXPECT().Stop().Once()
	setup.connMgr.EXPECT().Start().Once()
	setup.discovery.EXPECT().Start().Once()
	setup.requestCoordinator.EXPECT().Start().Once()
	setup.taskExecutor.EXPECT().Start().Once()

	// Initial state
	assert.False(t, setup.coordinator.IsStopped())

	// Stop
	setup.coordinator.Stop()
	assert.True(t, setup.coordinator.IsStopped())

	// Start
	setup.coordinator.Start()
	assert.False(t, setup.coordinator.IsStopped())
}

func TestDefaultPeerRaceCoordinator_SetTimeout(t *testing.T) {
	setup := setupTest(t)

	newTimeout := 10 * time.Second
	setup.coordinator.SetTimeout(newTimeout)

	assert.Equal(t, newTimeout, setup.coordinator.GetTimeout())
}

func TestDefaultPeerRaceCoordinator_GetComponents(t *testing.T) {
	setup := setupTest(t)

	assert.Equal(t, setup.requestCoordinator, setup.coordinator.GetCoordinator())
	assert.Equal(t, setup.taskExecutor, setup.coordinator.GetTaskExecutor())
}
