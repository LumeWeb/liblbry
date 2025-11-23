package peer_transfer

import (
	"context"
	"fmt"
	"time"

	"go.lumeweb.com/liblbry/blob/transfer"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/coordinator"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/downloader"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

const (
	// TransferName is the name of the peer transfer implementation
	TransferName = "peer"
)

// PeerTransfer is refactored version using separate components
// This implements the Transfer interface by orchestrating specialized components
type PeerTransfer struct {
	discovery   discovery.PeerDiscovery
	connMgr     connection.ConnectionManager
	coordinator coordinator.RequestCoordinator
	downloader  downloader.PeerDownloader
	logger      *zap.Logger
}

// PeerTransferOption configures the refactored peer transfer
type PeerTransferOption func(*PeerTransfer)

// NewPeerTransfer creates a new refactored PeerTransfer with the specified DHT node and peer client factory
func NewPeerTransfer(dhtNode protocol.DHTNode, peerClientFactory protocol.PeerClientFactory, options ...PeerTransferOption) (*PeerTransfer, error) {
	// Validate dhtNode at the start
	if dhtNode == nil {
		return nil, fmt.Errorf("dhtNode cannot be nil")
	}

	logger := zap.NewNop()

	// Create components with the configured logger first
	discovery := discovery.NewPeerDiscovery(dhtNode, logger)
	connMgr, err := connection.NewConnectionManager(peerClientFactory, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection manager: %w", err)
	}
	coordinator, err := coordinator.NewRequestCoordinator(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create request coordinator: %w", err)
	}
	downloader := downloader.NewPeerDownloaderWithDefaults(connMgr, discovery, coordinator, 5, 30*time.Second, logger)

	pt := &PeerTransfer{
		logger:      logger,
		discovery:   discovery,
		connMgr:     connMgr,
		coordinator: coordinator,
		downloader:  downloader,
	}

	// Apply options after components are initialized so options can access them
	for _, option := range options {
		option(pt)
	}

	return pt, nil
}

// Get attempts to fetch a blob from peers discovered via DHT using component architecture
func (pt *PeerTransfer) Get(ctx context.Context, hash string) ([]byte, error) {
	// Validate hash
	if !stream.ValidateHash(hash) {
		return nil, liblbryerrors.ErrInvalidHash
	}

	// Parse hash for DHT lookup
	hashBitmap, err := protocol.ParseHashFromString(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to parse hash for DHT lookup: %w", err)
	}

	// Get or create request (handles deduplication)
	req, isOwner, err := pt.coordinator.GetOrCreateRequest(hash)
	if err != nil {
		return nil, err
	}

	if !isOwner {
		// Join existing request
		return pt.coordinator.WaitForResult(ctx, req)
	}

	// This goroutine owns the request - ensure cleanup
	defer pt.coordinator.RemoveRequest(hash)

	// Download using the downloader component
	data, err := pt.downloader.DownloadFromPeers(ctx, hash, hashBitmap, req)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// Name returns the name of this transfer implementation
func (pt *PeerTransfer) Name() string {
	return TransferName
}

// Start initializes or restarts all components
func (pt *PeerTransfer) Start() {
	pt.logger.Debug("Starting PeerTransfer components")

	pt.discovery.Start()
	pt.connMgr.Start()
	pt.coordinator.Start()
	pt.downloader.Start()
}

// Stop gracefully shuts down all components
func (pt *PeerTransfer) Stop() {
	pt.logger.Debug("Stopping PeerTransfer components")

	pt.downloader.Stop()
	pt.coordinator.Stop()
	pt.connMgr.Stop()
	pt.discovery.Stop()
}

// Configuration Options

// WithPeerTransferLogger sets the logger for the main PeerTransfer
// Note: Component loggers are set in their constructors, so we only set the main logger here
func WithPeerTransferLogger(logger *zap.Logger) PeerTransferOption {
	return func(pt *PeerTransfer) {
		if logger != nil {
			pt.logger = logger
		}
	}
}

// WithPeerTransferTimeout sets the timeout for peer downloads
func WithPeerTransferTimeout(timeout time.Duration) PeerTransferOption {
	return func(pt *PeerTransfer) {
		pt.downloader.SetTimeout(timeout)
	}
}

// WithPeerTransferMaxPeers sets the maximum number of peers to try
func WithPeerTransferMaxPeers(maxPeers int) PeerTransferOption {
	return func(pt *PeerTransfer) {
		pt.downloader.SetMaxPeers(maxPeers)
	}
}

// WithPeerTransferFixedPeers sets fixed fallback peers
func WithPeerTransferFixedPeers(peerAddresses []string) PeerTransferOption {
	return func(pt *PeerTransfer) {
		if len(peerAddresses) == 0 {
			pt.logger.Debug("No fixed peers provided, using empty list")
			pt.discovery.SetFixedPeers(nil)
			return
		}

		contacts, err := pt.discovery.ParseFixedPeers(peerAddresses)
		if err != nil {
			pt.logger.Warn("Failed to parse fixed peer addresses, using empty list",
				zap.Strings("addresses", peerAddresses),
				zap.Error(err))
			pt.discovery.SetFixedPeers(nil)
			return
		}

		pt.discovery.SetFixedPeers(contacts)
		pt.logger.Info("Configured fixed peers as fallback",
			zap.Strings("addresses", peerAddresses),
			zap.Int("parsed_contacts", len(contacts)))
	}
}

// WithPeerTransferRetryConfig sets retry configuration for DHT discovery
func WithPeerTransferRetryConfig(attempts int, delay time.Duration) PeerTransferOption {
	return func(pt *PeerTransfer) {
		pt.discovery.SetRetryConfig(attempts, delay)
	}
}

// WithPeerTransferDHTRetryAttempts sets the number of DHT retry attempts
func WithPeerTransferDHTRetryAttempts(attempts int) PeerTransferOption {
	return func(pt *PeerTransfer) {
		currentDelay := pt.discovery.RetryDelay()
		pt.discovery.SetRetryConfig(attempts, currentDelay)
	}
}

// WithPeerTransferDHTRetryDelay sets the delay between DHT retry attempts
func WithPeerTransferDHTRetryDelay(delay time.Duration) PeerTransferOption {
	return func(pt *PeerTransfer) {
		currentAttempts := pt.discovery.RetryAttempts()
		pt.discovery.SetRetryConfig(currentAttempts, delay)
	}
}

// WithPeerTransferMaxConcurrency sets the maximum concurrency for peer tasks
func WithPeerTransferMaxConcurrency(maxConcurrency int) PeerTransferOption {
	return func(pt *PeerTransfer) {
		pt.downloader.SetMaxConcurrency(maxConcurrency)
	}
}

// Adapter to make PeerTransfer compatible with existing TransferOption interface

// ErrorTransferOption implements TransferOption interface for handling adapter creation errors
type ErrorTransferOption struct {
	err error
}

// Apply returns the error that occurred during option creation
func (e *ErrorTransferOption) Apply(transfer any) error {
	return e.err
}

// PeerTransferOptionAdapter wraps PeerTransferOption to implement TransferOption interface
type PeerTransferOptionAdapter struct {
	option PeerTransferOption
}

// NewPeerTransferOptionAdapter creates a new adapter from a PeerTransferOption
func NewPeerTransferOptionAdapter(option PeerTransferOption) (*PeerTransferOptionAdapter, error) {
	if option == nil {
		return nil, fmt.Errorf("PeerTransferOption cannot be nil")
	}
	return &PeerTransferOptionAdapter{option: option}, nil
}

// Apply applies the wrapped PeerTransferOption to a PeerTransfer instance
func (a *PeerTransferOptionAdapter) Apply(transfer any) error {
	if a.option == nil {
		return fmt.Errorf("PeerTransferOptionAdapter has nil option")
	}

	peerTransfer, ok := transfer.(*PeerTransfer)
	if !ok {
		return fmt.Errorf("expected *PeerTransfer, got %T", transfer)
	}
	a.option(peerTransfer)
	return nil
}

// Convenience functions for creating TransferOption instances

// WithPeerTransferLoggerOption creates a TransferOption for setting logger
func WithPeerTransferLoggerOption(logger *zap.Logger) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferLogger(logger))
	if err != nil {
		// Return a no-op option that logs the error instead of panicking
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer logger option: %w", err)}
	}
	return adapter
}

// WithPeerTransferTimeoutOption creates a TransferOption for setting timeout
func WithPeerTransferTimeoutOption(timeout time.Duration) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferTimeout(timeout))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer timeout option: %w", err)}
	}
	return adapter
}

// WithPeerTransferMaxPeersOption creates a TransferOption for setting max peers
func WithPeerTransferMaxPeersOption(maxPeers int) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferMaxPeers(maxPeers))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer max peers option: %w", err)}
	}
	return adapter
}

// WithPeerTransferFixedPeersOption creates a TransferOption for setting fixed peers
func WithPeerTransferFixedPeersOption(peerAddresses []string) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferFixedPeers(peerAddresses))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer fixed peers option: %w", err)}
	}
	return adapter
}

// WithPeerTransferRetryConfigOption creates a TransferOption for setting retry config
func WithPeerTransferRetryConfigOption(attempts int, delay time.Duration) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferRetryConfig(attempts, delay))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer retry config option: %w", err)}
	}
	return adapter
}

// WithPeerTransferDHTRetryAttemptsOption creates a TransferOption for setting DHT retry attempts
func WithPeerTransferDHTRetryAttemptsOption(attempts int) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferDHTRetryAttempts(attempts))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer DHT retry attempts option: %w", err)}
	}
	return adapter
}

// WithPeerTransferDHTRetryDelayOption creates a TransferOption for setting DHT retry delay
func WithPeerTransferDHTRetryDelayOption(delay time.Duration) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferDHTRetryDelay(delay))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer DHT retry delay option: %w", err)}
	}
	return adapter
}

// WithPeerTransferMaxConcurrencyOption creates a TransferOption for setting max concurrency
func WithPeerTransferMaxConcurrencyOption(maxConcurrency int) transfer.TransferOption {
	adapter, err := NewPeerTransferOptionAdapter(WithPeerTransferMaxConcurrency(maxConcurrency))
	if err != nil {
		return &ErrorTransferOption{err: fmt.Errorf("failed to create peer transfer max concurrency option: %w", err)}
	}
	return adapter
}
