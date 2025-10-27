package protocol

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lbryio/lbry.go/v2/dht"
	"github.com/lbryio/lbry.go/v2/dht/bits"
)

// managedDHTNode implements the DHTNode interface by wrapping the existing DHT implementation
type managedDHTNode struct {
	dht     DHT
	config  *DHTConfig
	mu      sync.RWMutex
	joined  bool
	stopped bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// isActive returns true if the DHT peer is active (not stopped and has a DHT instance)
func (w *managedDHTNode) isActive() bool {
	return !w.stopped && w.dht != nil
}

// NewDHTNode creates a new DHT node instance. If dhtImpl is nil, it creates a new DHT instance.
func NewDHTNode(dhtImpl DHT, options ...DHTOption) (DHTNode, error) {
	config := NewDHTConfig()

	// Apply options
	for _, option := range options {
		option(config)
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
		}

		dhtImpl = dht.New(dhtConfig)
		if config.Logger != nil {
			dht.UseLogger(config.Logger)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	wrapper := &managedDHTNode{
		dht:    dhtImpl,
		config: config,
		joined: false,
		ctx:    ctx,
		cancel: cancel,
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

	if !w.isActive() {
		return fmt.Errorf("DHT node is stopped")
	}

	if w.joined {
		return nil
	}

	err := w.dht.Start()
	if err != nil {
		return fmt.Errorf("failed to start DHT node: %w", err)
	}

	// Track and manage the join goroutine
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()

		select {
		case <-w.ctx.Done():
			return
		default:
			w.dht.WaitUntilJoined()
			w.mu.Lock()
			w.joined = true
			w.mu.Unlock()
		}
	}()

	return nil
}

// Shutdown gracefully shuts down the DHT peer
func (w *managedDHTNode) Shutdown() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.isActive() {
		return
	}

	w.stopped = true
	w.joined = false

	// Cancel context to signal goroutines to stop
	w.cancel()

	// Shutdown the DHT
	w.dht.Shutdown()

	// Wait for all goroutines to finish
	w.wg.Wait()
}

// WaitUntilJoined blocks until the node joins the network
func (w *managedDHTNode) WaitUntilJoined() {
	w.mu.RLock()
	if !w.isActive() {
		w.mu.RUnlock()
		return
	}
	w.mu.RUnlock()

	w.dht.WaitUntilJoined()

	w.mu.Lock()
	w.joined = true
	w.mu.Unlock()
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
