package downloader

import (
	"context"
	"fmt"
	"sync/atomic"
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
	stopped      int32
}

// NewPeerDownloader creates a new DefaultPeerDownloader instance
func NewPeerDownloader(
	connMgr connection.ConnectionManager,
	discovery discovery.PeerDiscovery,
	coordinator coordinator2.RequestCoordinator,
	phaseManager phase.PhaseManager,
	maxPeers int,
	timeout time.Duration,
	logger *zap.Logger,
) *DefaultPeerDownloader {
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

// NewPeerDownloaderWithDefaults creates a new DefaultPeerDownloader with default component implementations
func NewPeerDownloaderWithDefaults(
	connMgr connection.ConnectionManager,
	discovery discovery.PeerDiscovery,
	coordinator coordinator2.RequestCoordinator,
	maxPeers int,
	timeout time.Duration,
	logger *zap.Logger,
) *DefaultPeerDownloader {
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
	return atomic.LoadInt32(&pd.stopped) == 1
}

// Stop stops the downloader and its components
func (pd *DefaultPeerDownloader) Stop() {
	atomic.StoreInt32(&pd.stopped, 1)
	pd.phaseManager.Stop()
}

// Start starts the downloader and its components
func (pd *DefaultPeerDownloader) Start() {
	atomic.StoreInt32(&pd.stopped, 0)
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

// SetMaxPeers updates the maximum number of peers to try
func (pd *DefaultPeerDownloader) SetMaxPeers(maxPeers int) {
	pd.maxPeers = maxPeers
	// Update phase manager max peers
	pd.phaseManager.SetMaxPeers(maxPeers)
	// Update task executor max concurrency through the race coordinator
	pd.phaseManager.GetRaceCoordinator().GetTaskExecutor().SetMaxConcurrency(maxPeers)
}

// SetMaxConcurrency updates the maximum concurrency for peer tasks
func (pd *DefaultPeerDownloader) SetMaxConcurrency(maxConcurrency int) {
	// Update task executor max concurrency through the race coordinator
	pd.phaseManager.GetRaceCoordinator().GetTaskExecutor().SetMaxConcurrency(maxConcurrency)
}
