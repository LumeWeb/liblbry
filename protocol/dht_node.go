package protocol

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol/watchdog"
)

// globalLoggerOnce ensures global logger is set only once per process to prevent data races
var globalLoggerOnce sync.Once
var globalLoggerSet bool

// managedDHTNode implements the DHTNode interface by wrapping the existing DHT implementation
type managedDHTNode struct {
	dht      DHT
	config   *DHTConfig
	watchdog watchdog.DHTWatchdog
	mu       sync.RWMutex
	joined   bool
	stopped  bool
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	joinDone chan struct{} // Channel to signal when join goroutine completes
	joinOnce sync.Once     // Ensure joinDone is closed only once
}

// isActive returns true if the DHT peer is active (not stopped and has a DHT instance)
func (w *managedDHTNode) isActive() bool {
	return !w.stopped && w.dht != nil
}

// NewDHTNode creates a new DHT node instance. If dhtImpl is nil, it creates a new DHT instance.
// Note: When an external DHT implementation is supplied (dhtImpl != nil), the watchdog created
// by this wrapper is a wrapper-level helper and is NOT wired into the external DHT implementation.
// The watchdog returned by Watchdog() may not be the same validator used by the underlying DHT
// in this case. For internally managed DHTs (dhtImpl == nil), the watchdog is properly wired
// as the validator for the DHT.
func NewDHTNode(dhtImpl DHT, options ...DHTOption) (DHTNode, error) {
	config, err := NewDHTConfig()
	if err != nil {
		return nil, err
	}

	// Apply options
	for _, option := range options {
		option(config)
	}

	// Set default watchdog if none provided
	if config.Watchdog == nil {
		config.Watchdog = watchdog.New()
	}

	// If no DHT implementation provided, create default one
	if dhtImpl == nil {
		// Convert to DHT config
		dhtConfig := &dht.Config{
			Address:          config.Address,
			SeedNodes:        config.SeedNodes,
			NodeID:           config.NodeID,
			PeerProtocolPort: config.PeerProtocolPort,
			RPCPort:          config.RPCPort,
			ReannounceTime:   config.ReannounceTime,
			AnnounceRate:     config.AnnounceRate,
			Validator:        config.Watchdog,
		}

		dhtImpl = dht.New(dhtConfig)
		if config.Logger != nil {
			// Note: dht.UseLogger() sets a package-level global logger that affects all DHT instances.
			// All nodes created after this call will use the same logger. If you need different logging
			// behavior for different nodes, set the logger once during application startup before any
			// dht.New() calls, or manage logging at a higher level.
			//
			// Use sync.Once to ensure global logger is set only once per process to prevent data races
			globalLoggerOnce.Do(func() {
				dht.UseLogger(config.Logger)
				dht.NodeFinderUseLogger(config.Logger)
				globalLoggerSet = true
			})
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	wrapper := &managedDHTNode{
		dht:      dhtImpl,
		config:   config,
		watchdog: config.Watchdog,
		joined:   false,
		ctx:      ctx,
		cancel:   cancel,
		joinDone: make(chan struct{}),
	}

	return wrapper, nil
}

// NewDHTNodeWithDefaults creates a new DHT node instance with default DHT implementation
// This maintains backward compatibility with the original function signature
func NewDHTNodeWithDefaults(options ...DHTOption) (DHTNode, error) {
	return NewDHTNode(nil, options...)
}

// Start starts the DHT peer and joins the network
func (w *managedDHTNode) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.stopped {
		return fmt.Errorf("DHT node is stopped")
	}

	err := w.dht.Start()
	if err != nil {
		return fmt.Errorf("failed to start DHT node: %w", err)
	}

	// Start the join monitoring goroutine
	w.startJoinGoroutine()

	return nil
}

// Shutdown gracefully shuts down the DHT peer
func (w *managedDHTNode) Shutdown() {
	// First, acquire lock to check state and set flags
	w.mu.Lock()
	if !w.isActive() {
		w.mu.Unlock()
		return
	}

	// Set stopped flag to prevent new operations
	w.stopped = true

	// Capture needed state before releasing lock
	dhtInstance := w.dht
	cancelFunc := w.cancel

	// Release lock before calling operations that might block
	w.mu.Unlock()

	// Cancel context to signal goroutines to stop
	cancelFunc()

	// Shutdown the DHT
	dhtInstance.Shutdown()

	// Stop the watchdog to clean up its goroutines and resources
	if w.watchdog != nil {
		w.watchdog.Stop()
	}

	// Wait for all goroutines to finish
	w.wg.Wait()

	// Re-acquire lock briefly to finalize state changes
	w.mu.Lock()
	w.joined = false
	w.mu.Unlock()
}

// WaitUntilJoined blocks until the node joins the network or context is cancelled
func (w *managedDHTNode) WaitUntilJoined() {
	w.mu.RLock()
	if !w.isActive() {
		w.mu.RUnlock()
		return
	}

	// Check if we're already joined
	if w.joined {
		w.mu.RUnlock()
		return
	}

	// Get the joinDone channel while holding the lock
	joinDone := w.joinDone
	w.mu.RUnlock()

	// Wait for either the join goroutine to complete or context cancellation
	select {
	case <-joinDone:
		// Join goroutine completed, state should already be updated
	case <-w.ctx.Done():
		// Context was cancelled
		return
	}
}

// ID returns the node's ID
func (w *managedDHTNode) ID() bits.Bitmap {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return bits.Bitmap{}
	}

	return w.dht.ID()
}

// Address returns the node's listening address
func (w *managedDHTNode) Address() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.config.Address
}

// Ping pings a given address to test connectivity
func (w *managedDHTNode) Ping(addr string) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return fmt.Errorf("DHT peer is stopped")
	}

	return w.dht.Ping(addr)
}

// Get returns the list of contacts that have the blob for the given hash
func (w *managedDHTNode) Get(hash bits.Bitmap) ([]dht.Contact, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return nil, fmt.Errorf("DHT peer is stopped")
	}

	return w.dht.Get(hash)
}

// Add adds a hash to the list of hashes this node is announcing
func (w *managedDHTNode) Add(hash bits.Bitmap) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return
	}

	w.dht.Add(hash)
}

// Remove removes a hash from the list of hashes this node is announcing
func (w *managedDHTNode) Remove(hash bits.Bitmap) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return
	}

	w.dht.Remove(hash)
}

// IsJoined returns whether the node has successfully joined the network
func (w *managedDHTNode) IsJoined() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.joined
}

// GetRoutingTableInfo returns information about the current routing table
func (w *managedDHTNode) GetRoutingTableInfo() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return fmt.Sprintf("DHT Node stopped at %s", w.Address())
	}

	// Since the original DHT doesn't expose this directly, we'll return a basic info string
	// In a real implementation, you might want to extend the DHT to expose more detailed info
	return fmt.Sprintf("DHT Node %s at %s - Joined: %v",
		w.ID().HexShort(),
		w.Address(),
		w.IsJoined())
}

// Wait blocks until all goroutines have finished
func (w *managedDHTNode) Wait() {
	w.wg.Wait()
}

// Restart restarts a stopped DHT node
func (w *managedDHTNode) Restart() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.stopped {
		return fmt.Errorf("DHT node must be stopped before restarting")
	}

	// Reset state
	w.stopped = false
	w.joined = false
	w.ctx, w.cancel = context.WithCancel(context.Background())
	w.joinDone = make(chan struct{}) // Create new joinDone channel
	w.joinOnce = sync.Once{}         // Reset joinOnce for the new channel

	// Start fresh wait group
	w.wg = sync.WaitGroup{}

	// Start the DHT
	err := w.dht.Start()
	if err != nil {
		return fmt.Errorf("failed to restart DHT node: %w", err)
	}

	// Start the join monitoring goroutine
	w.startJoinGoroutine()

	return nil
}

// Watchdog returns the DHT watchdog instance for contact validation.
// Note: For externally managed DHTs (when a DHT implementation was supplied to NewDHTNode),
// this watchdog is a wrapper-level helper and may not be the same validator used by the
// underlying DHT implementation. For internally managed DHTs, this is the active validator.
func (w *managedDHTNode) Watchdog() watchdog.DHTWatchdog {
	return w.watchdog
}

// startJoinGoroutine starts a goroutine to monitor DHT join status
func (w *managedDHTNode) startJoinGoroutine() {
	// Use a channel to signal when the goroutine has started
	started := make(chan struct{})

	// Capture the current joinDone channel to avoid race conditions with Restart()
	currentJoinDone := w.joinDone

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		// Signal that we've started
		close(started)

		// Check if context is already cancelled before starting
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		// Call the underlying DHT's WaitUntilJoined directly
		// This is a blocking call that waits for the DHT to join the network
		w.dht.WaitUntilJoined()

		// After WaitUntilJoined returns, check if we're still active and using the same channel
		w.mu.Lock()
		if !w.stopped && w.joinDone == currentJoinDone {
			w.joined = true
			// Use sync.Once to ensure the channel is only closed once
			w.joinOnce.Do(func() {
				close(w.joinDone)
			})
		}
		w.mu.Unlock()
	}()

	// Wait for the goroutine to start before returning
	<-started
}

// PrintState prints the current state of the DHT (for debugging)
func (w *managedDHTNode) PrintState() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if !w.isActive() {
		return
	}

	w.dht.PrintState()
}

// GetDHTInstance returns the underlying DHT instance for advanced operations
// This should be used carefully as it exposes the internal implementation
func (w *managedDHTNode) GetDHTInstance() DHT {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.dht
}

// ParseHashFromString parses a hex string into a bits.Bitmap
func ParseHashFromString(hashStr string) (bits.Bitmap, error) {
	// Strip 0x or 0X prefix if present
	if strings.HasPrefix(hashStr, "0x") || strings.HasPrefix(hashStr, "0X") {
		hashStr = hashStr[2:]
	}

	// Check for empty string
	if hashStr == "" {
		return bits.Bitmap{}, fmt.Errorf("invalid hash format: empty string")
	}

	// Use FromShortHex to handle shorter hex strings by padding with leading zeros
	hash, err := bits.FromShortHex(hashStr)
	if err != nil {
		return bits.Bitmap{}, fmt.Errorf("invalid hash format: %w", err)
	}

	return hash, nil
}

// HashToString converts a bits.Bitmap to a hex string
func HashToString(hash bits.Bitmap) string {
	return hash.Hex()
}
