package phase

import (
	"context"
	"fmt"
	"sync/atomic"

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

// PhaseConfig defines the configuration and behavior for a specific phase
type PhaseConfig struct {
	Name          string
	GetContacts   func(ctx context.Context, pm *DefaultPhaseManager, hashBitmap bits.Bitmap) ([]dht.Contact, error)
	IsFallback    bool
	MaxContacts   func(pm *DefaultPhaseManager) int
	ShouldExecute func(pm *DefaultPhaseManager, contacts []dht.Contact) bool
	ResetForPhase func(pm *DefaultPhaseManager, req *blob.BlobRequest, contacts []dht.Contact)
}

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

	// GetMaxPeers returns the current maximum number of peers to try per phase
	GetMaxPeers() int32

	// GetRaceCoordinator returns the underlying race coordinator
	GetRaceCoordinator() coordinator.PeerRaceCoordinator

	// SetLogger updates the logger for this phase manager instance
	SetLogger(logger *zap.Logger)
}

// DefaultPhaseManager manages the execution of different download phases
//
// NOTE: All fields are accessed concurrently and must be protected:
// - maxPeers: accessed via atomic operations (LoadInt32/StoreInt32)
// - stopped: accessed via atomic operations (LoadInt32/StoreInt32)
type DefaultPhaseManager struct {
	discovery       discovery.PeerDiscovery
	raceCoordinator coordinator.PeerRaceCoordinator
	maxPeers        int32 // accessed atomically
	logger          *zap.Logger
	stopped         int32 // accessed atomically (0 = running, 1 = stopped)
	phases          map[PhaseType]*PhaseConfig
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

	pm := &DefaultPhaseManager{
		discovery:       discovery,
		raceCoordinator: raceCoordinator,
		maxPeers:        int32(maxPeers),
		logger:          logger,
		stopped:         0, // 0 = not stopped, 1 = stopped
		phases:          make(map[PhaseType]*PhaseConfig),
	}

	pm.initializePhases()
	return pm
}

// initializePhases sets up the phase configurations
func (pm *DefaultPhaseManager) initializePhases() {
	pm.phases[PhaseDHT] = &PhaseConfig{
		Name: "DHT",
		GetContacts: func(ctx context.Context, pm *DefaultPhaseManager, hashBitmap bits.Bitmap) ([]dht.Contact, error) {
			contacts, err := pm.discovery.DiscoverPeers(ctx, hashBitmap)
			if err != nil {
				return nil, err
			}

			// Filter out fixed peers from DHT results to avoid duplication
			var filteredContacts []dht.Contact
			for _, contact := range contacts {
				if !pm.discovery.IsFixedPeer(contact) {
					filteredContacts = append(filteredContacts, contact)
				}
			}

			return filteredContacts, nil
		},
		IsFallback: false,
		MaxContacts: func(pm *DefaultPhaseManager) int {
			return int(atomic.LoadInt32(&pm.maxPeers))
		},
		ShouldExecute: func(pm *DefaultPhaseManager, contacts []dht.Contact) bool {
			return len(contacts) > 0
		},
		ResetForPhase: func(pm *DefaultPhaseManager, req *blob.BlobRequest, contacts []dht.Contact) {
			pm.raceCoordinator.GetCoordinator().ResetForNewPhase(req, int32(len(contacts)))
		},
	}

	pm.phases[PhaseFixed] = &PhaseConfig{
		Name: "Fixed",
		GetContacts: func(ctx context.Context, pm *DefaultPhaseManager, hashBitmap bits.Bitmap) ([]dht.Contact, error) {
			return pm.discovery.GetFixedPeers(), nil
		},
		IsFallback: true,
		MaxContacts: func(pm *DefaultPhaseManager) int {
			return len(pm.discovery.GetFixedPeers())
		},
		ShouldExecute: func(pm *DefaultPhaseManager, contacts []dht.Contact) bool {
			return len(contacts) > 0
		},
		ResetForPhase: func(pm *DefaultPhaseManager, req *blob.BlobRequest, contacts []dht.Contact) {
			pm.raceCoordinator.GetCoordinator().ResetForNewPhase(req, int32(len(contacts)))
		},
	}
}

// ExecutePhases executes the download phases using the configured phase map
func (pm *DefaultPhaseManager) ExecutePhases(ctx context.Context, hash string, hashBitmap bits.Bitmap, req *blob.BlobRequest) ([]byte, error) {
	if pm.IsStopped() {
		return nil, fmt.Errorf("phase manager is stopped")
	}

	var lastError error

	// Execute phases in order
	for phaseType := PhaseDHT; phaseType <= PhaseFixed; phaseType++ {
		phaseConfig, exists := pm.phases[phaseType]
		if !exists {
			pm.logger.Warn("Phase configuration not found", zap.Int("phase", int(phaseType)))
			continue
		}

		// Get contacts for this phase
		contacts, err := phaseConfig.GetContacts(ctx, pm, hashBitmap)
		if err != nil {
			if phaseType == PhaseDHT {
				pm.logger.Warn("DHT discovery failed, falling back to fixed peers if available",
					zap.String("hash", hash),
					zap.Error(err),
				)
				contacts = nil
			} else {
				pm.logger.Error("Failed to get contacts for phase",
					zap.String("phase", phaseConfig.Name),
					zap.String("hash", hash),
					zap.Error(err))
				continue
			}
		}

		// Check if we should execute this phase
		if !phaseConfig.ShouldExecute(pm, contacts) {
			if phaseType == PhaseDHT {
				pm.logger.Debug("No DHT contacts available", zap.String("hash", hash))
			} else {
				pm.logger.Debug("No fixed peers available", zap.String("hash", hash))
			}
			lastError = fmt.Errorf("no peers available for phase %s", phaseConfig.Name)
			continue
		}

		// For fixed peers phase, check if we should try it based on DHT results
		if phaseType == PhaseFixed && !pm.shouldTryFixedPeers() {
			continue
		}

		// Reset completion tracking for this phase
		phaseConfig.ResetForPhase(pm, req, contacts)

		pm.logger.Debug("Starting phase",
			zap.String("phase", phaseConfig.Name),
			zap.String("hash", hash),
			zap.Int("contacts", len(contacts)),
			zap.Int("fixed_peers", len(pm.discovery.GetFixedPeers())))

		// Execute the phase
		result, err := pm.executePhaseWithConfig(ctx, hash, contacts, hashBitmap, req, phaseConfig)
		if err == nil {
			return result, nil
		}

		lastError = err
		pm.logger.Debug("Phase completed",
			zap.String("phase", phaseConfig.Name),
			zap.String("hash", hash),
			zap.Error(err))
	}

	// All phases failed
	if lastError == nil {
		return nil, fmt.Errorf("blob not found from any peer source: no peers available")
	}
	return nil, fmt.Errorf("blob not found from any peer source: %v", lastError)
}

// executePhaseWithConfig executes a specific phase using its configuration
func (pm *DefaultPhaseManager) executePhaseWithConfig(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, phaseConfig *PhaseConfig) ([]byte, error) {
	// Limit number of peers to try based on phase configuration
	maxContacts := phaseConfig.MaxContacts(pm)
	// Guard against invalid maxContacts values (0 or negative)
	if maxContacts <= 0 {
		pm.logger.Debug("Phase: Invalid maxContacts value, skipping phase",
			zap.String("phase", phaseConfig.Name),
			zap.String("hash", hash),
			zap.Int("maxContacts", maxContacts))
		return nil, fmt.Errorf("invalid maxContacts value %d for phase %s", maxContacts, phaseConfig.Name)
	}
	phaseContacts := contacts
	if len(contacts) > maxContacts {
		phaseContacts = contacts[:maxContacts]
	}

	pm.logger.Debug("Phase: Trying peers",
		zap.String("phase", phaseConfig.Name),
		zap.String("hash", hash),
		zap.Int("peers", len(phaseContacts)))

	// Determine if this is the final phase (completeOnFailure)
	completeOnFailure := phaseConfig.IsFallback || len(pm.discovery.GetFixedPeers()) == 0

	// Execute the race for this phase
	result, err := pm.raceCoordinator.ExecuteRace(ctx, hash, phaseContacts, hashBitmap, req, completeOnFailure)

	return result, err
}

// shouldTryFixedPeers determines if we should proceed to fixed peers phase
func (pm *DefaultPhaseManager) shouldTryFixedPeers() bool {
	fixed := pm.discovery.GetFixedPeers()
	if len(fixed) == 0 {
		return false
	}

	// If we have any fixed peers configured, we should try the fixed-peer phase
	// since DHT contacts are now filtered to exclude fixed peers
	return len(fixed) > 0
}

// IsStopped checks if the phase manager is stopped
func (pm *DefaultPhaseManager) IsStopped() bool {
	return atomic.LoadInt32(&pm.stopped) == 1
}

// Stop stops the phase manager
func (pm *DefaultPhaseManager) Stop() {
	atomic.StoreInt32(&pm.stopped, 1)
}

// Start starts the phase manager
func (pm *DefaultPhaseManager) Start() {
	atomic.StoreInt32(&pm.stopped, 0)
}

// SetLogger updates the logger for this phase manager instance and propagates to subcomponents
func (pm *DefaultPhaseManager) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	pm.logger = logger
	// Propagate logger to subcomponents
	pm.discovery.SetLogger(logger)
	pm.raceCoordinator.SetLogger(logger)
}

// SetMaxPeers updates the maximum number of peers to try per phase
func (pm *DefaultPhaseManager) SetMaxPeers(maxPeers int) {
	atomic.StoreInt32(&pm.maxPeers, int32(maxPeers))
}

// GetMaxPeers returns the current maximum number of peers to try per phase
func (pm *DefaultPhaseManager) GetMaxPeers() int32 {
	return atomic.LoadInt32(&pm.maxPeers)
}

// GetRaceCoordinator returns the underlying race coordinator
func (pm *DefaultPhaseManager) GetRaceCoordinator() coordinator.PeerRaceCoordinator {
	return pm.raceCoordinator
}
