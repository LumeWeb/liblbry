package blob

import (
	"context"
	"sync"
	"sync/atomic"
)

// BlobRequest represents a single blob request in progress
type BlobRequest struct {
	cancel     context.CancelFunc // Cancels the race context for all peer attempts
	waiters    int
	completed  int32 // number of completed peer attempts
	totalPeers int32 // total number of peer attempts scheduled
	result     taskResult
	lastError  error // tracks the last error from any peer attempt
	done       chan struct{}
	mu         sync.Mutex
	once       sync.Once
}

// CompleteOnce executes the given function exactly once using sync.Once
func (br *BlobRequest) CompleteOnce(fn func()) {
	br.once.Do(fn)
}

// Lock acquires the mutex for exclusive access
func (br *BlobRequest) Lock() {
	br.mu.Lock()
}

// Unlock releases the mutex
func (br *BlobRequest) Unlock() {
	br.mu.Unlock()
}

// taskResult holds the result of a blob request attempt
type taskResult struct {
	data []byte
	err  error
}

// NewBlobRequest creates a new BlobRequest instance
func NewBlobRequest() *BlobRequest {
	return &BlobRequest{
		done: make(chan struct{}),
	}
}

// NewTaskResult creates a new taskResult with the given data and error
func NewTaskResult(data []byte, err error) taskResult {
	return taskResult{
		data: data,
		err:  err,
	}
}

// GetCancel returns the cancel function
func (br *BlobRequest) GetCancel() context.CancelFunc {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.cancel
}

// SetCancel sets the cancel function
func (br *BlobRequest) SetCancel(cancel context.CancelFunc) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.cancel = cancel
}

// GetWaiters returns the waiters count
func (br *BlobRequest) GetWaiters() int {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.waiters
}

// SetWaiters sets the waiters count
func (br *BlobRequest) SetWaiters(waiters int) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.waiters = waiters
}

// GetCompleted returns the completed count
func (br *BlobRequest) GetCompleted() int32 {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.completed
}

// SetCompleted sets the completed count
func (br *BlobRequest) SetCompleted(completed int32) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.completed = completed
}

// IncrementCompleted atomically increments the completed counter
func (br *BlobRequest) IncrementCompleted() int32 {
	return atomic.AddInt32(&br.completed, 1)
}

// DecrementCompleted atomically decrements the completed counter
func (br *BlobRequest) DecrementCompleted() int32 {
	return atomic.AddInt32(&br.completed, -1)
}

// GetTotalPeers returns the total peers count
func (br *BlobRequest) GetTotalPeers() int32 {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.totalPeers
}

// SetTotalPeers sets the total peers count
func (br *BlobRequest) SetTotalPeers(totalPeers int32) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.totalPeers = totalPeers
}

// GetResult returns the result
func (br *BlobRequest) GetResult() taskResult {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.result
}

// SetResult sets the result
func (br *BlobRequest) SetResult(result taskResult) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.result = result
}

// GetLastError returns the last error
func (br *BlobRequest) GetLastError() error {
	br.mu.Lock()
	defer br.mu.Unlock()
	return br.lastError
}

// SetLastError sets the last error
func (br *BlobRequest) SetLastError(lastError error) {
	br.mu.Lock()
	defer br.mu.Unlock()
	br.lastError = lastError
}

// GetDone returns the done channel
// Note: No mutex lock needed since channels are safe for concurrent reads
func (br *BlobRequest) GetDone() <-chan struct{} {
	return br.done
}

// MarkDone safely closes the done channel
func (br *BlobRequest) MarkDone() {
	br.mu.Lock()
	defer br.mu.Unlock()
	close(br.done)
}

// GetData returns the data from taskResult
func (tr taskResult) GetData() []byte {
	return tr.data
}

// GetErr returns the error from taskResult
func (tr taskResult) GetErr() error {
	return tr.err
}
