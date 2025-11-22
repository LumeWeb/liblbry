package phase

import (
	"context"
	"fmt"

	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	"go.uber.org/zap"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
)

// PhaseType represents the different phases of peer download
type PhaseType int

const (
	PhaseDHT PhaseType = iota
	PhaseFixed
)

// PhaseManager defines the interface for managing download phases
type PhaseManager interface {
	// ExecutePhases executes the download phases (DHT then fixed peers as fallback)
	ExecutePhases(ctx context.Context, hash string, hashBitmap bits.Bitmap, req *blob.BlobRequest) ([]byte, error)

	// IsStopped checks if the phase manager is stopped
	IsStopped() bool

	// Stop stops the phase manager
	Stop()

	// Start starts the phase manager
	Start()

	// SetMaxPeers updates the maximum number of peers to try per phase
	SetMaxPeers(maxPeers int)

	// GetRaceCoordinator returns the underlying race coordinator
	GetRaceCoordinator() coordinator.PeerRaceCoordinator
}

// DefaultPhaseManager manages the execution of different download phases
type DefaultPhaseManager struct {
	discovery       discovery.PeerDiscovery
	raceCoordinator coordinator.PeerRaceCoordinator
	maxPeers        int
	logger          *zap.Logger
	stopped         int32
}

// NewPhaseManager creates a new DefaultPhaseManager instance
func NewPhaseManager(
	discovery discovery.PeerDiscovery,
	raceCoordinator coordinator.PeerRaceCoordinator,
	maxPeers int,
	logger *zap.Logger,
) PhaseManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &DefaultPhaseManager{
		discovery:       discovery,
		raceCoordinator: raceCoordinator,
		maxPeers:        maxPeers,
		logger:          logger,
	}
}

// ExecutePhases executes the download phases (DHT then fixed peers as fallback)
func (pm *DefaultPhaseManager) ExecutePhases(ctx context.Context, hash string, hashBitmap bits.Bitmap, req *blob.BlobRequest) ([]byte, error) {
	if pm.IsStopped() {
		return nil, fmt.Errorf("phase manager is stopped")
	}

	// Discover peers via DHT
	contacts, err := pm.discovery.DiscoverPeers(ctx, hashBitmap)
	if err != nil {
		return nil, fmt.Errorf("failed to discover peers: %w", err)
	}

	// Execute Phase 1: DHT peers
	result, err := pm.executePhase(ctx, hash, contacts, hashBitmap, req, PhaseDHT)
	if err == nil {
		return result, nil
	}

	// Check if we should proceed to fixed peers phase
	if !pm.shouldTryFixedPeers(contacts) {
		return nil, err
	}

	// Reset completion tracking for fixed peers phase
	pm.raceCoordinator.GetCoordinator().ResetForNewPhase(req, int32(len(pm.discovery.GetFixedPeers())))

	pm.logger.Debug("All DHT peers failed, checking fixed peers fallback",
		zap.String("hash", hash),
		zap.Error(err))

	// Execute Phase 2: Fixed peers
	result, err = pm.executePhase(ctx, hash, pm.discovery.GetFixedPeers(), hashBitmap, req, PhaseFixed)
	if err == nil {
		return result, nil
	}

	pm.logger.Debug("All fixed peers failed",
		zap.String("hash", hash),
		zap.Error(err))

	// All phases failed
	return nil, fmt.Errorf("blob not found from any peer source")
}

// executePhase executes a specific phase with the given contacts
func (pm *DefaultPhaseManager) executePhase(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, phase PhaseType) ([]byte, error) {
	if len(contacts) == 0 {
		if phase == PhaseDHT {
			pm.logger.Debug("No DHT contacts available", zap.String("hash", hash))
		} else {
			pm.logger.Debug("No fixed peers available", zap.String("hash", hash))
		}
		return nil, fmt.Errorf("no peers available for phase %v", phase)
	}

	phaseName := "DHT"
	if phase == PhaseFixed {
		phaseName = "Fixed"
	}

	pm.logger.Debug("Starting phase",
		zap.String("phase", phaseName),
		zap.String("hash", hash),
		zap.Int("contacts", len(contacts)),
		zap.Int("fixed_peers", len(pm.discovery.GetFixedPeers())))

	// Limit number of peers to try based on phase
	var phaseContacts []dht.Contact
	if phase == PhaseDHT {
		// Limit DHT peers to maxPeers
		dhtPeersToTry := min(len(contacts), pm.maxPeers)
		phaseContacts = contacts[:dhtPeersToTry]
	} else {
		// Use all fixed peers
		phaseContacts = contacts
	}

	pm.logger.Debug("Phase: Trying peers",
		zap.String("phase", phaseName),
		zap.String("hash", hash),
		zap.Int("peers", len(phaseContacts)))

	// Determine if this is the final phase (completeOnFailure)
	completeOnFailure := phase == PhaseFixed || len(pm.discovery.GetFixedPeers()) == 0

	// Execute the race for this phase
	result, err := pm.raceCoordinator.ExecuteRace(ctx, hash, phaseContacts, hashBitmap, req, completeOnFailure)

	pm.logger.Debug("Phase completed",
		zap.String("phase", phaseName),
		zap.String("hash", hash),
		zap.Error(err))

	return result, err
}

// shouldTryFixedPeers determines if we should proceed to fixed peers phase
func (pm *DefaultPhaseManager) shouldTryFixedPeers(dhtContacts []dht.Contact) bool {
	// No fixed peers configured
	if len(pm.discovery.GetFixedPeers()) == 0 {
		return false
	}

	// Check if any of the DHT contacts were fixed peers
	for _, contact := range dhtContacts {
		if pm.discovery.IsFixedPeer(contact) {
			// Fixed peer was tried in DHT phase and failed, no need to retry
			return false
		}
	}

	return true
}

// IsStopped checks if the phase manager is stopped
func (pm *DefaultPhaseManager) IsStopped() bool {
	return pm.stopped == 1
}

// Stop stops the phase manager
func (pm *DefaultPhaseManager) Stop() {
	pm.stopped = 1
}

// Start starts the phase manager
func (pm *DefaultPhaseManager) Start() {
	pm.stopped = 0
}

// SetMaxPeers updates the maximum number of peers to try per phase
func (pm *DefaultPhaseManager) SetMaxPeers(maxPeers int) {
	pm.maxPeers = maxPeers
}

// GetRaceCoordinator returns the underlying race coordinator
func (pm *DefaultPhaseManager) GetRaceCoordinator() coordinator.PeerRaceCoordinator {
	return pm.raceCoordinator
}
