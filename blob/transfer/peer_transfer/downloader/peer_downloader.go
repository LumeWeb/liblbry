package downloader

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection"
	coordinator2 "go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/executor"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/phase"
	"go.uber.org/zap"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
)

// PeerDownloader defines the interface for peer download operations
type PeerDownloader interface {
	IsStopped() bool
	Stop()
	Start()
	DownloadFromPeers(ctx context.Context, hash string, hashBitmap bits.Bitmap, req *blob.BlobRequest) ([]byte, error)
	SetTimeout(timeout time.Duration)
	SetMaxPeers(maxPeers int)
	SetMaxConcurrency(maxConcurrency int)
	GetTimeout() time.Duration
	GetMaxPeers() int
}

// DefaultPeerDownloader orchestrates peer downloads using specialized components
type DefaultPeerDownloader struct {
	connMgr      connection.ConnectionManager
	discovery    discovery.PeerDiscovery
	coordinator  coordinator2.RequestCoordinator
	phaseManager phase.PhaseManager
	maxPeers     int
	timeout      time.Duration
	logger       *zap.Logger
}

// NewPeerDownloader creates a new DefaultPeerDownloader instance.
// All dependency parameters must be non-nil. This constructor is intended for internal use
// where components are pre-configured. For most use cases, use NewPeerDownloaderWithDefaults.
func NewPeerDownloader(
	connMgr connection.ConnectionManager,
	discovery discovery.PeerDiscovery,
	coordinator coordinator2.RequestCoordinator,
	phaseManager phase.PhaseManager,
	maxPeers int,
	timeout time.Duration,
	logger *zap.Logger,
) *DefaultPeerDownloader {
	// Validate required dependencies
	if connMgr == nil {
		panic("connection manager cannot be nil")
	}
	if discovery == nil {
		panic("peer discovery cannot be nil")
	}
	if coordinator == nil {
		panic("request coordinator cannot be nil")
	}
	if phaseManager == nil {
		panic("phase manager cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	pd := &DefaultPeerDownloader{
		connMgr:      connMgr,
		discovery:    discovery,
		coordinator:  coordinator,
		phaseManager: phaseManager,
		maxPeers:     maxPeers,
		timeout:      timeout,
		logger:       logger,
	}

	return pd
}

// NewPeerDownloaderWithDefaults creates a new DefaultPeerDownloader with default component implementations.
// All dependency parameters must be non-nil. This is the recommended constructor for most use cases.
func NewPeerDownloaderWithDefaults(
	connMgr connection.ConnectionManager,
	discovery discovery.PeerDiscovery,
	coordinator coordinator2.RequestCoordinator,
	maxPeers int,
	timeout time.Duration,
	logger *zap.Logger,
) *DefaultPeerDownloader {
	// Validate required dependencies
	if connMgr == nil {
		panic("connection manager cannot be nil")
	}
	if discovery == nil {
		panic("peer discovery cannot be nil")
	}
	if coordinator == nil {
		panic("request coordinator cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create component dependencies
	taskExecutor := executor.NewPeerTaskExecutor(connMgr, discovery, coordinator, maxPeers, logger)
	raceCoordinator := coordinator2.NewPeerRaceCoordinator(connMgr, discovery, coordinator, taskExecutor, timeout, logger)
	phaseManager := phase.NewPhaseManager(discovery, raceCoordinator, maxPeers, logger)

	return NewPeerDownloader(connMgr, discovery, coordinator, phaseManager, maxPeers, timeout, logger)
}

// IsStopped checks if downloader is stopped
func (pd *DefaultPeerDownloader) IsStopped() bool {
	return pd.phaseManager.IsStopped()
}

// Stop stops the downloader and its components
func (pd *DefaultPeerDownloader) Stop() {
	pd.phaseManager.Stop()
}

// Start starts the downloader and its components
func (pd *DefaultPeerDownloader) Start() {
	pd.phaseManager.Start()
}

// DownloadFromPeers attempts to download from multiple peer sources (DHT then fixed peers)
// This is the main entry point for downloading a blob
func (pd *DefaultPeerDownloader) DownloadFromPeers(ctx context.Context, hash string, hashBitmap bits.Bitmap, req *blob.BlobRequest) ([]byte, error) {
	if pd.IsStopped() {
		return nil, fmt.Errorf("peer downloader is stopped")
	}

	// Delegate to phase manager for execution
	return pd.phaseManager.ExecutePhases(ctx, hash, hashBitmap, req)
}

// SetTimeout updates the download timeout
func (pd *DefaultPeerDownloader) SetTimeout(timeout time.Duration) {
	pd.timeout = timeout
	// Update race coordinator timeout through the phase manager
	pd.phaseManager.GetRaceCoordinator().SetTimeout(timeout)
}

// GetTimeout returns the current download timeout
func (pd *DefaultPeerDownloader) GetTimeout() time.Duration {
	return pd.timeout
}

// GetMaxPeers returns the maximum number of peers to try
func (pd *DefaultPeerDownloader) GetMaxPeers() int {
	return pd.maxPeers
}

// SetMaxPeers updates the maximum number of peers to try per phase.
// This method couples peer limits with task execution concurrency by setting both:
// - Phase manager's maxPeers (limits peers tried per download phase)
// - Task executor's maxConcurrency (limits concurrent peer tasks)
//
// Use SetMaxConcurrency() if you need to decouple these values and control
// concurrency independently from the peer limit per phase.
func (pd *DefaultPeerDownloader) SetMaxPeers(maxPeers int) {
	pd.maxPeers = maxPeers
	// Update phase manager max peers
	pd.phaseManager.SetMaxPeers(maxPeers)
	// Update task executor max concurrency through the race coordinator
	pd.phaseManager.GetRaceCoordinator().GetTaskExecutor().SetMaxConcurrency(maxPeers)
}

// SetMaxConcurrency updates the maximum concurrency for peer tasks independently.
// This method only affects the task executor's worker pool concurrency and does not
// change the maximum number of peers tried per phase (controlled by SetMaxPeers).
//
// Use this when you want to decouple task concurrency from peer limits, such as:
// - Allowing more concurrent tasks than peers per phase (for retry scenarios)
// - Limiting concurrency for resource management while keeping peer limits high
func (pd *DefaultPeerDownloader) SetMaxConcurrency(maxConcurrency int) {
	// Update task executor max concurrency through the race coordinator
	pd.phaseManager.GetRaceCoordinator().GetTaskExecutor().SetMaxConcurrency(maxConcurrency)
}
