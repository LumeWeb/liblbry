package transfer

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammazero/workerpool"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// TaskResult represents the result of a peer download attempt
type TaskResult struct {
	data []byte
	err  error
}

// BlobRequest tracks an in-progress blob download with race coordination
type BlobRequest struct {
	resultChan chan TaskResult
	cancel     context.CancelFunc
	waiters    int
	completed  int32 // atomic counter for completed peers
	mu         sync.Mutex
}

// PeerTransfer fetches blobs from LBRY peers using DHT discovery
type PeerTransfer struct {
	dhtNode           protocol.DHTNode
	peerClientFactory protocol.PeerClientFactory
	timeout           time.Duration
	maxPeers          int
	logger            *zap.Logger

	// Concurrent execution fields
	workerPool     *workerpool.WorkerPool
	backlog        map[string]*BlobRequest
	backlogMu      sync.RWMutex
	maxConcurrency int
	clientPool     *sync.Pool
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
		if logger != nil {
			t.logger = logger
		}
	}
}

// WithPeerTransferMaxConcurrency sets the maximum number of concurrent peer connections
func WithPeerTransferMaxConcurrency(maxConcurrency int) PeerTransferOption {
	return func(t *PeerTransfer) {
		if maxConcurrency < 1 {
			maxConcurrency = 5
		}
		t.maxConcurrency = maxConcurrency
	}
}

// NewPeerTransfer creates a new PeerTransfer with the specified DHT node and peer client factory
func NewPeerTransfer(dhtNode protocol.DHTNode, peerClientFactory protocol.PeerClientFactory, options ...PeerTransferOption) *PeerTransfer {
	transfer := &PeerTransfer{
		dhtNode:           dhtNode,
		peerClientFactory: peerClientFactory,
		timeout:           30 * time.Second,
		maxPeers:          5,
		logger:            zap.NewNop(),
		maxConcurrency:    5,
		backlog:           make(map[string]*BlobRequest),
		clientPool: &sync.Pool{
			New: func() interface{} {
				return peerClientFactory()
			},
		},
	}

	for _, option := range options {
		option(transfer)
	}

	// Initialize worker pool after options are applied
	transfer.workerPool = workerpool.New(transfer.maxConcurrency)

	return transfer
}

// getFromBacklog checks if a blob download is already in progress and returns the existing request
func (t *PeerTransfer) getFromBacklog(hash string) *BlobRequest {
	t.backlogMu.RLock()
	defer t.backlogMu.RUnlock()

	if req, exists := t.backlog[hash]; exists {
		req.mu.Lock()
		req.waiters++
		req.mu.Unlock()
		return req
	}
	return nil
}

// addToBacklog adds a new blob request to the backlog
func (t *PeerTransfer) addToBacklog(hash string, req *BlobRequest) {
	t.backlogMu.Lock()
	defer t.backlogMu.Unlock()
	t.backlog[hash] = req
}

// removeFromBacklog removes a completed blob request from the backlog
func (t *PeerTransfer) removeFromBacklog(hash string) {
	t.backlogMu.Lock()
	defer t.backlogMu.Unlock()
	delete(t.backlog, hash)
}

// returnClientToPool resets the client and returns it to the pool
func (t *PeerTransfer) returnClientToPool(peerClient protocol.PeerClient) {
	if resetErr := peerClient.Reset(); resetErr != nil {
		t.logger.Debug("Failed to reset peer client", zap.Error(resetErr))
	}
	t.clientPool.Put(peerClient)
}

// wait waits for the blob request to complete and returns the result
func (br *BlobRequest) wait(ctx context.Context) ([]byte, error) {
	defer func() {
		br.mu.Lock()
		br.waiters--
		br.mu.Unlock()
	}()

	for {
		select {
		case result := <-br.resultChan:
			// If we got a successful result, return it immediately
			if result.err == nil {
				return result.data, nil
			}
			// If we got an error result, check if all peers have completed
			// and if so, return the error
			completed := atomic.LoadInt32(&br.completed)
			if completed > 0 {
				// Check if we've received results from all waiters
				br.mu.Lock()
				waiters := br.waiters
				br.mu.Unlock()

				if completed >= int32(waiters) {
					return nil, result.err
				}
			}
			// Continue waiting for other results (loop again)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// downloadFromPeer attempts to download a blob from a specific peer
func (t *PeerTransfer) downloadFromPeer(ctx context.Context, peerAddr, hash string) ([]byte, error) {
	// Get a peer client from the pool
	peerClient := t.clientPool.Get().(protocol.PeerClient)

	// Attempt to connect and download
	if err := peerClient.Connect(ctx, peerAddr); err != nil {
		// Return client to pool even on error
		t.returnClientToPool(peerClient)
		// Preserve context errors without wrapping
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("connect to peer %s: %w", peerAddr, err)
	}

	defer func() {
		// Return client to pool when done
		t.returnClientToPool(peerClient)
	}()

	data, err := peerClient.GetBlob(ctx, hash)
	if err != nil {
		// Preserve context errors without wrapping
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("fetch blob from peer %s: %w", peerAddr, err)
	}

	return data, nil
}

// Get attempts to fetch a blob from peers discovered via DHT using concurrent race pattern
func (t *PeerTransfer) Get(ctx context.Context, hash string) ([]byte, error) {
	// Validate hash
	if !stream.ValidateHash(hash) {
		return nil, liblbryerrors.ErrInvalidHash
	}

	// Check if this blob is already being downloaded
	if req := t.getFromBacklog(hash); req != nil {
		t.logger.Debug("Joining existing blob download", zap.String("hash", hash))
		return req.wait(ctx)
	}

	// Create race context for cancellation
	raceCtx, raceCancel := context.WithTimeout(ctx, t.timeout)

	resultChan := make(chan TaskResult, 1)
	req := &BlobRequest{
		resultChan: resultChan,
		cancel:     raceCancel,
		waiters:    1,
	}

	// Add to backlog before starting downloads
	t.addToBacklog(hash, req)
	defer t.removeFromBacklog(hash)

	// Discover peers via DHT
	hashBitmap, err := protocol.ParseHashFromString(hash)
	if err != nil {
		raceCancel()
		return nil, fmt.Errorf("failed to parse hash for DHT lookup: %w", err)
	}

	contacts, err := t.dhtNode.Get(hashBitmap)
	if err != nil {
		raceCancel()
		return nil, fmt.Errorf("failed to discover peers via DHT: %w", err)
	}

	if len(contacts) == 0 {
		raceCancel()
		return nil, liblbryerrors.ErrBlobNotFound
	}

	// Limit number of peers to try
	peersToTry := min(len(contacts), t.maxPeers)
	t.logger.Debug("Starting concurrent peer race",
		zap.String("hash", hash),
		zap.Int("total_peers", len(contacts)),
		zap.Int("peers_to_try", peersToTry))

	// Submit ALL peer tasks to worker pool simultaneously
	for i := 0; i < peersToTry; i++ {
		contact := contacts[i]
		peerAddr := contact.Addr().String()

		// Capture loop variables
		peerAddrCopy := peerAddr
		hashCopy := hash

		t.workerPool.Submit(func() {
			t.logger.Debug("Attempting peer download",
				zap.String("hash", hashCopy),
				zap.String("peer", peerAddrCopy))

			data, err := t.downloadFromPeer(raceCtx, peerAddrCopy, hashCopy)

			if err == nil {
				// SUCCESS! Cancel all other peer tasks
				select {
				case resultChan <- TaskResult{data: data}:
					t.logger.Debug("Peer download succeeded, canceling others",
						zap.String("hash", hashCopy),
						zap.String("peer", peerAddrCopy))
					raceCancel() // This cancels all other in-flight tasks
				default:
					// Another peer already won the race
					t.logger.Debug("Peer download succeeded but race already won",
						zap.String("hash", hashCopy),
						zap.String("peer", peerAddrCopy))
				}
			} else {
				// Track failed peer and send error result
				atomic.AddInt32(&req.completed, 1)
				t.logger.Debug("Peer download failed",
					zap.String("hash", hashCopy),
					zap.String("peer", peerAddrCopy),
					zap.Error(err))

				// Send error result to indicate failure
				select {
				case resultChan <- TaskResult{err: err}:
					t.logger.Debug("Sent error result for failed peer",
						zap.String("hash", hashCopy),
						zap.String("peer", peerAddrCopy))
				default:
					// Result already sent or channel closed
				}
			}
		})
	}

	// Wait for first success or timeout
	data, err := req.wait(raceCtx)

	// Ensure race context is cancelled
	raceCancel()

	return data, err
}

// Name returns the name of this transfer implementation
func (t *PeerTransfer) Name() string {
	return "peer"
}

// Stop gracefully shuts down the peer transfer and its worker pool
func (t *PeerTransfer) Stop() {
	if t.workerPool != nil {
		t.workerPool.Stop()
	}

	// Clear the client pool
	if t.clientPool != nil {
		// Clear the pool by draining it
		for {
			client := t.clientPool.Get()
			if client == nil {
				break
			}
			// Reset and discard the client
			if resetErr := client.(protocol.PeerClient).Reset(); resetErr != nil {
				t.logger.Debug("Failed to reset client during shutdown", zap.Error(resetErr))
			}
		}
	}
}
