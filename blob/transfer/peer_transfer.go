package transfer

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammazero/workerpool"
	"go.lumeweb.com/lbry-dht"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

const (
	// DefaultMaxConcurrency is the default number of concurrent peer connections
	DefaultMaxConcurrency = 5
)

// taskResult represents the result of a peer download attempt
type taskResult struct {
	data []byte
	err  error
}

// blobRequest tracks an in-progress blob download with race coordination
type blobRequest struct {
	cancel     context.CancelFunc // Cancels the race context for all peer attempts
	waiters    int
	completed  int32 // number of completed peer attempts
	totalPeers int32 // total number of peer attempts scheduled
	result     taskResult
	done       chan struct{}
	mu         sync.Mutex
	once       sync.Once
}

// PeerTransfer fetches blobs from LBRY peers using DHT discovery
type PeerTransfer struct {
	dhtNode           protocol.DHTNode
	peerClientFactory protocol.PeerClientFactory
	timeout           time.Duration
	maxPeers          int
	dhtRetryAttempts  int
	dhtRetryDelay     time.Duration
	logger            *zap.Logger

	// Concurrent execution fields
	workerPool     *workerpool.WorkerPool
	workerPoolMu   sync.RWMutex // protects workerPool access
	backlog        map[string]*blobRequest
	backlogMu      sync.RWMutex
	maxConcurrency int
	clientPool     *sync.Pool
	clientPoolMu   sync.RWMutex               // protects clientPool access
	clientFactory  protocol.PeerClientFactory // stored factory for pool recreation
	stopped        int32                      // atomic flag for stopped state
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
// Values less than 1 are treated as invalid and will be rejected with a warning
func WithPeerTransferMaxConcurrency(maxConcurrency int) PeerTransferOption {
	return func(t *PeerTransfer) {
		if maxConcurrency < 1 {
			if t.logger != nil {
				t.logger.Warn("Invalid maxConcurrency value provided, must be >= 1. Using default value instead.",
					zap.Int("provided", maxConcurrency),
					zap.Int("default", DefaultMaxConcurrency))
			}
			// Keep the existing default value instead of silently changing it
			return
		}
		t.maxConcurrency = maxConcurrency
	}
}

// WithPeerTransferDHTRetryAttempts sets the number of retry attempts when DHT returns 0 contacts
// Values less than 0 are treated as invalid and will be rejected with a warning
func WithPeerTransferDHTRetryAttempts(retryAttempts int) PeerTransferOption {
	return func(t *PeerTransfer) {
		if retryAttempts < 0 {
			if t.logger != nil {
				t.logger.Warn("Invalid dhtRetryAttempts value provided, must be >= 0. Using default value instead.",
					zap.Int("provided", retryAttempts),
					zap.Int("default", 0))
			}
			// Keep the existing default value instead of silently changing it
			return
		}
		t.dhtRetryAttempts = retryAttempts
	}
}

// WithPeerTransferDHTRetryDelay sets the delay between DHT retry attempts
// Values less than or equal to 0 are treated as invalid and will be rejected with a warning
func WithPeerTransferDHTRetryDelay(delay time.Duration) PeerTransferOption {
	return func(t *PeerTransfer) {
		if delay <= 0 {
			if t.logger != nil {
				t.logger.Warn("Invalid dhtRetryDelay value provided, must be > 0. Using default value instead.",
					zap.Duration("provided", delay),
					zap.Duration("default", 100*time.Millisecond))
			}
			// Keep the existing default value instead of silently changing it
			return
		}
		t.dhtRetryDelay = delay
	}
}

// NewPeerTransfer creates a new PeerTransfer with the specified DHT node and peer client factory
// Returns an error if peerClientFactory is nil
func NewPeerTransfer(dhtNode protocol.DHTNode, peerClientFactory protocol.PeerClientFactory, options ...PeerTransferOption) (*PeerTransfer, error) {
	// Validate that peerClientFactory is not nil
	if peerClientFactory == nil {
		return nil, errors.New("peerClientFactory cannot be nil")
	}

	transfer := &PeerTransfer{
		dhtNode:           dhtNode,
		peerClientFactory: peerClientFactory,
		timeout:           30 * time.Second,
		maxPeers:          5,
		dhtRetryAttempts:  0,                      // Default to no retries
		dhtRetryDelay:     100 * time.Millisecond, // Default retry delay
		logger:            zap.NewNop(),
		maxConcurrency:    DefaultMaxConcurrency,
		backlog:           make(map[string]*blobRequest),
		clientFactory:     peerClientFactory, // store factory for pool recreation
	}

	for _, option := range options {
		option(transfer)
	}

	// Initialize worker pool and client pool after options are applied
	transfer.createWorkerPool()
	transfer.createClientPool()

	return transfer, nil
}

// getOrCreateFromBacklog atomically checks if a blob download is already in progress
// and returns the existing request, or creates and adds a new one if none exists.
// Returns the request and a boolean indicating if this caller is the owner (created the request).
func (t *PeerTransfer) getOrCreateFromBacklog(hash string) (*blobRequest, bool) {
	t.backlogMu.Lock()
	defer t.backlogMu.Unlock()

	if req, exists := t.backlog[hash]; exists {
		req.mu.Lock()
		req.waiters++
		req.mu.Unlock()
		return req, false // joined existing request
	}

	// No existing request, create a placeholder to reserve the spot
	// This prevents other goroutines from creating duplicate requests
	placeholder := &blobRequest{
		waiters: 1,
		done:    make(chan struct{}),
	}
	t.backlog[hash] = placeholder
	return placeholder, true // caller owns this request and must initialize it
}

// removeFromBacklog removes a completed blob request from the backlog
func (t *PeerTransfer) removeFromBacklog(hash string) {
	t.backlogMu.Lock()
	defer t.backlogMu.Unlock()
	delete(t.backlog, hash)
}

// isStopped checks if the transfer is in a stopped state
func (t *PeerTransfer) isStopped() bool {
	return atomic.LoadInt32(&t.stopped) == 1
}

// createClientPool creates a new sync.Pool using the stored factory
func (t *PeerTransfer) createClientPool() {
	t.clientPoolMu.Lock()
	defer t.clientPoolMu.Unlock()

	if t.clientPool == nil {
		// Defensive check - this should never happen due to constructor validation
		if t.clientFactory == nil {
			if t.logger != nil {
				t.logger.Error("clientFactory is nil - client pool not created")
			}
			return
		}
		t.clientPool = &sync.Pool{
			New: func() interface{} {
				return t.clientFactory()
			},
		}
	}
}

// createWorkerPool creates a new worker pool with the configured concurrency
func (t *PeerTransfer) createWorkerPool() {
	t.workerPoolMu.Lock()
	defer t.workerPoolMu.Unlock()

	if t.workerPool == nil {
		t.workerPool = workerpool.New(t.maxConcurrency)
	}
}

// getPeerClient safely gets and validates a peer client from the pool
func (t *PeerTransfer) getPeerClient() (protocol.PeerClient, error) {
	t.clientPoolMu.RLock()
	defer t.clientPoolMu.RUnlock()

	// Check if client pool is available
	if t.clientPool == nil {
		return nil, liblbryerrors.Err(liblbryerrors.ErrTransferStopped)
	}

	// Get a peer client from the pool
	client := t.clientPool.Get()
	if client == nil {
		return nil, fmt.Errorf("failed to get client from pool: client is nil")
	}

	// Type assert to protocol.PeerClient
	peerClient, ok := client.(protocol.PeerClient)
	if !ok {
		return nil, fmt.Errorf("failed to assert client as PeerClient: got %T", client)
	}

	return peerClient, nil
}

// safeReturnClient safely returns a client to the pool if both pool and client are valid
func (t *PeerTransfer) safeReturnClient(peerClient protocol.PeerClient) {
	t.clientPoolMu.RLock()
	poolExists := t.clientPool != nil && peerClient != nil
	t.clientPoolMu.RUnlock()

	if poolExists {
		t.returnClientToPool(peerClient)
	}
}

// returnClientToPool resets the client and returns it to the pool
func (t *PeerTransfer) returnClientToPool(peerClient protocol.PeerClient) {
	// Check if transfer is stopped first (without lock for performance)
	if t.isStopped() {
		t.logger.Debug("Discarding client because transfer is stopped")
		return
	}

	if resetErr := peerClient.Reset(); resetErr != nil {
		t.logger.Debug("Discarding client due to reset failure", zap.Error(resetErr))
		return
	}

	// Use a single lock to check pool existence and put the client
	t.clientPoolMu.RLock()
	defer t.clientPoolMu.RUnlock()

	if t.clientPool != nil {
		t.clientPool.Put(peerClient)
	}
}

// wait waits for the blob request to complete and returns the result.
//
// Timeout Policy:
// - Individual caller timeouts (via ctx) do NOT cancel the shared download race
// - This allows multiple callers to share the same download work without interference
// - Only the overall race timeout (managed by the race context) cancels all peer attempts
// - When a caller times out, they receive ctx.Err() but other waiters continue unaffected
//
// This design ensures that one caller's timeout doesn't waste work already in progress
// for other callers who may still be waiting for the result.
func (br *blobRequest) wait(ctx context.Context) ([]byte, error) {
	defer func() {
		br.mu.Lock()
		br.waiters--
		br.mu.Unlock()
	}()

	select {
	case <-br.done:
		br.mu.Lock()
		res := br.result
		br.mu.Unlock()
		return res.data, res.err
	case <-ctx.Done():
		// Check if this was the last waiter - if so, cancel the race to avoid wasted work
		br.mu.Lock()
		isLastWaiter := br.waiters == 1
		cancelFunc := br.cancel
		br.mu.Unlock()

		if isLastWaiter && cancelFunc != nil {
			// This was the last waiter, cancel the race to stop all peer attempts
			// This optimization prevents wasted work when no one is waiting anymore
			cancelFunc()
		}

		// Return the caller's context error
		return nil, ctx.Err()
	}
}

// downloadFromPeer attempts to download a blob from a specific peer
func (t *PeerTransfer) downloadFromPeer(ctx context.Context, peerAddr, hash string) ([]byte, error) {
	// Check if transfer is stopped
	if t.isStopped() {
		return nil, liblbryerrors.Err(liblbryerrors.ErrTransferStopped)
	}

	// Get and validate peer client
	peerClient, err := t.getPeerClient()
	if err != nil {
		return nil, err
	}

	// Attempt to connect and download
	if err := peerClient.Connect(ctx, peerAddr); err != nil {
		// Return client to pool even on error
		t.safeReturnClient(peerClient)
		// Preserve context errors without wrapping
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return nil, err
		}
		return nil, fmt.Errorf("connect to peer %s: %w", peerAddr, err)
	}

	// Defer return client to pool when done
	defer t.safeReturnClient(peerClient)

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
	// Check if transfer is stopped
	if t.isStopped() {
		return nil, liblbryerrors.Err(liblbryerrors.ErrTransferStopped)
	}

	// Validate hash
	if !stream.ValidateHash(hash) {
		return nil, liblbryerrors.ErrInvalidHash
	}

	// Check if this blob is already being downloaded, atomically
	req, isOwner := t.getOrCreateFromBacklog(hash)
	if !isOwner {
		t.logger.Debug("Joining existing blob download", zap.String("hash", hash))
		return req.wait(ctx)
	}

	// This goroutine is the owner - initialize the request
	// Ensure cleanup happens when the request is completed, even on early errors
	defer t.removeFromBacklog(hash)

	// Discover peers via DHT
	hashBitmap, err := protocol.ParseHashFromString(hash)
	if err != nil {
		// Complete the placeholder request with error so waiters are unblocked
		req.once.Do(func() {
			req.result = taskResult{err: fmt.Errorf("failed to parse hash for DHT lookup: %w", err)}
			close(req.done)
		})
		return nil, fmt.Errorf("failed to parse hash for DHT lookup: %w", err)
	}

	// Retry logic for DHT contact discovery
	var contacts []dht.Contact

	for attempt := 0; attempt <= t.dhtRetryAttempts; attempt++ {
		// Check if context is cancelled before retrying
		if ctx.Err() != nil {
			req.once.Do(func() {
				req.result = taskResult{err: ctx.Err()}
				close(req.done)
			})
			return nil, ctx.Err()
		}

		if attempt > 0 {
			t.logger.Debug("Retrying DHT contact discovery",
				zap.String("hash", hash),
				zap.Int("attempt", attempt),
				zap.Int("maxAttempts", t.dhtRetryAttempts))

			// Add delay between retries using configurable delay with exponential backoff
			// Exponential backoff: base_delay × 2^(attempt-1) with jitter
			backoffFactor := time.Duration(1 << uint(attempt-1)) // 1, 2, 4, 8...
			delay := backoffFactor * t.dhtRetryDelay
			// Add jitter: ±25% randomization
			jitter := time.Duration(rand.Int63n(int64(delay / 2)))
			delay = delay - delay/4 + jitter
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				req.once.Do(func() {
					req.result = taskResult{err: ctx.Err()}
					close(req.done)
				})
				return nil, ctx.Err()
			}
		}

		contacts, err = t.dhtNode.Get(hashBitmap)
		if err != nil {
			// Complete the placeholder request with error so waiters are unblocked
			req.once.Do(func() {
				req.result = taskResult{err: fmt.Errorf("failed to discover peers via DHT: %w", err)}
				close(req.done)
			})
			return nil, fmt.Errorf("failed to discover peers via DHT: %w", err)
		}

		// If we found contacts, break out of retry loop
		if len(contacts) > 0 {
			break
		}

		// If this was the last attempt, we'll fall through to the error handling below
		if attempt == t.dhtRetryAttempts {
			t.logger.Debug("No contacts found after all retry attempts",
				zap.String("hash", hash),
				zap.Int("totalAttempts", t.dhtRetryAttempts+1))
		}
	}

	if len(contacts) == 0 {
		// Complete the placeholder request with error so waiters are unblocked
		req.once.Do(func() {
			req.result = taskResult{err: liblbryerrors.ErrBlobNotFound}
			close(req.done)
		})
		return nil, liblbryerrors.ErrBlobNotFound
	}

	// Limit number of peers to try
	peersToTry := min(len(contacts), t.maxPeers)

	// Handle edge case where no peers will be tried
	if peersToTry == 0 {
		// Complete the placeholder request with error so waiters are unblocked
		req.once.Do(func() {
			req.result = taskResult{err: liblbryerrors.ErrBlobNotFound}
			close(req.done)
		})
		return nil, liblbryerrors.ErrBlobNotFound
	}

	// Create race context for cancellation
	// This context manages the overall timeout for all peer attempts in the race
	// It is separate from individual caller contexts to prevent one caller's timeout
	// from cancelling the entire race for all participants
	raceCtx, raceCancel := context.WithTimeout(context.Background(), t.timeout)

	// Initialize the placeholder request with actual values
	req.mu.Lock()
	req.cancel = raceCancel // Store cancel function for potential early cancellation
	req.totalPeers = int32(peersToTry)
	req.mu.Unlock()

	t.logger.Debug("Starting concurrent peer race",
		zap.String("hash", hash),
		zap.Int("total_peers", len(contacts)),
		zap.Int("peers_to_try", peersToTry))

	// Submit ALL peer tasks to worker pool simultaneously
	t.logger.Debug("Get: submitting peer tasks to worker pool",
		zap.String("hash", hash),
		zap.Int("num_tasks", peersToTry))

	for i := 0; i < peersToTry; i++ {
		contact := contacts[i]

		// Use peer's specific port if available, otherwise use the address from Addr()
		var peerAddr string
		if contact.PeerPort != 0 {
			peerAddr = net.JoinHostPort(contact.IP.String(), fmt.Sprintf("%d", contact.PeerPort))
		} else {
			peerAddr = contact.Addr().String()
		}

		// Capture loop variables
		peerAddrCopy := peerAddr
		hashCopy := hash

		t.workerPoolMu.RLock()
		workerPool := t.workerPool
		t.workerPoolMu.RUnlock()

		if workerPool == nil {
			t.logger.Debug("Worker pool is nil, cannot submit task")
			// Complete the placeholder request with error so waiters are unblocked
			req.once.Do(func() {
				req.result = taskResult{err: liblbryerrors.Err(liblbryerrors.ErrTransferStopped)}
				close(req.done)
			})
			return nil, liblbryerrors.Err(liblbryerrors.ErrTransferStopped)
		}

		workerPool.Submit(func() {
			t.logger.Debug("Attempting peer download",
				zap.String("hash", hashCopy),
				zap.String("peer", peerAddrCopy))

			data, err := t.downloadFromPeer(raceCtx, peerAddrCopy, hashCopy)

			if err == nil {
				// SUCCESS! Set result and notify all waiters
				req.once.Do(func() {
					req.mu.Lock()
					req.result = taskResult{data: data}
					req.mu.Unlock()
					close(req.done)
					t.logger.Debug("Peer download succeeded, canceling others",
						zap.String("hash", hashCopy),
						zap.String("peer", peerAddrCopy))
					raceCancel() // This cancels all other in-flight tasks
				})
			} else {
				// Track failed peer
				completed := atomic.AddInt32(&req.completed, 1)
				t.logger.Debug("Peer download failed",
					zap.String("hash", hashCopy),
					zap.String("peer", peerAddrCopy),
					zap.Error(err))

				// Check if this was the last peer to fail
				if completed >= req.totalPeers {
					req.once.Do(func() {
						req.mu.Lock()
						req.result = taskResult{err: err}
						req.mu.Unlock()
						close(req.done)
						t.logger.Debug("All peers failed, returning error",
							zap.String("hash", hashCopy))
						raceCancel() // Cancel all remaining in-flight peer attempts
					})
				}
			}
		})
	}

	// Wait for first success or timeout
	data, err := req.wait(ctx)

	return data, err
}

// Name returns the name of this transfer implementation
func (t *PeerTransfer) Name() string {
	return "peer"
}

// Start initializes or restarts the peer transfer
func (t *PeerTransfer) Start() {
	// Reset stopped flag to allow operations
	atomic.StoreInt32(&t.stopped, 0)

	// Recreate client pool if it was cleared
	t.createClientPool()

	// Recreate worker pool if it was stopped
	t.createWorkerPool()
}

// Stop gracefully shuts down the peer transfer and its worker pool
func (t *PeerTransfer) Stop() {
	// Set stopped flag atomically to prevent new operations
	atomic.StoreInt32(&t.stopped, 1)

	t.workerPoolMu.Lock()
	defer t.workerPoolMu.Unlock()

	if t.workerPool != nil {
		t.workerPool.Stop()
		t.workerPool = nil // Clear to allow recreation
	}

	// Clear the client pool reference to allow garbage collection
	// Note: We don't drain the pool because sync.Pool.Get() with a New function
	// will never return nil, which would cause an infinite loop
	t.clientPoolMu.Lock()
	t.clientPool = nil
	t.clientPoolMu.Unlock()
}
