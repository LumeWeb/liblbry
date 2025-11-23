package executor

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gammazero/workerpool"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	"go.uber.org/zap"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
)

// errorType represents the type of error that occurred during peer transfer
type errorType string

const (
	errorTypeProtocol errorType = "protocol"
	errorTypeContext  errorType = "context"
	errorTypeTimeout  errorType = "timeout"
)

// PeerTaskExecutor defines the interface for executing individual peer download tasks.
//
// Concurrency Semantics:
//   - ExecutePeerTasks can be called concurrently from multiple goroutines
//   - Start/Stop/SetMaxConcurrency are synchronized and safe for concurrent use
//   - WaitForCompletionWithContext snapshots the submitted task count at entry to handle
//     concurrent submissions gracefully. New tasks submitted after the call will not
//     be considered for that completion cycle.
//   - The executor is designed for high-concurrency scenarios but callers should
//     avoid overlapping WaitForCompletionWithContext calls unless this behavior is desired.
type PeerTaskExecutor interface {
	// ExecutePeerTasks executes download tasks for all provided peers concurrently
	ExecutePeerTasks(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, raceCancel context.CancelFunc) error

	// IsStopped checks if the executor is stopped. Returns true only after Stop() has been called.
	// During Start(), this returns false only after the worker pool is fully initialized.
	IsStopped() bool

	// Stop stops the executor and its worker pool. This method is idempotent and safe for concurrent calls.
	Stop()

	// Start starts the executor and recreates worker pool. The executor is considered started
	// only after the worker pool is fully initialized.
	Start()

	// SetMaxConcurrency updates the maximum concurrency for peer tasks
	SetMaxConcurrency(maxConcurrency int)

	// GetMaxConcurrency returns the current maximum concurrency for peer tasks
	GetMaxConcurrency() int

	// WaitForCompletion waits for all currently submitted tasks to complete
	WaitForCompletion(timeout time.Duration) error

	// WaitForCompletionWithContext waits for all currently submitted tasks to complete with context support.
	// It snapshots the submitted task count at entry to handle concurrent task submissions gracefully.
	WaitForCompletionWithContext(ctx context.Context) error

	// GetWorkerPoolStats returns detailed statistics about the worker pool state
	GetWorkerPoolStats() (size int, waitingQueueSize int, stopped bool)

	// WaitAllTasksComplete waits for all tasks to complete using the worker pool's StopWait mechanism

	// SetLogger updates the logger for this task executor instance
	SetLogger(logger *zap.Logger)
	WaitAllTasksComplete() error

	// GetTaskStats returns statistics about submitted, completed, and pending tasks
	GetTaskStats() (submitted, completed, pending int64)
}

// DefaultPeerTaskExecutor handles concurrent execution of peer download tasks
type DefaultPeerTaskExecutor struct {
	connMgr           connection.ConnectionManager
	discovery         discovery.PeerDiscovery
	completionHandler CompletionHandler
	workerPool        *workerpool.WorkerPool
	workerPoolMu      sync.RWMutex
	maxConcurrency    int
	logger            *zap.Logger
	stopped           uint32

	// Task tracking fields
	submittedTasks int64
	completedTasks int64
	pendingTasks   int64
}

// NewPeerTaskExecutor creates a new DefaultPeerTaskExecutor instance
func NewPeerTaskExecutor(
	connMgr connection.ConnectionManager,
	discovery discovery.PeerDiscovery,
	completionHandler CompletionHandler,
	maxConcurrency int,
	logger *zap.Logger,
) PeerTaskExecutor {
	if connMgr == nil {
		panic("connection manager cannot be nil")
	}
	if discovery == nil {
		panic("peer discovery cannot be nil")
	}
	if completionHandler == nil {
		panic("completion handler cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	// Use a reasonable default if not specified
	if maxConcurrency <= 0 {
		maxConcurrency = 5
	}

	executor := &DefaultPeerTaskExecutor{
		connMgr:           connMgr,
		discovery:         discovery,
		completionHandler: completionHandler,
		maxConcurrency:    maxConcurrency,
		logger:            logger,
	}

	// Initialize worker pool
	executor.createWorkerPool()

	return executor
}

// ExecutePeerTasks executes download tasks for all provided peers concurrently
func (pte *DefaultPeerTaskExecutor) ExecutePeerTasks(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, raceCancel context.CancelFunc) error {
	if pte.IsStopped() {
		return fmt.Errorf("peer task executor is stopped")
	}

	// Check if request is already done before submitting tasks
	if pte.shouldSkipTask(req) {
		return nil
	}

	// Get worker pool
	workerPool := pte.getWorkerPool()
	if workerPool == nil {
		return fmt.Errorf("worker pool is not available")
	}

	// Submit ALL peer tasks to worker pool simultaneously
	pte.logger.Debug("Submitting peer tasks to worker pool",
		zap.String("hash", hash),
		zap.Int("num_tasks", len(contacts)))

	// Increment submitted tasks counter
	// Note: We'll increment this inside the loop to ensure it only counts actual submissions
	// This ensures that if we early-return due to context cancellation or request completion,
	// we don't count tasks that weren't actually submitted

	// Pre-check for context cancellation and request completion before any task submission
	select {
	case <-ctx.Done():
		// Context cancelled, don't submit any tasks
		return nil
	default:
	}

	// Check if request is done after context check
	if pte.shouldSkipTask(req) {
		return nil
	}

	for _, contact := range contacts {
		// Check if we should proceed with this task before creating it
		// This ensures we only create and submit tasks that will actually execute
		if !pte.shouldProceedWithTask(ctx, req) {
			continue
		}

		task := pte.createPeerTask(ctx, hash, contact, hashBitmap, req, raceCancel)
		if task == nil {
			continue
		}

		// Increment counters only for tasks that will actually execute
		atomic.AddInt64(&pte.submittedTasks, 1)
		atomic.AddInt64(&pte.pendingTasks, 1)
		workerPool.Submit(task)
	}

	return nil
}

// createPeerTask creates a task function for downloading from a specific peer
func (pte *DefaultPeerTaskExecutor) createPeerTask(ctx context.Context, hash string, contact dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, raceCancel context.CancelFunc) func() {
	// Check if this is a fixed peer
	isFixed := pte.discovery.IsFixedPeer(contact)

	// Use peer's specific port if available, otherwise use the address from Addr()
	var peerAddr string
	if contact.PeerPort != 0 {
		peerAddr = net.JoinHostPort(contact.IP.String(), fmt.Sprintf("%d", contact.PeerPort))
	} else {
		peerAddr = contact.Addr().String()
	}

	// Capture loop variables
	peerAddress := peerAddr
	blobHash := hash
	isFixedPeer := isFixed
	peerContact := contact
	peerHashBitmap := hashBitmap
	raceCancelFunc := raceCancel

	return func() {
		// Quick side-effect-free check for context cancellation or request completion
		// This handles race conditions where the request might complete between task submission and execution
		if pte.checkEarlyExit(ctx, req) {
			return
		}

		data, err := pte.connMgr.DownloadFromPeer(ctx, peerAddress, blobHash)

		if err == nil {
			// SUCCESS! Set result and notify all waiters
			pte.completionHandler.CompleteWithData(req, data, raceCancelFunc, blobHash)
		} else {
			// Handle peer failure
			pte.handlePeerFailure(ctx, req, err, peerContact, peerHashBitmap, isFixedPeer, peerAddress, blobHash, raceCancelFunc)
		}

		// Decrement pending tasks and increment completed tasks
		atomic.AddInt64(&pte.completedTasks, 1)
		atomic.AddInt64(&pte.pendingTasks, -1)
	}
}

// handlePeerFailure handles the failure of a peer download attempt
func (pte *DefaultPeerTaskExecutor) handlePeerFailure(ctx context.Context, req *blob.BlobRequest, err error, contact dht.Contact, hashBitmap bits.Bitmap, isFixed bool, peerAddr string, hash string, raceCancel context.CancelFunc) {
	// Only mark peer as bad for non-context errors (connection failures, protocol errors, etc.)
	// Context cancellation/timeout errors shouldn't penalize the peer
	// Also, never remove fixed peers from DHT as they are fallback peers
	if ctx.Err() == nil && !isFixed {
		if dhtNode := pte.discovery.DHTNode(); dhtNode != nil {
			dhtNode.RemoveBadPeerFromHash(hashBitmap, contact)
		}
	}

	// Track failed peer
	_, isFinal := pte.completionHandler.TrackFailedPeer(req, err)

	// Determine error type for logging
	var errType errorType = errorTypeProtocol
	if ctx.Err() != nil {
		errType = errorTypeContext
	} else if errors.Is(err, context.DeadlineExceeded) {
		errType = errorTypeTimeout
	}

	pte.logger.Debug("Peer download failed",
		zap.String("hash", hash),
		zap.String("peer", peerAddr),
		zap.String("error_type", string(errType)),
		zap.Bool("is_fixed_peer", isFixed),
		zap.Bool("removed_from_dht", ctx.Err() == nil && !isFixed),
		zap.Error(err))

	// Check if this was the last peer to fail
	if isFinal {
		// Store the error and complete the request
		pte.completionHandler.CompleteWithErrorAndCancel(req, err, raceCancel, hash)
	}
}

// getWorkerPool safely gets the current worker pool
func (pte *DefaultPeerTaskExecutor) getWorkerPool() *workerpool.WorkerPool {
	pte.workerPoolMu.RLock()
	defer pte.workerPoolMu.RUnlock()
	return pte.workerPool
}

// createWorkerPool creates a new worker pool with the configured concurrency
func (pte *DefaultPeerTaskExecutor) createWorkerPool() {
	pte.workerPoolMu.Lock()
	defer pte.workerPoolMu.Unlock()

	if pte.workerPool == nil {
		pte.workerPool = workerpool.New(pte.maxConcurrency)
	}
}

// IsStopped checks if the executor is stopped
func (pte *DefaultPeerTaskExecutor) IsStopped() bool {
	return atomic.LoadUint32(&pte.stopped) == 1
}

// Stop stops the executor and its worker pool.
// This method is idempotent and safe for concurrent calls.
// Multiple calls to Stop will not cause errors and will only stop the worker pool once.
func (pte *DefaultPeerTaskExecutor) Stop() {
	// Only proceed with worker pool shutdown if we're the first goroutine to set the flag to 1
	if atomic.CompareAndSwapUint32(&pte.stopped, 0, 1) {
		pte.workerPoolMu.Lock()
		defer pte.workerPoolMu.Unlock()

		if pte.workerPool != nil {
			pte.workerPool.Stop()
			pte.workerPool = nil // Clear to allow recreation
		}
	}
}

// Start starts the executor and recreates worker pool
func (pte *DefaultPeerTaskExecutor) Start() {
	pte.createWorkerPool()
	atomic.StoreUint32(&pte.stopped, 0)
}

// SetLogger updates the logger for this task executor instance
func (pte *DefaultPeerTaskExecutor) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	pte.logger = logger
}

// GetTaskStats returns statistics about submitted, completed, and pending tasks
func (pte *DefaultPeerTaskExecutor) GetTaskStats() (submitted, completed, pending int64) {
	return atomic.LoadInt64(&pte.submittedTasks), atomic.LoadInt64(&pte.completedTasks), atomic.LoadInt64(&pte.pendingTasks)
}

// WaitForCompletion waits for all currently submitted tasks to complete
func (pte *DefaultPeerTaskExecutor) WaitForCompletion(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return pte.WaitForCompletionWithContext(ctx)
}

// GetMaxConcurrency returns the current maximum concurrency for peer tasks
func (pte *DefaultPeerTaskExecutor) GetMaxConcurrency() int {
	pte.workerPoolMu.RLock()
	defer pte.workerPoolMu.RUnlock()
	return pte.maxConcurrency
}

// shouldSkipTask checks if we should skip submitting tasks
func (pte *DefaultPeerTaskExecutor) shouldSkipTask(req *blob.BlobRequest) bool {
	// Check if request is already done before submitting tasks
	select {
	case <-req.GetDone():
		return true
	default:
		// Request is not done yet, continue with task submission
		return false
	}
}

// shouldProceedWithTask checks if we should proceed with a task
func (pte *DefaultPeerTaskExecutor) shouldProceedWithTask(ctx context.Context, req *blob.BlobRequest) bool {
	// Check if request is already done or context is cancelled before doing any work
	select {
	case <-req.GetDone():
		pte.logger.Debug("Skipping peer task: request already completed")
		return false
	default:
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		pte.logger.Debug("Skipping peer task: context cancelled", zap.Error(ctx.Err()))
		return false
	default:
	}

	return true
}

// handleEarlyTaskExit handles the counter adjustments when a task exits early due to
// context cancellation or request completion. This should only be called after
// pendingTasks has been incremented for the task.
func (pte *DefaultPeerTaskExecutor) handleEarlyTaskExit() {
	atomic.AddInt64(&pte.completedTasks, 1)
	atomic.AddInt64(&pte.pendingTasks, -1)
}

// checkEarlyExit checks if the task should exit early due to context cancellation or request completion.
// If so, it handles the counter adjustments and returns true. Otherwise, returns false.
// This should only be called after pendingTasks has been incremented for the task.
func (pte *DefaultPeerTaskExecutor) checkEarlyExit(ctx context.Context, req *blob.BlobRequest) bool {
	select {
	case <-req.GetDone():
		// Request already completed, handle counter adjustments and return
		pte.handleEarlyTaskExit()
		return true
	case <-ctx.Done():
		// Context cancelled, handle counter adjustments and return
		pte.handleEarlyTaskExit()
		return true
	default:
		return false
	}
}

// WaitForCompletionWithContext waits for all currently submitted tasks to complete with context support.
// It snapshots the submitted task count at entry to handle concurrent task submissions gracefully.
// If new tasks are submitted while waiting, they will not be considered for this completion cycle.
func (pte *DefaultPeerTaskExecutor) WaitForCompletionWithContext(ctx context.Context) error {
	// Snapshot the submitted task count at entry to handle concurrent submissions
	// This ensures we wait for a consistent set of tasks even if new ones are submitted
	submittedAtStart, _, _ := pte.GetTaskStats()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Check if all tasks that were submitted at the start are completed
			_, completed, pending := pte.GetTaskStats()
			if submittedAtStart == 0 || (completed >= submittedAtStart && pending == 0) {
				return nil
			}

			// Brief pause to avoid busy waiting
			<-ticker.C
		}
	}
}

// GetWorkerPoolStats returns detailed statistics about the worker pool state
func (pte *DefaultPeerTaskExecutor) GetWorkerPoolStats() (size int, waitingQueueSize int, stopped bool) {
	workerPool := pte.getWorkerPool()
	if workerPool == nil {
		return 0, 0, true
	}

	return workerPool.Size(), workerPool.WaitingQueueSize(), workerPool.Stopped()
}

// WaitAllTasksComplete waits for all tasks to complete using the worker pool's StopWait mechanism.
// This is a heavyweight synchronization operation that:
// - Blocks all other operations (Start/Stop/SetMaxConcurrency) until all tasks complete
// - Stops and recreates the worker pool, which is expensive
// - Should not be called in hot paths or frequently
// Use this only when you need to ensure all in-flight tasks are completed before proceeding.
func (pte *DefaultPeerTaskExecutor) WaitAllTasksComplete() error {
	// Create a new worker pool to replace the old one after stopping
	pte.workerPoolMu.Lock()
	defer pte.workerPoolMu.Unlock()

	if pte.workerPool == nil {
		return fmt.Errorf("worker pool is not available")
	}

	// Stop and wait for all tasks to complete
	pte.workerPool.StopWait()

	// Recreate the worker pool for future use
	pte.workerPool = workerpool.New(pte.maxConcurrency)

	return nil
}

// SetMaxConcurrency updates the maximum concurrency for peer tasks.
// Note: This recreates the worker pool, which discards any queued-but-not-running tasks.
func (pte *DefaultPeerTaskExecutor) SetMaxConcurrency(maxConcurrency int) {
	// Validate and clamp the value to prevent invalid worker pool configuration
	if maxConcurrency <= 0 {
		maxConcurrency = 1
	}

	pte.workerPoolMu.Lock()
	defer pte.workerPoolMu.Unlock()

	pte.maxConcurrency = maxConcurrency

	// Recreate worker pool with new concurrency if it exists
	if pte.workerPool != nil {
		pte.workerPool.Stop()
		pte.workerPool = workerpool.New(maxConcurrency)
	}
}
