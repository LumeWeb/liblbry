package testing

import (
	"fmt"
	"sync"

	"github.com/gammazero/workerpool"
	"github.com/stretchr/testify/assert"
)

// Package testing provides concurrency testing utilities for verifying thread-safe behavior
// of storage implementations. The helpers simplify writing concurrent tests by handling:
// - Worker pool management
// - Error collection and reporting
// - Common concurrency test patterns
//
// Typical usage involves:
// 1. Implementing the StoreOperations interface for your storage type
// 2. Using the TestConcurrent* functions to verify behavior under load
// 3. Optionally using RunConcurrentTasks directly for custom test scenarios

// StoreOperations defines the minimal interface required for concurrency testing.
// Storage implementations should satisfy this interface to work with the test helpers.
type StoreOperations interface {
	// Has checks if a blob exists in the store
	Has(hash string) (bool, error)
	// Get retrieves a blob from the store
	Get(hash string) ([]byte, error)
	// Put stores a blob in the store
	Put(hash string, data []byte) error
}

// RunConcurrentTasks executes multiple tasks concurrently using a worker pool and
// collects any errors that occur. This is a low-level helper used by other test functions.
//
// Parameters:
//   - t: TestingT interface for assertions
//   - maxWorkers: Maximum number of concurrent workers (goroutines) to use
//   - tasks: Slice of functions to execute concurrently
//
// Example:
//
//	tasks := make([]func() error, 10)
//	for i := 0; i < 10; i++ {
//	    tasks[i] = func() error {
//	        // Test logic here
//	        return nil
//	    }
//	}
//	RunConcurrentTasks(t, 5, tasks)
func RunConcurrentTasks(t assert.TestingT, maxWorkers int, tasks []func() error) {
	if maxWorkers <= 0 {
		maxWorkers = 1
	}
	if len(tasks) == 0 {
		return
	}
	wp := workerpool.New(maxWorkers)
	defer wp.StopWait()

	var wg sync.WaitGroup
	errs := make(chan error, len(tasks))
	wg.Add(len(tasks))

	for _, task := range tasks {
		wp.Submit(func() {
			defer wg.Done()
			if err := task(); err != nil {
				errs <- err
			}
		})
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
}

// TestConcurrentHas verifies concurrent Has operations on a store.
// It spawns multiple goroutines to check for blob existence simultaneously.
//
// Parameters:
//   - t: TestingT interface for assertions
//   - store: Store implementation to test
//   - hash: Blob hash to check
//   - expectedExists: Expected result of Has operation
//   - tasks: Number of concurrent Has operations to perform
//   - maxWorkers: Maximum number of concurrent workers
//
// Example:
//
//	store := NewMemoryStore()
//	store.Put("testhash", []byte("data"))
//	TestConcurrentHas(t, store, "testhash", true, 100, 10)
func TestConcurrentHas(t assert.TestingT, store StoreOperations, hash string, expectedExists bool, tasks int, maxWorkers int) {
	concurrentTasks := make([]func() error, tasks)
	for i := 0; i < tasks; i++ {
		concurrentTasks[i] = func() error {
			exists, err := store.Has(hash)
			if err != nil {
				return fmt.Errorf("Has error: %v", err)
			}
			if exists != expectedExists {
				return fmt.Errorf("expected exists=%v, got %v", expectedExists, exists)
			}
			return nil
		}
	}
	RunConcurrentTasks(t, maxWorkers, concurrentTasks)
}

// TestConcurrentGet verifies concurrent Get operations on a store.
// It spawns multiple goroutines to retrieve the same blob simultaneously.
//
// Parameters:
//   - t: TestingT interface for assertions
//   - store: Store implementation to test
//   - hash: Blob hash to retrieve
//   - expectedData: Expected blob content
//   - tasks: Number of concurrent Get operations to perform
//   - maxWorkers: Maximum number of concurrent workers
//
// Notes:
// - The store should already contain the blob before calling this function
// - Useful for testing read locks and cache consistency
//
// Example:
//
//	store := NewMemoryStore()
//	store.Put("testhash", []byte("data"))
//	TestConcurrentGet(t, store, "testhash", []byte("data"), 100, 10)
func TestConcurrentGet(t assert.TestingT, store StoreOperations, hash string, expectedData []byte, tasks int, maxWorkers int) {
	concurrentTasks := make([]func() error, tasks)
	for i := 0; i < tasks; i++ {
		concurrentTasks[i] = func() error {
			data, err := store.Get(hash)
			if err != nil {
				return fmt.Errorf("Get error: %v", err)
			}
			if string(data) != string(expectedData) {
				return fmt.Errorf("expected data %q, got %q", expectedData, data)
			}
			return nil
		}
	}
	RunConcurrentTasks(t, maxWorkers, concurrentTasks)
}

// TestConcurrentPut verifies concurrent Put operations on a store.
// It spawns multiple goroutines to store different blobs simultaneously.
//
// Parameters:
//   - t: TestingT interface for assertions
//   - store: Store implementation to test
//   - hashGen: Function that generates unique hashes for each task
//   - dataGen: Function that generates blob data for each task
//   - tasks: Number of concurrent Put operations to perform
//   - maxWorkers: Maximum number of concurrent workers
//
// Notes:
// - Each task gets unique hash/data via the generator functions
// - Verifies both Put success and subsequent Get returns correct data
// - Useful for testing write locks and concurrent modifications
//
// Example:
//
//	store := NewMemoryStore()
//	hashGen := func(i int) string { return fmt.Sprintf("hash%d", i) }
//	dataGen := func(i int) []byte { return []byte(fmt.Sprintf("data%d", i)) }
//	TestConcurrentPut(t, store, hashGen, dataGen, 100, 10)
func TestConcurrentPut(t assert.TestingT, store StoreOperations, hashGen func(int) string, dataGen func(int) []byte, tasks int, maxWorkers int) {
	concurrentTasks := make([]func() error, tasks)
	for i := 0; i < tasks; i++ {
		id := i // Capture loop variable
		concurrentTasks[i] = func() error {
			hash := hashGen(id)
			data := dataGen(id)

			if err := store.Put(hash, data); err != nil {
				return fmt.Errorf("Put error: %v", err)
			}

			storedData, err := store.Get(hash)
			if err != nil {
				return fmt.Errorf("Get after Put error: %v", err)
			}
			if !assert.Equal(t, data, storedData) {
				return fmt.Errorf("data mismatch for hash %s", hash)
			}
			return nil
		}
	}
	RunConcurrentTasks(t, maxWorkers, concurrentTasks)
}

// TestConcurrentHasMultiple verifies concurrent Has operations using multiple store instances.
// This function creates multiple store instances via the factory function and performs
// concurrent Has operations on each instance to test thread safety across independent stores.
//
// Parameters:
//   - t: TestingT interface for assertions
//   - storeFactory: Function that creates and returns a new StoreOperations instance
//   - hash: Blob hash to check for existence
//   - expectedExists: Expected result of Has operation
//   - tasks: Number of concurrent Has operations to perform
//   - maxWorkers: Maximum number of concurrent workers
func TestConcurrentHasMultiple(t assert.TestingT, storeFactory func() StoreOperations, hash string, expectedExists bool, tasks int, maxWorkers int) {
	concurrentTasks := make([]func() error, tasks)
	for i := 0; i < tasks; i++ {
		concurrentTasks[i] = func() error {
			store := storeFactory()
			exists, err := store.Has(hash)
			if err != nil {
				return fmt.Errorf("Has error: %v", err)
			}
			if exists != expectedExists {
				return fmt.Errorf("expected exists=%v, got %v", expectedExists, exists)
			}
			return nil
		}
	}
	RunConcurrentTasks(t, maxWorkers, concurrentTasks)
}

// TestConcurrentAccess provides a comprehensive concurrency test for storage implementations.
// It performs a sequence of concurrent operations:
// 1. Initial Put of test data
// 2. Concurrent Has operations
// 3. Concurrent Get operations
// 4. Concurrent Put operations with unique data
//
// Parameters:
//   - t: TestingT interface for assertions
//   - store: Store implementation to test
//   - hash: Blob hash for initial test data
//   - expectedData: Initial blob content
//   - hashGen: Function that generates unique hashes for concurrent Puts
//   - dataGen: Function that generates blob data for concurrent Puts
//   - tasks: Number of concurrent operations to perform for each test phase
//   - maxWorkers: Maximum number of concurrent workers
//
// Notes:
// - Provides complete coverage of basic concurrency scenarios
// - Each test phase waits for completion before starting the next
// - Useful for integration testing storage implementations
//
// Example:
//
//	store := NewMemoryStore()
//	hashGen := func(i int) string { return fmt.Sprintf("%064d", i) }
//	dataGen := func(i int) []byte { return []byte(fmt.Sprintf("data%d", i)) }
//	TestConcurrentAccess(t, store, "testhash", []byte("data"), hashGen, dataGen, 100, 10)
func TestConcurrentAccess(
	t assert.TestingT,
	store StoreOperations,
	hash string,
	expectedData []byte,
	hashGen func(int) string,
	dataGen func(int) []byte,
	tasks int,
	maxWorkers int,
) {
	// Put initial data
	err := store.Put(hash, expectedData)
	assert.NoError(t, err)

	// Test concurrent operations
	TestConcurrentHas(t, store, hash, true, tasks, maxWorkers)
	TestConcurrentGet(t, store, hash, expectedData, tasks, maxWorkers)
	TestConcurrentPut(t, store, hashGen, dataGen, tasks, maxWorkers)
}
