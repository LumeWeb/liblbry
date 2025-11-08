package protocol

import (
	"errors"
	"time"

	"github.com/lbryio/lbry.go/v2/dht"
	"github.com/lbryio/lbry.go/v2/dht/bits"
	"github.com/sirupsen/logrus"
	"go.uber.org/zap"
)

// DefaultDHTAddress is the default address for DHT nodes
const DefaultDHTAddress = "127.0.0.1:4444"

// DHT abstracts the underlying dht.DHT implementation
type DHT interface {
	// Start begins listening for DHT connections
	Start() error

	// Shutdown gracefully stops the DHT
	Shutdown()

	// WaitUntilJoined blocks until the node joins the network
	WaitUntilJoined()

	// ID returns the node's ID
	ID() bits.Bitmap

	// Ping tests connectivity to another DHT node
	Ping(addr string) error

	// Get returns contacts that have the given hash
	Get(hash bits.Bitmap) ([]dht.Contact, error)

	// Add announces a hash to the DHT
	Add(hash bits.Bitmap)

	// Remove stops announcing a hash
	Remove(hash bits.Bitmap)

	// PrintState outputs debug information about the DHT state
	PrintState()
}

// DHTNode defines the interface for DHT (Distributed Hash Table) node operations
// This interface provides a clean abstraction over the underlying DHT implementation
// for blob discovery, node management, and network participation in the LBRY network.
type DHTNode interface {
	// Lifecycle Management
	Start() error
	Shutdown()
	WaitUntilJoined()
	Wait() // Blocks until all goroutines complete

	// Node Information
	ID() bits.Bitmap
	Address() string

	// Peer Discovery
	Ping(addr string) error
	Get(hash bits.Bitmap) ([]dht.Contact, error)

	// Blob Announcement
	Add(hash bits.Bitmap)
	Remove(hash bits.Bitmap)

	// State Management
	IsJoined() bool
	GetRoutingTableInfo() string
	
	// Restart restarts a stopped DHT node
	Restart() error
}

// DHTConfig holds configuration for DHT peer operations
type DHTConfig struct {
	// This node's address in format "ip:port"
	Address string
	// Logger for DHT operations (internally converted from zap.Logger)
	Logger *logrus.Logger
	// Seed nodes for joining the DHT network
	SeedNodes []string
	// Hex-encoded node ID (empty for random)
	NodeID string
	// Port for blob protocol clients
	PeerProtocolPort int
	// RPC server port (0 to disable)
	RPCPort int
	// Reannouncement interval for blobs
	ReannounceTime time.Duration
	// Maximum announces per second
	AnnounceRate int
}

// DHTOption configures DHT peer instances
type DHTOption func(*DHTConfig)

// validateDHTConfig checks configuration values are valid
func validateDHTConfig(c *DHTConfig) error {
	if c.Address == "" {
		return errors.New("DHT address cannot be empty")
	}

	if c.PeerProtocolPort < 1 || c.PeerProtocolPort > 65535 {
		return errors.New("peer protocol port must be between 1 and 65535")
	}

	if c.RPCPort < 0 || c.RPCPort > 65535 {
		return errors.New("RPC port must be between 0 and 65535 (0 to disable)")
	}

	if c.ReannounceTime <= 0 {
		return errors.New("reannounce time must be positive")
	}

	if c.AnnounceRate <= 0 {
		return errors.New("announce rate must be positive")
	}

	return nil
}

// NewDHTConfig creates a default DHT configuration
func NewDHTConfig() (*DHTConfig, error) {
	cfg := &DHTConfig{
		Address:          DefaultDHTAddress,
		SeedNodes:        []string{"lbrynet1.lbry.com:4444", "lbrynet2.lbry.com:4444", "lbrynet3.lbry.com:4444", "lbrynet4.lbry.com:4444"},
		PeerProtocolPort: 3333,
		ReannounceTime:   50 * time.Minute,
		AnnounceRate:     10,
	}
	if err := validateDHTConfig(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// WithDHTAddress sets the DHT listening address
func WithDHTAddress(addr string) DHTOption {
	return func(c *DHTConfig) {
		c.Address = addr
		if err := validateDHTConfig(c); err != nil {
			panic(err) // Fail fast during configuration
		}
	}
}

// WithDHTSeedNodes sets the seed nodes for DHT network joining
func WithDHTSeedNodes(nodes []string) DHTOption {
	return func(c *DHTConfig) {
		c.SeedNodes = nodes
	}
}

// WithDHTNodeID sets the DHT node ID (empty for random)
func WithDHTNodeID(id string) DHTOption {
	return func(c *DHTConfig) {
		c.NodeID = id
	}
}

// WithDHTPeerProtocolPort sets the port for DHT blob protocol clients
func WithDHTPeerProtocolPort(port int) DHTOption {
	return func(c *DHTConfig) {
		c.PeerProtocolPort = port
		if err := validateDHTConfig(c); err != nil {
			panic(err) // Fail fast during configuration
		}
	}
}

// WithDHTRPCPort sets the DHT RPC server port (0 to disable)
func WithDHTRPCPort(port int) DHTOption {
	return func(c *DHTConfig) {
		c.RPCPort = port
		if err := validateDHTConfig(c); err != nil {
			panic(err) // Fail fast during configuration
		}
	}
}

// WithDHTReannounceTime sets the DHT blob reannouncement interval
func WithDHTReannounceTime(interval time.Duration) DHTOption {
	return func(c *DHTConfig) {
		c.ReannounceTime = interval
		if err := validateDHTConfig(c); err != nil {
			panic(err) // Fail fast during configuration
		}
	}
}

// WithDHTAnnounceRate sets the maximum DHT announces per second
func WithDHTAnnounceRate(rate int) DHTOption {
	return func(c *DHTConfig) {
		c.AnnounceRate = rate
		if err := validateDHTConfig(c); err != nil {
			panic(err) // Fail fast during configuration
		}
	}
}

// WithDHTLogger sets the DHT-specific logger (converts zap.Logger to logrus.Logger for DHT compatibility)
func WithDHTLogger(logger *zap.Logger) DHTOption {
	return func(c *DHTConfig) {
		if logger == nil {
			c.Logger = nil
		} else {
			c.Logger = NewZapToLogrusAdapter(logger)
		}
	}
}
