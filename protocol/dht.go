package protocol

import (
	"errors"
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol/watchdog"
	"go.uber.org/zap"
)

// IsValidPortRange checks if a port is within the valid range (0-65535)
// Returns true if valid, false otherwise
func IsValidPortRange(port int) bool {
	return port >= 0 && port <= 65535
}

// IsValidNonZeroPortRange checks if a port is within the valid range (1-65535)
// Returns true if valid, false otherwise
func IsValidNonZeroPortRange(port int) bool {
	return port >= 1 && port <= 65535
}

// DefaultDHTAddress is the default address for DHT nodes
// Uses 127.0.0.1 (localhost) by default because 0.0.0.0 cannot be used for DHT broadcasting.
// Production deployments must explicitly set their external IP using WithDHTAddress().
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

	// GetContacts returns all contacts in the routing table
	GetContacts() []dht.Contact

	// FindContacts performs iterative findNode/findValue operations
	FindContacts(target bits.Bitmap, findValue bool) ([]dht.Contact, bool, error)

	// ExploreKeyspace systematically explores the keyspace around a target
	ExploreKeyspace(target bits.Bitmap) ([]dht.Contact, error)

	// ExploreKeyspaceWithLimit systematically explores the keyspace with iteration limit
	ExploreKeyspaceWithLimit(target bits.Bitmap, maxIterations int) ([]dht.Contact, error)

	// GetRoutingTable returns the internal routing table for advanced operations
	GetRoutingTable() dht.RoutingTable
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

	// Watchdog returns the DHT watchdog instance for contact validation
	Watchdog() watchdog.DHTWatchdog

	// Network Exploration Methods
	// GetRoutingTableContacts returns all contacts in the routing table
	GetRoutingTableContacts() ([]dht.Contact, error)

	// FindContacts performs iterative findNode/findValue operations
	FindContacts(target bits.Bitmap, findValue bool) ([]dht.Contact, bool, error)

	// ProbeHashes probes for specific hashes to find content and populate routing table
	ProbeHashes(hashes []bits.Bitmap) (map[bits.Bitmap][]dht.Contact, error)

	// AddContact manually adds a contact to the routing table
	AddContact(contact dht.Contact) error

	// ExploreKeyspace systematically explores the keyspace around a target
	ExploreKeyspace(target bits.Bitmap) ([]dht.Contact, error)
}

// DHTConfig holds configuration for DHT peer operations
type DHTConfig struct {
	// This node's address in format "ip:port"
	Address string
	// Logger for DHT operations (internally converted from zap.Logger)
	Logger *logrus.Logger
	// Original zap logger for components that need zap logger
	ZapLogger *zap.Logger
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
	// Watchdog for contact validation with caching and blacklisting
	Watchdog watchdog.DHTWatchdog
	// Enable network scanning during startup to populate routing table
	NetworkScanEnabled bool
}

// DHTOption configures DHT peer instances
type DHTOption func(*DHTConfig)

// validateDHTConfigIfNeeded validates the config only if it appears to be fully initialized
// This allows partial configs during building phase without triggering validation panics
func validateDHTConfigIfNeeded(c *DHTConfig) {
	// Lightweight per-field validation for obviously invalid values
	// This catches invalid field combinations early, even for partial configs

	// Validate peer protocol port range (must be non-zero for DHT operations)
	if !IsValidNonZeroPortRange(c.PeerProtocolPort) {
		panic(fmt.Sprintf("peer protocol port must be between 1 and 65535, got %d", c.PeerProtocolPort))
	}

	// Validate RPC port range if set (0 is allowed to disable)
	if !IsValidPortRange(c.RPCPort) {
		panic(fmt.Sprintf("RPC port must be between 0 and 65535 (0 to disable), got %d", c.RPCPort))
	}

	// Validate time-based fields if set
	if c.ReannounceTime < 0 {
		panic(fmt.Sprintf("reannounce time must be non-negative, got %v", c.ReannounceTime))
	}

	if c.AnnounceRate < 0 {
		panic(fmt.Sprintf("announce rate must be non-negative, got %d", c.AnnounceRate))
	}

	// Full validation for complete configs
	// We consider a config "potentially complete" if it has an address and
	// at least some configuration beyond zero values.
	hasBasicConfig := c.Address != "" &&
		(c.PeerProtocolPort != 0 || c.ReannounceTime != 0 || c.AnnounceRate != 0)

	if hasBasicConfig {
		if err := validateDHTConfig(c); err != nil {
			panic(err) // Fail fast during configuration
		}
	}
}

// validateDHTConfig checks configuration values are valid
func validateDHTConfig(c *DHTConfig) error {
	if c.Address == "" {
		return errors.New("DHT address cannot be empty")
	}

	if !IsValidNonZeroPortRange(c.PeerProtocolPort) {
		return errors.New("peer protocol port must be between 1 and 65535")
	}

	if !IsValidPortRange(c.RPCPort) {
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
		validateDHTConfigIfNeeded(c)
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
		validateDHTConfigIfNeeded(c)
	}
}

// WithDHTRPCPort sets the DHT RPC server port (0 to disable)
func WithDHTRPCPort(port int) DHTOption {
	return func(c *DHTConfig) {
		c.RPCPort = port
		validateDHTConfigIfNeeded(c)
	}
}

// WithDHTReannounceTime sets the DHT blob reannouncement interval
func WithDHTReannounceTime(interval time.Duration) DHTOption {
	return func(c *DHTConfig) {
		c.ReannounceTime = interval
		validateDHTConfigIfNeeded(c)
	}
}

// WithDHTAnnounceRate sets the maximum DHT announces per second
func WithDHTAnnounceRate(rate int) DHTOption {
	return func(c *DHTConfig) {
		c.AnnounceRate = rate
		validateDHTConfigIfNeeded(c)
	}
}

// WithDHTLogger sets the DHT-specific logger (converts zap.Logger to logrus.Logger for DHT compatibility)
func WithDHTLogger(logger *zap.Logger) DHTOption {
	return func(c *DHTConfig) {
		if logger == nil {
			c.Logger = nil
			c.ZapLogger = nil
		} else {
			c.Logger = NewZapToLogrusAdapter(logger)
			c.ZapLogger = logger
		}
	}
}

// WithDHTWatchdog sets the DHT watchdog for contact validation
func WithDHTWatchdog(wd watchdog.DHTWatchdog) DHTOption {
	return func(c *DHTConfig) {
		c.Watchdog = wd
	}
}

// WithDHTNetworkScan enables or disables network scanning during startup
func WithDHTNetworkScan(enabled bool) DHTOption {
	return func(c *DHTConfig) {
		c.NetworkScanEnabled = enabled
	}
}

// ApplyOptions applies a slice of DHTOption functions to this DHTConfig
// This helper allows applying options to a config without creating a DHT node
func (c *DHTConfig) ApplyOptions(options ...DHTOption) {
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
}
