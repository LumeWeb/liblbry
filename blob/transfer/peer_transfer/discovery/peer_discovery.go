package discovery

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"sync/atomic"
	"time"

	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol"
	"go.uber.org/zap"
)

// PeerDiscovery defines the interface for peer discovery functionality
type PeerDiscovery interface {
	IsStopped() bool
	Stop()
	Start()
	GetFixedPeers() []dht.Contact
	IsFixedPeer(contact dht.Contact) bool
	DiscoverPeers(ctx context.Context, hashBitmap bits.Bitmap) ([]dht.Contact, error)
	SetRetryConfig(attempts int, delay time.Duration)
	SetFixedPeers(peers []dht.Contact)
	ParseFixedPeers(peerAddresses []string) ([]dht.Contact, error)
	DHTNode() protocol.DHTNode
	RetryAttempts() int
	RetryDelay() time.Duration
}

// DefaultPeerDiscovery handles DHT peer discovery with retry logic and fixed peers fallback
type DefaultPeerDiscovery struct {
	dhtNode       protocol.DHTNode
	fixedPeers    []dht.Contact
	retryAttempts int
	retryDelay    time.Duration
	logger        *zap.Logger
	stopped       int32
	hostResolver  HostResolver
}

// NewPeerDiscovery creates a new DefaultPeerDiscovery instance
func NewPeerDiscovery(dhtNode protocol.DHTNode, logger *zap.Logger) *DefaultPeerDiscovery {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DefaultPeerDiscovery{
		dhtNode:       dhtNode,
		fixedPeers:    nil,
		retryAttempts: 0,
		retryDelay:    100 * time.Millisecond,
		logger:        logger,
		hostResolver:  NewDefaultHostResolver(logger, 10*time.Second),
	}
}

// NewPeerDiscoveryWithHostResolver creates a new DefaultPeerDiscovery instance with a custom HostResolver
// This allows for dependency injection and testing with mock resolvers
func NewPeerDiscoveryWithHostResolver(dhtNode protocol.DHTNode, logger *zap.Logger, hostResolver HostResolver) *DefaultPeerDiscovery {
	if logger == nil {
		logger = zap.NewNop()
	}
	if hostResolver == nil {
		hostResolver = NewDefaultHostResolver(logger, 10*time.Second)
	}
	return &DefaultPeerDiscovery{
		dhtNode:       dhtNode,
		fixedPeers:    nil,
		retryAttempts: 0,
		retryDelay:    100 * time.Millisecond,
		logger:        logger,
		hostResolver:  hostResolver,
	}
}

// SetRetryConfig configures retry behavior
func (pd *DefaultPeerDiscovery) SetRetryConfig(attempts int, delay time.Duration) {
	pd.retryAttempts = attempts
	pd.retryDelay = delay
}

// SetFixedPeers configures fixed fallback peers
func (pd *DefaultPeerDiscovery) SetFixedPeers(peers []dht.Contact) {
	pd.fixedPeers = peers
}

// IsStopped checks if discovery is stopped
func (pd *DefaultPeerDiscovery) IsStopped() bool {
	return atomic.LoadInt32(&pd.stopped) == 1
}

// Stop stops the discovery service
func (pd *DefaultPeerDiscovery) Stop() {
	atomic.StoreInt32(&pd.stopped, 1)
}

// Start starts the discovery service
func (pd *DefaultPeerDiscovery) Start() {
	atomic.StoreInt32(&pd.stopped, 0)
}

// DiscoverPeers attempts to discover peers for a given hash using DHT
// Returns discovered contacts or error if discovery fails
func (pd *DefaultPeerDiscovery) DiscoverPeers(ctx context.Context, hashBitmap bits.Bitmap) ([]dht.Contact, error) {
	if pd.IsStopped() {
		return nil, fmt.Errorf("peer discovery is stopped")
	}

	return pd.discoverWithRetry(ctx, hashBitmap)
}

// GetFixedPeers returns the configured fixed peers
func (pd *DefaultPeerDiscovery) GetFixedPeers() []dht.Contact {
	return pd.fixedPeers
}

// IsFixedPeer checks if a contact is in the fixed peers list
func (pd *DefaultPeerDiscovery) IsFixedPeer(contact dht.Contact) bool {
	if len(pd.fixedPeers) == 0 {
		return false
	}

	// Use the Contact's Equals method with checkID=false since fixed peers have empty IDs
	for _, fixedPeer := range pd.fixedPeers {
		if contact.Equals(fixedPeer, false) {
			return true
		}
	}
	return false
}

// ParseFixedPeers converts a slice of peer address strings to dht.Contact instances
// Each address should be in the format "host:port" (e.g., "192.168.1.100:3333" or "peer.example.com:3333")
// Supports both IP addresses and hostnames. Hostnames are resolved to IP addresses using DNS lookup.
// Returns an error if any address is invalid or hostname resolution fails
func (pd *DefaultPeerDiscovery) ParseFixedPeers(peerAddresses []string) ([]dht.Contact, error) {
	if len(peerAddresses) == 0 {
		return nil, nil
	}

	contacts := make([]dht.Contact, 0, len(peerAddresses))
	for _, addr := range peerAddresses {
		if addr == "" {
			continue // skip empty addresses
		}

		// Parse address to validate format
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid peer address format '%s': %w", addr, err)
		}

		// Parse port to ensure it's valid
		portNum, err := net.LookupPort("tcp", port)
		if err != nil {
			return nil, fmt.Errorf("invalid port in peer address '%s': %w", addr, err)
		}

		// Resolve host to IP address (supports both IP addresses and hostnames)
		ip, err := pd.hostResolver.ResolveHostToIP(host, addr)
		if err != nil {
			return nil, err
		}

		// Create a dht.Contact with the required fields
		// ID is set to empty since fixed peers don't need DHT node IDs
		// Port is the DHT port (4444), PeerPort is parsed from the address string
		contact := dht.Contact{
			ID:       bits.Bitmap{}, // Empty ID for fixed peers
			IP:       ip,
			Port:     protocol.DefaultDHTPort, // Fixed DHT port
			PeerPort: portNum,                 // Peer port parsed from address
		}
		contacts = append(contacts, contact)
	}

	return contacts, nil
}

// DHTNode returns the underlying DHT node
func (pd *DefaultPeerDiscovery) DHTNode() protocol.DHTNode {
	return pd.dhtNode
}

// RetryAttempts returns the configured retry attempts
func (pd *DefaultPeerDiscovery) RetryAttempts() int {
	return pd.retryAttempts
}

// RetryDelay returns the configured retry delay
func (pd *DefaultPeerDiscovery) RetryDelay() time.Duration {
	return pd.retryDelay
}

// discoverWithRetry attempts DHT discovery with exponential backoff retry logic
func (pd *DefaultPeerDiscovery) discoverWithRetry(ctx context.Context, hashBitmap bits.Bitmap) ([]dht.Contact, error) {
	var contacts []dht.Contact

	for attempt := 0; attempt <= pd.retryAttempts; attempt++ {
		// Check if context is cancelled before retrying
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if pd.IsStopped() {
			return nil, fmt.Errorf("peer discovery is stopped")
		}

		if attempt > 0 {
			pd.logger.Debug("Retrying DHT contact discovery",
				zap.Int("attempt", attempt),
				zap.Int("maxAttempts", pd.retryAttempts))

			// Add delay between retries using configurable delay with exponential backoff
			delay := calculateBackoffDelay(attempt, pd.retryDelay)
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		var err error
		contacts, err = pd.dhtNode.Get(hashBitmap)
		if err != nil {
			pd.logger.Debug("DHT discovery failed, will fall back to fixed peers",
				zap.Error(err))
			// Return empty contacts to allow fallback to fixed peers
			return []dht.Contact{}, nil
		}

		// If we found contacts, break out of retry loop
		if len(contacts) > 0 {
			pd.logger.Debug("DHT discovery succeeded",
				zap.Int("contacts_found", len(contacts)))
			break
		}

		// If this was the last attempt, we'll fall through to return empty contacts
		if attempt == pd.retryAttempts {
			pd.logger.Debug("No contacts found after all retry attempts",
				zap.Int("totalAttempts", pd.retryAttempts+1))
		}
	}

	return contacts, nil
}

// calculateBackoffDelay calculates exponential backoff delay with jitter
// Exponential backoff: base_delay × 2^(attempt-1) with ±25% jitter
func calculateBackoffDelay(attempt int, baseDelay time.Duration) time.Duration {
	backoffFactor := time.Duration(1 << uint(attempt-1)) // 1, 2, 4, 8...
	delay := backoffFactor * baseDelay
	// Add jitter: ±25% randomization
	jitter := time.Duration(rand.Int63n(int64(delay / 2)))
	delay = delay - delay/4 + jitter
	return delay
}
