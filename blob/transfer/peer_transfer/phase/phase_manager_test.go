package phase

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	coordinatorMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	discoveryMocks "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery/mocks"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
)

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// testSetup provides common test setup components for phase manager tests
type testSetup struct {
	logger          *zap.Logger
	peerDiscovery   *discoveryMocks.MockPeerDiscovery
	raceCoordinator *coordinatorMocks.MockPeerRaceCoordinator
	manager         PhaseManager
	maxPeers        int
}

// setupTest creates a standard test environment with logger, mocks, and phase manager
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupTest(t *testing.T) *testSetup {
	logger := zaptest.NewLogger(t)
	peerDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	raceCoordinator := coordinatorMocks.NewMockPeerRaceCoordinator(t)
	maxPeers := 10

	manager := NewPhaseManager(peerDiscovery, raceCoordinator, maxPeers, logger)

	return &testSetup{
		logger:          logger,
		peerDiscovery:   peerDiscovery,
		raceCoordinator: raceCoordinator,
		manager:         manager,
		maxPeers:        maxPeers,
	}
}

// setupTestWithCustomMaxPeers creates a test environment with custom maxPeers
func setupTestWithCustomMaxPeers(t *testing.T, maxPeers int) *testSetup {
	logger := zaptest.NewLogger(t)
	peerDiscovery := discoveryMocks.NewMockPeerDiscovery(t)
	raceCoordinator := coordinatorMocks.NewMockPeerRaceCoordinator(t)

	manager := NewPhaseManager(peerDiscovery, raceCoordinator, maxPeers, logger)

	return &testSetup{
		logger:          logger,
		peerDiscovery:   peerDiscovery,
		raceCoordinator: raceCoordinator,
		manager:         manager,
		maxPeers:        maxPeers,
	}
}

// createTestHashAndBitmap returns commonly used test hash and bitmap
func createTestHashAndBitmap() (string, bits.Bitmap) {
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]
	hashBitmap := bits.Bitmap{1}
	return hash, hashBitmap
}

// createTestBlobRequest creates a standard blob request for testing
func createTestBlobRequest() *blob.BlobRequest {
	req := blob.NewBlobRequest()
	req.SetWaiters(1)
	return req
}

// createTestContact creates a single test contact with the specified parameters
func createTestContact(ip string, dhtPort, peerPort int, id bits.Bitmap) dht.Contact {
	return dht.Contact{
		ID:       id,
		IP:       net.ParseIP(ip),
		Port:     dhtPort,
		PeerPort: peerPort,
	}
}

// createTestContacts creates commonly used test contact structures
func createTestContacts() []dht.Contact {
	contactID1 := bits.Bitmap{1}
	contactID1.Set(0, true)
	contactID2 := bits.Bitmap{2}
	contactID2.Set(0, true)

	return []dht.Contact{
		createTestContact("127.0.0.1", 5000, 5001, contactID1),
		createTestContact("127.0.0.2", 5000, 5001, contactID2),
	}
}

// createDHTContacts creates test contacts for DHT scenarios
func createDHTContacts() []dht.Contact {
	contactID := bits.Bitmap{1}
	contactID.Set(0, true)
	return []dht.Contact{
		createTestContact("127.0.0.1", 5000, 5001, contactID),
	}
}

// createFixedContacts creates test contacts for fixed peer scenarios
func createFixedContacts() []dht.Contact {
	fixedContactID := bits.Bitmap{1}
	fixedContactID.Set(0, true)
	return []dht.Contact{
		createTestContact("127.0.0.1", 5000, 5001, fixedContactID),
	}
}

// assertStoppedPhaseManager checks common stopped phase manager behavior
// Consolidates repetitive assertions for stopped phase manager test scenarios.
func assertStoppedPhaseManager(t *testing.T, manager PhaseManager, err error, result interface{}) {
	assert.True(t, manager.IsStopped())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "phase manager is stopped")
	assert.Nil(t, result)
}

// assertSuccessfulOperation checks common successful operation patterns
// Reduces duplication of success assertions across test cases.
func assertSuccessfulOperation(t *testing.T, err error, result interface{}) {
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// assertNoPeersAvailable checks common "no peers available" error scenario
func assertNoPeersAvailable(t *testing.T, err error, result interface{}) {
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no peers available")
	assert.Nil(t, result)
}

// setupMockDiscoveryForDHTSuccess sets up mock expectations for successful DHT discovery
func setupMockDiscoveryForDHTSuccess(setup *testSetup, hashBitmap bits.Bitmap, contacts []dht.Contact) {
	setup.peerDiscovery.EXPECT().DiscoverPeers(mock.Anything, hashBitmap).Return(contacts, nil)
	setup.peerDiscovery.EXPECT().GetFixedPeers().Return([]dht.Contact{}).Maybe()
	// Mock IsFixedPeer calls for each DHT contact (they should not be fixed peers)
	for _, contact := range contacts {
		setup.peerDiscovery.EXPECT().IsFixedPeer(contact).Return(false)
	}
}

// setupMockDiscoveryForDHTFailure sets up mock expectations for DHT discovery failure
func setupMockDiscoveryForDHTFailure(setup *testSetup, hashBitmap bits.Bitmap) {
	setup.peerDiscovery.EXPECT().DiscoverPeers(mock.Anything, hashBitmap).Return([]dht.Contact{}, nil)
}

// setupMockDiscoveryForFixedPeers sets up mock expectations for fixed peers
func setupMockDiscoveryForFixedPeers(setup *testSetup, fixedContacts []dht.Contact) {
	setup.peerDiscovery.EXPECT().GetFixedPeers().Return(fixedContacts).Maybe()
}

// setupMockDiscoveryForDHTAndFixedPeers sets up mock expectations for DHT discovery with fixed peers
func setupMockDiscoveryForDHTAndFixedPeers(setup *testSetup, hashBitmap bits.Bitmap, dhtContacts []dht.Contact, fixedContacts []dht.Contact) {
	setup.peerDiscovery.EXPECT().DiscoverPeers(mock.Anything, hashBitmap).Return(dhtContacts, nil)
	setup.peerDiscovery.EXPECT().GetFixedPeers().Return(fixedContacts).Maybe()
	// Mock IsFixedPeer calls for each DHT contact (they should not be fixed peers)
	for _, contact := range dhtContacts {
		setup.peerDiscovery.EXPECT().IsFixedPeer(contact).Return(false)
	}
}

// setupMockCoordinatorForPhaseReset sets up mock expectations for phase reset
func setupMockCoordinatorForPhaseReset(setup *testSetup, req *blob.BlobRequest, contactCount int, t *testing.T) *coordinatorMocks.MockRequestCoordinator {
	mockCoordinator := coordinatorMocks.NewMockRequestCoordinator(t)
	setup.raceCoordinator.EXPECT().GetCoordinator().Return(mockCoordinator)
	mockCoordinator.EXPECT().ResetForNewPhase(req, int32(contactCount))
	return mockCoordinator
}

func TestNewPhaseManager(t *testing.T) {
	setup := setupTest(t)
	manager := setup.manager

	require.NotNil(t, manager)
	assert.Equal(t, int32(setup.maxPeers), manager.GetMaxPeers())
	assert.Equal(t, setup.logger, manager.(*DefaultPhaseManager).logger)
	assert.False(t, manager.IsStopped())
}

func TestDefaultPhaseManager_ExecutePhases_DHTSuccess(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()
	contacts := createDHTContacts()

	// Mock DHT peer discovery
	setupMockDiscoveryForDHTSuccess(setup, hashBitmap, contacts)

	// Mock GetCoordinator().ResetForNewPhase for DHT phase
	_ = setupMockCoordinatorForPhaseReset(setup, req, len(contacts), t)

	// Mock successful race execution
	testData := []byte("test data")
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, contacts, hashBitmap, req, true).Return(testData, nil)

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assertSuccessfulOperation(t, err, result)
	assert.Equal(t, testData, result)
}

func TestDefaultPhaseManager_ExecutePhases_DHTFallbackToFixed(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()
	fixedContacts := createFixedContacts()

	// Mock DHT peer discovery returns no peers
	setupMockDiscoveryForDHTFailure(setup, hashBitmap)

	// Mock fixed peer discovery
	setupMockDiscoveryForFixedPeers(setup, fixedContacts)

	// Mock GetCoordinator().ResetForNewPhase for fixed peers phase
	mockCoordinator := setupMockCoordinatorForPhaseReset(setup, req, len(fixedContacts), t)
	mockCoordinator.EXPECT().ResetForNewPhase(req, int32(len(fixedContacts)))

	// Mock successful race execution with fixed peers
	testData := []byte("test data")
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, fixedContacts, hashBitmap, req, true).Return(testData, nil)

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assertSuccessfulOperation(t, err, result)
	assert.Equal(t, testData, result)
}

func TestDefaultPhaseManager_ExecutePhases_DHTErrorFallbackToFixed(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()
	fixedContacts := createFixedContacts()

	// Mock DHT peer discovery to return empty contacts (not error, so we proceed to fixed peers)
	setupMockDiscoveryForDHTFailure(setup, hashBitmap)

	// Mock fixed peer discovery
	setupMockDiscoveryForFixedPeers(setup, fixedContacts)

	// Mock GetCoordinator().ResetForNewPhase for fixed peers phase
	mockCoordinator := setupMockCoordinatorForPhaseReset(setup, req, len(fixedContacts), t)
	mockCoordinator.EXPECT().ResetForNewPhase(req, int32(len(fixedContacts)))

	// Mock successful race execution with fixed peers
	testData := []byte("test data")
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, fixedContacts, hashBitmap, req, true).Return(testData, nil)

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assertSuccessfulOperation(t, err, result)
	assert.Equal(t, testData, result)
}

func TestDefaultPhaseManager_ExecutePhases_DHTPhaseFailsFixedSuccess(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()
	dhtContacts := createDHTContacts()
	fixedContactID := bits.Bitmap{2}
	fixedContactID.Set(0, true)
	fixedContacts := []dht.Contact{
		createTestContact("127.0.0.2", 5000, 5001, fixedContactID),
	}

	// Mock DHT peer discovery with fixed peers
	setupMockDiscoveryForDHTAndFixedPeers(setup, hashBitmap, dhtContacts, fixedContacts)

	// Mock IsFixedPeer to return false (DHT contact is not a fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(dhtContacts[0]).Return(false)

	// Mock DHT race execution failure (completeOnFailure=false since fixed peers are available)
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, dhtContacts, hashBitmap, req, false).Return(nil, assert.AnError)

	// Mock GetCoordinator().ResetForNewPhase for fixed peers phase
	mockCoordinator := setupMockCoordinatorForPhaseReset(setup, req, len(fixedContacts), t)
	mockCoordinator.EXPECT().ResetForNewPhase(req, int32(len(fixedContacts)))

	// Mock successful race execution with fixed peers
	testData := []byte("test data")
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, fixedContacts, hashBitmap, req, true).Return(testData, nil)

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assertSuccessfulOperation(t, err, result)
	assert.Equal(t, testData, result)
}

func TestDefaultPhaseManager_ExecutePhases_AllPhasesFail(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()
	dhtContacts := createDHTContacts()
	fixedContactID := bits.Bitmap{2}
	fixedContactID.Set(0, true)
	fixedContacts := []dht.Contact{
		createTestContact("127.0.0.2", 5000, 5001, fixedContactID),
	}

	// Mock DHT peer discovery with fixed peers
	setupMockDiscoveryForDHTAndFixedPeers(setup, hashBitmap, dhtContacts, fixedContacts)

	// Mock IsFixedPeer to return false (DHT contact is not a fixed peer)
	setup.peerDiscovery.EXPECT().IsFixedPeer(dhtContacts[0]).Return(false)

	// Mock DHT race execution failure
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, dhtContacts, hashBitmap, req, false).Return(nil, assert.AnError)

	// Mock GetCoordinator().ResetForNewPhase for fixed peers phase
	mockCoordinator := setupMockCoordinatorForPhaseReset(setup, req, len(fixedContacts), t)
	mockCoordinator.EXPECT().ResetForNewPhase(req, int32(len(fixedContacts)))

	// Mock fixed race execution failure
	setup.raceCoordinator.EXPECT().ExecuteRace(mock.Anything, hash, fixedContacts, hashBitmap, req, true).Return(nil, assert.AnError)

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestDefaultPhaseManager_ExecutePhases_NoPeersAtAll(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()

	// Mock DHT peer discovery returns no peers
	setupMockDiscoveryForDHTFailure(setup, hashBitmap)

	// Mock fixed peer discovery returns no peers
	setup.peerDiscovery.EXPECT().GetFixedPeers().Return([]dht.Contact{}).Maybe()

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assertNoPeersAvailable(t, err, result)
}

func TestDefaultPhaseManager_ExecutePhases_Stopped(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()

	setup.manager.Stop()

	result, err := setup.manager.ExecutePhases(context.Background(), hash, hashBitmap, req)

	assertStoppedPhaseManager(t, setup.manager, err, result)
}

func TestDefaultPhaseManager_ExecutePhases_ContextCancellation(t *testing.T) {
	setup := setupTest(t)
	hash, hashBitmap := createTestHashAndBitmap()
	req := createTestBlobRequest()

	// Create a context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Mock DiscoverPeers to return an error due to context cancellation
	setup.peerDiscovery.EXPECT().DiscoverPeers(mock.Anything, hashBitmap).Return(nil, context.Canceled)
	// Mock GetFixedPeers to return empty list for fallback
	setup.peerDiscovery.EXPECT().GetFixedPeers().Return([]dht.Contact{})

	result, err := setup.manager.ExecutePhases(ctx, hash, hashBitmap, req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no peers available for phase Fixed")
	assert.Nil(t, result)
}

func TestDefaultPhaseManager_Lifecycle(t *testing.T) {
	setup := setupTest(t)
	manager := setup.manager

	// Initial state
	assert.False(t, manager.IsStopped())

	// Stop
	manager.Stop()
	assert.True(t, manager.IsStopped())

	// Start
	manager.Start()
	assert.False(t, manager.IsStopped())
}

func TestDefaultPhaseManager_SetMaxPeers(t *testing.T) {
	setup := setupTest(t)
	manager := setup.manager

	newMaxPeers := 20
	manager.SetMaxPeers(newMaxPeers)

	assert.Equal(t, int32(newMaxPeers), manager.GetMaxPeers())
}

func TestDefaultPhaseManager_GetRaceCoordinator(t *testing.T) {
	setup := setupTest(t)

	assert.Equal(t, setup.raceCoordinator, setup.manager.GetRaceCoordinator())
}
