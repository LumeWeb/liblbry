package transfer

import (
	"context"
	"fmt"
	"time"

	lbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// PeerTransfer fetches blobs from LBRY peers using DHT discovery
type PeerTransfer struct {
	dhtNode    protocol.DHTNode
	peerClient protocol.PeerClient
	timeout    time.Duration
	maxPeers   int
	logger     *zap.Logger
}

// PeerTransferOption configures the peer transfer
type PeerTransferOption func(*PeerTransfer)

// WithPeerTransferTimeout sets the timeout for peer transfers
func WithPeerTransferTimeout(timeout time.Duration) PeerTransferOption {
	return func(t *PeerTransfer) {
		t.timeout = timeout
	}
}

// WithPeerTransferMaxPeers sets the maximum number of peers to try
func WithPeerTransferMaxPeers(maxPeers int) PeerTransferOption {
	return func(t *PeerTransfer) {
		t.maxPeers = maxPeers
	}
}

// WithPeerTransferLogger sets the logger for peer transfer
func WithPeerTransferLogger(logger *zap.Logger) PeerTransferOption {
	return func(t *PeerTransfer) {
		t.logger = logger
	}
}

// NewPeerTransfer creates a new PeerTransfer with the specified DHT node and peer client
func NewPeerTransfer(dhtNode protocol.DHTNode, peerClient protocol.PeerClient, options ...PeerTransferOption) *PeerTransfer {
	transfer := &PeerTransfer{
		dhtNode:    dhtNode,
		peerClient: peerClient,
		timeout:    30 * time.Second,
		maxPeers:   5,
		logger:     zap.NewNop(),
	}

	for _, option := range options {
		option(transfer)
	}

	return transfer
}

// Get attempts to fetch a blob from peers discovered via DHT
func (t *PeerTransfer) Get(hash string) ([]byte, error) {
	// Validate hash
	if !stream.ValidateHash(hash) {
		return nil, lbryerrors.ErrInvalidHash
	}

	// Discover peers via DHT
	// Convert hash to bitmap for DHT lookup
	hashBitmap, err := protocol.ParseHashFromString(hash)
	if err != nil {
		return nil, fmt.Errorf("failed to parse hash for DHT lookup: %w", err)
	}
	contacts, err := t.dhtNode.Get(hashBitmap)
	if err != nil {
		return nil, fmt.Errorf("failed to discover peers via DHT: %w", err)
	}

	if len(contacts) == 0 {
		return nil, lbryerrors.ErrBlobNotFound
	}

	// Try up to maxPeers peers
	ctx, cancel := context.WithTimeout(context.Background(), t.timeout)
	defer cancel()

	var lastErr error

	for i, contact := range contacts {
		if i >= t.maxPeers {
			break
		}

		peerAddr := contact.Addr().String()

		if err := t.peerClient.Connect(ctx, peerAddr); err != nil {
			lastErr = fmt.Errorf("connect to peer %s: %w", peerAddr, err)
			t.logger.Debug("Failed to connect to peer",
				zap.String("hash", hash),
				zap.String("peer", peerAddr),
				zap.Error(err))
			continue
		}

		// Attempt to download blob from peer
		data, err := t.peerClient.GetBlob(ctx, hash)
		if err == nil {
			// Success!
			t.logger.Debug("Successfully fetched blob from peer",
				zap.String("hash", hash),
				zap.String("peer", peerAddr))
			if closeErr := t.peerClient.Close(); closeErr != nil {
				t.logger.Debug("Failed to close peer connection",
					zap.String("hash", hash),
					zap.String("peer", peerAddr),
					zap.Error(closeErr))
			}
			return data, nil
		}

		lastErr = fmt.Errorf("fetch blob from peer %s: %w", peerAddr, err)
		t.logger.Debug("Failed to fetch blob from peer",
			zap.String("hash", hash),
			zap.String("peer", peerAddr),
			zap.Error(err))
		if closeErr := t.peerClient.Close(); closeErr != nil {
			t.logger.Debug("Failed to close peer connection",
				zap.String("hash", hash),
				zap.String("peer", peerAddr),
				zap.Error(closeErr))
		}
	}

	// All attempts failed
	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch blob from %d peers: %w", len(contacts), lastErr)
	}
	return nil, lbryerrors.ErrAcquisitionFailed
}

// Name returns the name of this transfer implementation
func (t *PeerTransfer) Name() string {
	return "peer"
}
