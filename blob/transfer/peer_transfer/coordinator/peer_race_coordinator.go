package coordinator

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/connection"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/discovery"
	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/executor"
	"go.uber.org/zap"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
)

// PeerRaceCoordinator defines the interface for coordinating peer download races
type PeerRaceCoordinator interface {
	// ExecuteRace executes a race among multiple peers to download a blob
	// Returns the first successful result or error if all peers fail
	ExecuteRace(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, completeOnFailure bool) ([]byte, error)

	// IsStopped checks if the coordinator is stopped
	IsStopped() bool

	// Stop stops the coordinator
	Stop()

	// Start starts the coordinator
	Start()

	// SetTimeout updates the race timeout
	SetTimeout(timeout time.Duration)

	// GetTimeout returns the current race timeout
	GetTimeout() time.Duration

	// GetCoordinator returns the underlying request coordinator
	GetCoordinator() RequestCoordinator

	// GetTaskExecutor returns the underlying task executor
	GetTaskExecutor() executor.PeerTaskExecutor

	// SetLogger updates the logger for this race coordinator instance
	SetLogger(logger *zap.Logger)
}

// DefaultPeerRaceCoordinator handles peer race coordination and task execution
type DefaultPeerRaceCoordinator struct {
	connMgr      connection.ConnectionManager
	discovery    discovery.PeerDiscovery
	coordinator  RequestCoordinator
	taskExecutor executor.PeerTaskExecutor
	timeout      time.Duration
	timeoutMu    sync.RWMutex
	logger       *zap.Logger
	stopped      int32
}

// NewPeerRaceCoordinator creates a new DefaultPeerRaceCoordinator instance
func NewPeerRaceCoordinator(
	connMgr connection.ConnectionManager,
	discovery discovery.PeerDiscovery,
	coordinator RequestCoordinator,
	taskExecutor executor.PeerTaskExecutor,
	timeout time.Duration,
	logger *zap.Logger,
) PeerRaceCoordinator {
	if connMgr == nil {
		panic("connection manager cannot be nil")
	}
	if discovery == nil {
		panic("peer discovery cannot be nil")
	}
	if coordinator == nil {
		panic("request coordinator cannot be nil")
	}
	if taskExecutor == nil {
		panic("task executor cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &DefaultPeerRaceCoordinator{
		connMgr:      connMgr,
		discovery:    discovery,
		coordinator:  coordinator,
		taskExecutor: taskExecutor,
		timeout:      timeout,
		logger:       logger,
	}
}

// ExecuteRace executes a race among multiple peers to download a blob
func (prc *DefaultPeerRaceCoordinator) ExecuteRace(ctx context.Context, hash string, contacts []dht.Contact, hashBitmap bits.Bitmap, req *blob.BlobRequest, completeOnFailure bool) ([]byte, error) {
	if prc.IsStopped() {
		return nil, fmt.Errorf("peer race coordinator is stopped")
	}

	// Check if we have any peers to try
	if len(contacts) == 0 {
		prc.logger.Debug("No contacts available for race", zap.String("hash", hash))
		return nil, fmt.Errorf("no peers available")
	}

	// Create race context for cancellation
	prc.timeoutMu.RLock()
	raceCtx, raceCancel := context.WithTimeout(ctx, prc.timeout)
	prc.timeoutMu.RUnlock()
	defer raceCancel()

	// Initialize the request with race context and total peers
	prc.coordinator.InitializeRequest(req, raceCancel, int32(len(contacts)))

	// Execute peer tasks
	err := prc.taskExecutor.ExecutePeerTasks(raceCtx, hash, contacts, hashBitmap, req, raceCancel)
	if err != nil {
		return nil, fmt.Errorf("failed to execute peer tasks: %w", err)
	}

	// Wait for race completion
	return prc.waitForRaceCompletion(ctx, req, completeOnFailure, hash)
}

// waitForRaceCompletion waits for the race to complete and returns the result
func (prc *DefaultPeerRaceCoordinator) waitForRaceCompletion(ctx context.Context, req *blob.BlobRequest, completeOnFailure bool, hash string) ([]byte, error) {
	if completeOnFailure {
		// This is the final phase, wait for completion
		return prc.coordinator.WaitForResult(ctx, req)
	}

	// This is not the final phase - wait for either success or all peers to fail
	for {
		select {
		case <-ctx.Done():
			// Context was cancelled
			return nil, ctx.Err()
		case <-req.GetDone():
			// Request was completed successfully
			completed, total, _ := prc.coordinator.GetRequestState(req)
			prc.logger.Debug("Request completed successfully in non-final phase",
				zap.String("hash", hash),
				zap.Int32("completed", completed),
				zap.Int32("total", total))

			// Get the result from the request
			res := req.GetResult()
			return res.GetData(), res.GetErr()
		case <-time.After(10 * time.Millisecond):
			// Check if all peers have completed
			completed, total, lastErr := prc.coordinator.GetRequestState(req)

			if completed >= total {
				// Give a brief moment for async completion to propagate
				time.Sleep(10 * time.Millisecond)

				// Check if request was actually completed successfully
				select {
				case <-req.GetDone():
					// Request was completed successfully
					res := req.GetResult()
					return res.GetData(), res.GetErr()
				default:
					// All peers attempted but none succeeded
					// If there was only one peer, return the actual error
					if total == 1 && lastErr != nil {
						return nil, lastErr
					}
					return nil, fmt.Errorf("all peers in this phase failed")
				}
			}
		}
	}
}

// IsStopped checks if the coordinator is stopped
func (prc *DefaultPeerRaceCoordinator) IsStopped() bool {
	return atomic.LoadInt32(&prc.stopped) != 0
}

// Stop stops the coordinator
func (prc *DefaultPeerRaceCoordinator) Stop() {
	atomic.StoreInt32(&prc.stopped, 1)
}

// Start starts the coordinator
func (prc *DefaultPeerRaceCoordinator) Start() {
	atomic.StoreInt32(&prc.stopped, 0)
}

// SetTimeout updates the race timeout
func (prc *DefaultPeerRaceCoordinator) SetTimeout(timeout time.Duration) {
	prc.timeoutMu.Lock()
	prc.timeout = timeout
	prc.timeoutMu.Unlock()
}

// GetTimeout returns the current race timeout
func (prc *DefaultPeerRaceCoordinator) GetTimeout() time.Duration {
	prc.timeoutMu.RLock()
	defer prc.timeoutMu.RUnlock()
	return prc.timeout
}

// SetLogger updates the logger for this race coordinator instance and propagates to subcomponents
func (prc *DefaultPeerRaceCoordinator) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	prc.logger = logger
	// Propagate logger to subcomponents
	prc.connMgr.SetLogger(logger)
	prc.discovery.SetLogger(logger)
	prc.coordinator.SetLogger(logger)
	prc.taskExecutor.SetLogger(logger)
}

// GetCoordinator returns the underlying request coordinator
func (prc *DefaultPeerRaceCoordinator) GetCoordinator() RequestCoordinator {
	return prc.coordinator
}

// GetTaskExecutor returns the underlying task executor
func (prc *DefaultPeerRaceCoordinator) GetTaskExecutor() executor.PeerTaskExecutor {
	return prc.taskExecutor
}
