package executor

import (
	"context"
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

// PeerTaskExecutor defines the interface for executing individual peer download tasks
type PeerTaskExecutor interface {
	// ExecutePeerTasks executes download tasks for all provided peers concurrently
	ExecutePeerTasks(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest) error

	// IsStopped checks if the executor is stopped
	IsStopped() bool

	// Stop stops the executor and its worker pool
	Stop()

	// Start starts the executor and recreates worker pool
	Start()

	// SetMaxConcurrency updates the maximum concurrency for peer tasks
	SetMaxConcurrency(maxConcurrency int)

	// GetMaxConcurrency returns the current maximum concurrency for peer tasks
	GetMaxConcurrency() int

	// WaitForCompletion waits for all currently submitted tasks to complete
	WaitForCompletion(timeout time.Duration) error

	// WaitForCompletionWithContext waits for all currently submitted tasks to complete with context support
	WaitForCompletionWithContext(ctx context.Context) error

	// GetWorkerPoolStats returns detailed statistics about the worker pool state
	GetWorkerPoolStats() (size int, waitingQueueSize int, stopped bool)

	// WaitAllTasksComplete waits for all tasks to complete using the worker pool's StopWait mechanism
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
	stopped           int32

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
func (pte *DefaultPeerTaskExecutor) ExecutePeerTasks(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest) error {
	if pte.IsStopped() {
		return fmt.Errorf("peer task executor is stopped")
	}

	// Check if request is already done before submitting tasks
	if pte.shouldSkipTask(req) {
		// Counters are already reset by shouldSkipTask
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
		// Counters are already reset by shouldSkipTask
		return nil
	}

	for _, contact := range contacts {
		// Check again if we should proceed with each task
		if !pte.shouldProceedWithTask(ctx, req) {
			continue
		}

		task := pte.createPeerTask(ctx, hash, contact, hashBitmap, req)
		if task == nil {
			continue
		}

		// Only increment if we're actually going to submit the task
		// This is a double-check to prevent race conditions
		if pte.shouldProceedWithTask(ctx, req) {
			atomic.AddInt64(&pte.submittedTasks, 1)
			workerPool.Submit(task)
		}
	}

	return nil
}

// createPeerTask creates a task function for downloading from a specific peer
func (pte *DefaultPeerTaskExecutor) createPeerTask(ctx context.Context, hash string, contact dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest) func() {
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

	return func() {
		// Check if we should skip this task
		if !pte.shouldProceedWithTask(ctx, req) {
			return
		}

		// Increment pending tasks
		atomic.AddInt64(&pte.pendingTasks, 1)

		data, err := pte.connMgr.DownloadFromPeer(ctx, peerAddress, blobHash)

		if err == nil {
			// SUCCESS! Set result and notify all waiters
			pte.completionHandler.CompleteWithData(req, data, func() {}, blobHash)
		} else {
			// Handle peer failure
			pte.handlePeerFailure(ctx, req, err, peerContact, peerHashBitmap, isFixedPeer, peerAddress, blobHash)
		}

		// Decrement pending tasks and increment completed tasks
		atomic.AddInt64(&pte.completedTasks, 1)
		atomic.AddInt64(&pte.pendingTasks, -1)
	}
}

// handlePeerFailure handles the failure of a peer download attempt
func (pte *DefaultPeerTaskExecutor) handlePeerFailure(ctx context.Context, req *blob.BlobRequest, err error, contact dht.Contact, hashBitmap bits.Bitmap, isFixed bool, peerAddr string, hash string) {
	// Only mark peer as bad for non-context errors (connection failures, protocol errors, etc.)
	// Context cancellation/timeout errors shouldn't penalize the peer
	// Also, never remove fixed peers from DHT as they are fallback peers
	if ctx.Err() == nil && !isFixed {
		pte.discovery.DHTNode().RemoveBadPeerFromHash(hashBitmap, contact)
	}

	// Track failed peer
	_, isFinal := pte.completionHandler.TrackFailedPeer(req, err)
	pte.logger.Debug("Peer download failed",
		zap.String("hash", hash),
		zap.String("peer", peerAddr),
		zap.Error(err))

	// Check if this was the last peer to fail
	if isFinal {
		// Store the error and complete the request
		pte.completionHandler.CompleteWithErrorAndCancel(req, err, func() {}, hash)
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
	return pte.stopped == 1
}

// Stop stops the executor and its worker pool
func (pte *DefaultPeerTaskExecutor) Stop() {
	pte.stopped = 1

	pte.workerPoolMu.Lock()
	defer pte.workerPoolMu.Unlock()

	if pte.workerPool != nil {
		pte.workerPool.Stop()
		pte.workerPool = nil // Clear to allow recreation
	}
}

// Start starts the executor and recreates worker pool
func (pte *DefaultPeerTaskExecutor) Start() {
	pte.stopped = 0
	pte.createWorkerPool()
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
	return pte.maxConcurrency
}

// shouldSkipTask checks if we should skip submitting tasks
func (pte *DefaultPeerTaskExecutor) shouldSkipTask(req *blob.BlobRequest) bool {
	// Check if request is already done before submitting tasks
	select {
	case <-req.GetDone():
		// Request is done, reset counters to 0 since no tasks were actually submitted
		atomic.StoreInt64(&pte.submittedTasks, 0)
		atomic.StoreInt64(&pte.completedTasks, 0)
		atomic.StoreInt64(&pte.pendingTasks, 0)
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
		// Request is done, decrement pending tasks and increment completed tasks
		// But only if we've already incremented pending tasks
		atomic.AddInt64(&pte.completedTasks, 1)
		atomic.AddInt64(&pte.pendingTasks, -1)
		return false
	default:
	}

	// Check if context is cancelled
	select {
	case <-ctx.Done():
		// Context is cancelled, decrement pending tasks and increment completed tasks
		// But only if we've already incremented pending tasks
		atomic.AddInt64(&pte.completedTasks, 1)
		atomic.AddInt64(&pte.pendingTasks, -1)
		return false
	default:
	}

	return true
}

// WaitForCompletionWithContext waits for all currently submitted tasks to complete with context support
func (pte *DefaultPeerTaskExecutor) WaitForCompletionWithContext(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Check if all tasks are completed
			submitted, completed, pending := pte.GetTaskStats()
			if submitted > 0 && completed >= submitted && pending == 0 {
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

// WaitAllTasksComplete waits for all tasks to complete using the worker pool's StopWait mechanism
func (pte *DefaultPeerTaskExecutor) WaitAllTasksComplete() error {
	workerPool := pte.getWorkerPool()
	if workerPool == nil {
		return fmt.Errorf("worker pool is not available")
	}

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

// SetMaxConcurrency updates the maximum concurrency for peer tasks
func (pte *DefaultPeerTaskExecutor) SetMaxConcurrency(maxConcurrency int) {
	pte.maxConcurrency = maxConcurrency

	// Recreate worker pool with new concurrency if it exists
	pte.workerPoolMu.Lock()
	defer pte.workerPoolMu.Unlock()

	if pte.workerPool != nil {
		pte.workerPool.Stop()
		pte.workerPool = workerpool.New(maxConcurrency)
	}
}
