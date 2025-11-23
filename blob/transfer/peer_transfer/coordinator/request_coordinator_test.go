package coordinator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"go.lumeweb.com/liblbry/blob/transfer/peer_transfer/blob"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
)

// Test helpers and common setup functions
// These helpers eliminate code duplication across test functions and provide
// consistent setup patterns for common test scenarios.

// requestTestSetup provides common test setup components for request coordinator tests
type requestTestSetup struct {
	logger      *zap.Logger
	coordinator RequestCoordinator
}

// setupRequestTest creates a standard test environment with logger and coordinator
// This eliminates the repetitive setup code that was duplicated in every test function.
func setupRequestTest(t *testing.T) *requestTestSetup {
	logger := zaptest.NewLogger(t).Named("test")
	coordinator, err := NewRequestCoordinator(logger)
	require.NoError(t, err)

	return &requestTestSetup{
		logger:      logger,
		coordinator: coordinator,
	}
}

// requestBuilder helps build common test request data with fluent interface
// This builder pattern eliminates repetitive request setup and makes tests more readable.
type requestBuilder struct {
	req           *blob.BlobRequest
	raceCancel    func()
	raceCancelled *bool
}

// newRequestBuilder creates a new request builder with sensible defaults
func newRequestBuilder() *requestBuilder {
	return &requestBuilder{
		req: blob.NewBlobRequest(),
	}
}

// withWaiters sets the number of waiters on the request
func (b *requestBuilder) withWaiters(waiters int) *requestBuilder {
	b.req.SetWaiters(waiters)
	return b
}

// withTotalPeers sets the total number of peers for the request
func (b *requestBuilder) withTotalPeers(totalPeers int32) *requestBuilder {
	b.req.SetTotalPeers(totalPeers)
	return b
}

// withCompleted sets the completed count for the request
func (b *requestBuilder) withCompleted(completed int32) *requestBuilder {
	b.req.SetCompleted(completed)
	return b
}

// withError sets the last error for the request
func (b *requestBuilder) withError(err error) *requestBuilder {
	b.req.SetLastError(err)
	return b
}

// withRaceCancel sets up a race cancel function that tracks if it was called
func (b *requestBuilder) withRaceCancel() *requestBuilder {
	raceCancelled := false
	raceCancel := func() {
		raceCancelled = true
	}
	b.raceCancel = raceCancel
	b.raceCancelled = &raceCancelled
	return b
}

// build returns the constructed request and related components
func (b *requestBuilder) build() (*blob.BlobRequest, func(), *bool) {
	return b.req, b.raceCancel, b.raceCancelled
}

// assertRequestCompleted checks that a request is properly completed
// Consolidates repetitive completion assertions across test cases.
func assertRequestCompleted(t *testing.T, req *blob.BlobRequest) {
	select {
	case <-req.GetDone():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Request should be completed")
	}
}

// assertRequestNotCompleted checks that a request is not yet completed
// Useful for testing intermediate states in async operations.
func assertRequestNotCompleted(t *testing.T, req *blob.BlobRequest) {
	select {
	case <-req.GetDone():
		t.Fatal("Request should not be completed yet")
	case <-time.After(50 * time.Millisecond):
		// Expected - request not completed yet
	}
}

// assertRaceCancelled checks that a race cancel function was called
// Consolidates repetitive race cancellation assertions.
func assertRaceCancelled(t *testing.T, raceCancelled *bool, message string) {
	require.NotNil(t, raceCancelled, "Race cancel tracking not set up")
	assert.True(t, *raceCancelled, message)
}

// assertRaceNotCancelled checks that a race cancel function was not called
// Useful for testing scenarios where race should continue.
func assertRaceNotCancelled(t *testing.T, raceCancelled *bool, message string) {
	require.NotNil(t, raceCancelled, "Race cancel tracking not set up")
	assert.False(t, *raceCancelled, message)
}

// createAsyncCompletion creates a goroutine that will complete a request after a delay
// This helper eliminates repetitive async test setup patterns.
func createAsyncCompletion(setup *requestTestSetup, req *blob.BlobRequest, data []byte, err error, delay time.Duration, hash string) {
	go func() {
		time.Sleep(delay)
		if err != nil {
			setup.coordinator.CompleteWithError(req, err)
		} else {
			setup.coordinator.CompleteWithData(req, data, func() {}, hash)
		}
	}()
}

// Test functions

func TestNewRequestCoordinator(t *testing.T) {
	setup := setupRequestTest(t)

	assert.NotNil(t, setup.coordinator)
	assert.Equal(t, 0, setup.coordinator.GetBacklogSize())
	assert.False(t, setup.coordinator.IsStopped())
}

func TestRequestCoordinator_GetOrCreateRequest(t *testing.T) {
	setup := setupRequestTest(t)
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	t.Run("Create new request", func(t *testing.T) {
		req, isOwner, err := setup.coordinator.GetOrCreateRequest(hash)
		require.NoError(t, err)

		require.NotNil(t, req)
		assert.True(t, isOwner)
		assert.Equal(t, 1, req.GetWaiters())
		assert.Equal(t, 1, setup.coordinator.GetBacklogSize())
	})

	t.Run("Join existing request", func(t *testing.T) {
		req, isOwner, err := setup.coordinator.GetOrCreateRequest(hash)
		require.NoError(t, err)

		require.NotNil(t, req)
		assert.False(t, isOwner)
		assert.Equal(t, 2, req.GetWaiters())                   // Should increment waiters
		assert.Equal(t, 1, setup.coordinator.GetBacklogSize()) // Still only one request in backlog
	})
}

func TestRequestCoordinator_RemoveRequest(t *testing.T) {
	setup := setupRequestTest(t)
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	// Create a request first
	req, _, err := setup.coordinator.GetOrCreateRequest(hash)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Equal(t, 1, setup.coordinator.GetBacklogSize())

	// Remove it
	setup.coordinator.RemoveRequest(hash)
	assert.Equal(t, 0, setup.coordinator.GetBacklogSize())
}

func TestRequestCoordinator_WaitForResult(t *testing.T) {
	setup := setupRequestTest(t)

	t.Run("Successful result", func(t *testing.T) {
		req, _, _ := newRequestBuilder().withWaiters(2).build()
		testData := []byte("test data")

		// Simulate successful completion
		createAsyncCompletion(setup, req, testData, nil, 10*time.Millisecond, lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1])

		data, err := setup.coordinator.WaitForResult(context.Background(), req)

		require.NoError(t, err)
		assert.Equal(t, testData, data)
		assert.Equal(t, 1, req.GetWaiters()) // Should decrement waiters
	})

	t.Run("Error result", func(t *testing.T) {
		req, _, _ := newRequestBuilder().withWaiters(1).build()
		testErr := assert.AnError

		// Simulate error completion
		createAsyncCompletion(setup, req, nil, testErr, 10*time.Millisecond, "")

		data, err := setup.coordinator.WaitForResult(context.Background(), req)

		assert.Error(t, err)
		assert.Equal(t, testErr, err)
		assert.Nil(t, data)
		assert.Equal(t, 0, req.GetWaiters()) // Should decrement waiters
	})

	t.Run("Context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		req, _, _ := newRequestBuilder().withWaiters(1).build()

		data, err := setup.coordinator.WaitForResult(ctx, req)

		assert.Error(t, err)
		assert.Equal(t, context.DeadlineExceeded, err)
		assert.Nil(t, data)
		assert.Equal(t, 0, req.GetWaiters()) // Should decrement waiters
	})

	t.Run("Last waiter cancels race", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		req, raceCancel, raceCancelled := newRequestBuilder().withWaiters(1).withRaceCancel().build()
		req.SetCancel(raceCancel)

		setup.coordinator.WaitForResult(ctx, req)

		assertRaceCancelled(t, raceCancelled, "Race should be cancelled when last waiter times out")
	})
}

func TestRequestCoordinator_CompleteWithData(t *testing.T) {
	setup := setupRequestTest(t)
	req, raceCancel, raceCancelled := newRequestBuilder().withWaiters(1).withRaceCancel().build()
	testData := []byte("test data")

	setup.coordinator.CompleteWithData(req, testData, raceCancel, lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1])

	assertRequestCompleted(t, req)
	assert.Equal(t, testData, req.GetResult().GetData())
	assert.NoError(t, req.GetResult().GetErr())
	assertRaceCancelled(t, raceCancelled, "Race should be cancelled")
}

func TestRequestCoordinator_CompleteWithError(t *testing.T) {
	setup := setupRequestTest(t)
	req, _, _ := newRequestBuilder().withWaiters(1).build()
	testErr := assert.AnError

	err := setup.coordinator.CompleteWithError(req, testErr)

	assert.Equal(t, testErr, err)
	assertRequestCompleted(t, req)
	assert.Nil(t, req.GetResult().GetData())
	assert.Equal(t, testErr, req.GetResult().GetErr())
}

func TestRequestCoordinator_CompleteWithErrorAndCancel(t *testing.T) {
	setup := setupRequestTest(t)
	req, raceCancel, raceCancelled := newRequestBuilder().withWaiters(1).withRaceCancel().build()
	testErr := assert.AnError

	setup.coordinator.CompleteWithErrorAndCancel(req, testErr, raceCancel, lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1])

	assertRequestCompleted(t, req)
	assert.Nil(t, req.GetResult().GetData())
	assert.Equal(t, testErr, req.GetResult().GetErr())
	assertRaceCancelled(t, raceCancelled, "Race should be cancelled")
}

func TestRequestCoordinator_InitializeRequest(t *testing.T) {
	setup := setupRequestTest(t)
	req, _, _ := newRequestBuilder().withWaiters(1).build()
	raceCancel := func() {}
	totalPeers := int32(5)

	setup.coordinator.InitializeRequest(req, raceCancel, totalPeers)

	assert.NotNil(t, req.GetCancel())
	assert.Equal(t, totalPeers, req.GetTotalPeers())
}

func TestRequestCoordinator_TrackFailedPeer(t *testing.T) {
	setup := setupRequestTest(t)
	testErr := assert.AnError

	t.Run("Non-final peer", func(t *testing.T) {
		req, _, _ := newRequestBuilder().withWaiters(1).withTotalPeers(3).build()
		completed, isFinal := setup.coordinator.TrackFailedPeer(req, testErr)

		assert.Equal(t, int32(1), completed)
		assert.False(t, isFinal)
		assert.Equal(t, testErr, req.GetLastError())
	})

	t.Run("Final peer", func(t *testing.T) {
		req, _, _ := newRequestBuilder().withWaiters(1).withTotalPeers(3).build()
		// Track two failures to reach total
		setup.coordinator.TrackFailedPeer(req, testErr)
		setup.coordinator.TrackFailedPeer(req, testErr)
		completed, isFinal := setup.coordinator.TrackFailedPeer(req, testErr)

		assert.Equal(t, int32(3), completed)
		assert.True(t, isFinal)
		assert.Equal(t, testErr, req.GetLastError())
	})
}

func TestRequestCoordinator_ResetForNewPhase(t *testing.T) {
	setup := setupRequestTest(t)
	req, _, _ := newRequestBuilder().withWaiters(1).withCompleted(2).withTotalPeers(3).withError(assert.AnError).build()
	newTotalPeers := int32(5)

	setup.coordinator.ResetForNewPhase(req, newTotalPeers)

	assert.Equal(t, int32(0), req.GetCompleted())
	assert.Equal(t, newTotalPeers, req.GetTotalPeers())
	assert.NoError(t, req.GetLastError())
}

func TestRequestCoordinator_GetRequestState(t *testing.T) {
	setup := setupRequestTest(t)
	req, _, _ := newRequestBuilder().withWaiters(1).withCompleted(2).withTotalPeers(5).withError(assert.AnError).build()

	completed, total, lastError := setup.coordinator.GetRequestState(req)

	assert.Equal(t, int32(2), completed)
	assert.Equal(t, int32(5), total)
	assert.Equal(t, assert.AnError, lastError)
}

func TestRequestCoordinator_ConcurrentAccess(t *testing.T) {
	setup := setupRequestTest(t)
	hash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	// Test concurrent access to the same hash
	numGoroutines := 10
	var wg sync.WaitGroup
	results := make([]*blob.BlobRequest, numGoroutines)
	owners := make([]bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			req, isOwner, err := setup.coordinator.GetOrCreateRequest(hash)
			if err != nil {
				t.Errorf("GetOrCreateRequest failed: %v", err)
				return
			}
			results[index] = req
			owners[index] = isOwner
		}(i)
	}

	wg.Wait()

	// Verify that exactly one goroutine became the owner
	ownerCount := 0
	for _, isOwner := range owners {
		if isOwner {
			ownerCount++
		}
	}
	assert.Equal(t, 1, ownerCount, "Exactly one goroutine should be the owner")

	// Verify all goroutines got the same request
	firstReq := results[0]
	for _, req := range results {
		assert.Equal(t, firstReq, req, "All goroutines should get the same request")
	}

	// Verify the request has the correct number of waiters
	assert.Equal(t, numGoroutines, firstReq.GetWaiters())
}

func TestRequestCoordinator_OnceBehavior(t *testing.T) {
	setup := setupRequestTest(t)
	req, _, _ := newRequestBuilder().withWaiters(1).build()
	testData := []byte("test data")
	raceCancel := func() {}

	// Call CompleteWithData multiple times - should only complete once
	setup.coordinator.CompleteWithData(req, testData, raceCancel, lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1])
	setup.coordinator.CompleteWithData(req, []byte("other data"), raceCancel, lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1])

	// Verify only the first completion took effect
	assert.Equal(t, testData, req.GetResult().GetData())
	assert.NoError(t, req.GetResult().GetErr())

	// Verify done channel is closed
	select {
	case <-req.GetDone():
		// Expected
	default:
		t.Fatal("Done channel should be closed")
	}
}
