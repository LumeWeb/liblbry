package coordinator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/executor"
	"go.uber.org/zap"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
)

// RequestCoordinator defines the interface for coordinating blob requests
type RequestCoordinator interface {
	GetOrCreateRequest(hash string) (*blob.BlobRequest, bool, error)
	RemoveRequest(hash string)
	WaitForResult(ctx context.Context, req *blob.BlobRequest) ([]byte, error)
	CompleteWithData(req *blob.BlobRequest, data []byte, raceCancel context.CancelFunc, hash string)
	CompleteWithError(req *blob.BlobRequest, err error) error
	CompleteWithErrorAndCancel(req *blob.BlobRequest, err error, raceCancel context.CancelFunc, hash string)
	InitializeRequest(req *blob.BlobRequest, raceCancel context.CancelFunc, totalPeers int32)
	TrackFailedPeer(req *blob.BlobRequest, err error) (int32, bool)
	ResetForNewPhase(req *blob.BlobRequest, newTotalPeers int32)
	GetRequestState(req *blob.BlobRequest) (int32, int32, error)
	GetBacklogSize() int
	IsStopped() bool
	Stop()
	Start()
}

// DefaultRequestCoordinator manages blob requests and coordinates peer downloads
type DefaultRequestCoordinator struct {
	backlog   map[string]*blob.BlobRequest
	backlogMu sync.RWMutex
	logger    *zap.Logger
	stopped   int32
}

// NewRequestCoordinator creates a new DefaultRequestCoordinator instance
func NewRequestCoordinator(logger *zap.Logger) (*DefaultRequestCoordinator, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &DefaultRequestCoordinator{
		backlog: make(map[string]*blob.BlobRequest),
		logger:  logger,
	}, nil
}

// Compile-time check to ensure DefaultRequestCoordinator implements executor.CompletionHandler
var _ executor.CompletionHandler = (*DefaultRequestCoordinator)(nil)

// IsStopped checks if the request coordinator is stopped
func (rc *DefaultRequestCoordinator) IsStopped() bool {
	return atomic.LoadInt32(&rc.stopped) == 1
}

// Stop stops the request coordinator
func (rc *DefaultRequestCoordinator) Stop() {
	atomic.StoreInt32(&rc.stopped, 1)
}

// Start starts the request coordinator
func (rc *DefaultRequestCoordinator) Start() {
	atomic.StoreInt32(&rc.stopped, 0)
}

// GetOrCreateRequest retrieves an existing request or creates a new one
func (rc *DefaultRequestCoordinator) GetOrCreateRequest(hash string) (*blob.BlobRequest, bool, error) {
	if rc.IsStopped() {
		return nil, false, fmt.Errorf("request coordinator is stopped")
	}

	rc.backlogMu.Lock()
	defer rc.backlogMu.Unlock()

	req, exists := rc.backlog[hash]
	if !exists {
		req = blob.NewBlobRequest()
		req.SetWaiters(1) // Creator is the first waiter
		rc.backlog[hash] = req
		return req, true, nil
	}

	// If request exists, increment waiters
	req.AddWaiter()
	return req, false, nil
}

// RemoveRequest removes a request from the backlog
func (rc *DefaultRequestCoordinator) RemoveRequest(hash string) {
	rc.backlogMu.Lock()
	defer rc.backlogMu.Unlock()

	delete(rc.backlog, hash)
}

// WaitForResult waits for a request to complete and returns the result
func (rc *DefaultRequestCoordinator) WaitForResult(ctx context.Context, req *blob.BlobRequest) ([]byte, error) {
	defer func() {
		// Decrement waiters when done
		newWaiters, ok := req.DoneWaiter()
		if !ok {
			// This indicates a bug - more WaitForResult calls than waiters
			rc.logger.Warn("Waiter underflow detected - more WaitForResult calls than waiters added")
			return
		}

		// If this was the last waiter and context was cancelled, cancel the race
		if newWaiters == 0 && ctx.Err() != nil {
			if cancel := req.GetCancel(); cancel != nil {
				cancel()
			}
		}
	}()

	select {
	case <-req.GetDone():
		// Request completed, return result
		result := req.GetResult()
		return result.GetData(), result.GetErr()
	case <-ctx.Done():
		// Context cancelled, return error
		return nil, ctx.Err()
	}
}

// CompleteWithData completes a request with successful data
func (rc *DefaultRequestCoordinator) CompleteWithData(req *blob.BlobRequest, data []byte, raceCancel context.CancelFunc, hash string) {
	req.CompleteOnce(func() {
		// Cancel any remaining race attempts
		if raceCancel != nil {
			raceCancel()
		}

		// Set the result
		req.SetResult(blob.NewTaskResult(data, nil))

		// Mark done - this is safe inside CompleteOnce
		req.MarkDone()
	})
}

// CompleteWithError completes a request with an error
func (rc *DefaultRequestCoordinator) CompleteWithError(req *blob.BlobRequest, err error) error {
	req.CompleteOnce(func() {
		// Set the result
		req.SetResult(blob.NewTaskResult(nil, err))
		req.MarkDone()
	})

	return err
}

// CompleteWithErrorAndCancel completes a request with an error and cancels race attempts
func (rc *DefaultRequestCoordinator) CompleteWithErrorAndCancel(req *blob.BlobRequest, err error, raceCancel context.CancelFunc, hash string) {
	req.CompleteOnce(func() {
		// Cancel any remaining race attempts
		if raceCancel != nil {
			raceCancel()
		}

		// Set the result
		req.SetResult(blob.NewTaskResult(nil, err))
		req.MarkDone()
	})
}

// InitializeRequest initializes a request with the total number of peers
func (rc *DefaultRequestCoordinator) InitializeRequest(req *blob.BlobRequest, raceCancel context.CancelFunc, totalPeers int32) {
	req.SetCancel(raceCancel)
	req.SetTotalPeers(totalPeers)
}

// TrackFailedPeer tracks a failed peer attempt
func (rc *DefaultRequestCoordinator) TrackFailedPeer(req *blob.BlobRequest, err error) (int32, bool) {
	if rc.IsStopped() {
		return 0, false
	}

	// Always update the last error when a peer fails
	req.SetLastError(err)

	completed := req.IncrementCompleted()
	isFinal := completed >= req.GetTotalPeers()

	return completed, isFinal
}

// ResetForNewPhase resets a request for a new phase of peer attempts
func (rc *DefaultRequestCoordinator) ResetForNewPhase(req *blob.BlobRequest, newTotalPeers int32) {
	if rc.IsStopped() {
		return
	}

	req.SetCompleted(0)
	req.SetTotalPeers(newTotalPeers)
	req.SetLastError(nil)
}

// GetRequestState returns the current state of a request
func (rc *DefaultRequestCoordinator) GetRequestState(req *blob.BlobRequest) (int32, int32, error) {
	if rc.IsStopped() {
		return 0, 0, fmt.Errorf("request coordinator is stopped")
	}

	return req.GetCompleted(), req.GetTotalPeers(), req.GetLastError()
}

// GetBacklogSize returns the current size of the backlog
func (rc *DefaultRequestCoordinator) GetBacklogSize() int {
	if rc.IsStopped() {
		return 0
	}

	rc.backlogMu.RLock()
	defer rc.backlogMu.RUnlock()

	return len(rc.backlog)
}
