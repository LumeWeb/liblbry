package server

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/protocol/watchdog"
	"go.uber.org/zap"
)

// DHTBuilder handles all DHT configuration complexity with a clean, fluent interface
// This isolates DHT configuration logic from the main ServerBuilder
type DHTBuilder struct {
	config  *dhtBuilderConfig
	options []protocol.DHTOption
	logger  *zap.Logger
}

// dhtBuilderConfig holds all DHT configuration state
type dhtBuilderConfig struct {
	// Core network settings
	port      int
	address   string
	seedNodes []string

	// DHT node settings
	nodeID           string
	peerProtocolPort int
	rpcPort          int
	reannounceTime   time.Duration
	announceRate     int

	// Runtime components
	logger   *zap.Logger
	watchdog watchdog.DHTWatchdog

	// Validation state
	validationEnabled bool
}

// NewDHTBuilder creates a new DHTBuilder with sensible defaults
func NewDHTBuilder(logger *zap.Logger) (*DHTBuilder, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Get default DHT configuration to inherit seed nodes and other defaults
	defaultDHTConfig, err := protocol.NewDHTConfig()
	if err != nil {
		// Return nil if protocol config creation fails - caller can handle this appropriately
		return nil, err
	}

	return &DHTBuilder{
		config: &dhtBuilderConfig{
			port:              DefaultDHTPort,
			address:           "",
			seedNodes:         defaultDHTConfig.SeedNodes,
			nodeID:            "",
			peerProtocolPort:  DefaultPeerPort,
			rpcPort:           0, // Disabled by default
			reannounceTime:    defaultDHTConfig.ReannounceTime,
			announceRate:      defaultDHTConfig.AnnounceRate,
			logger:            logger.Named("dht"),
			watchdog:          nil,
			validationEnabled: true,
		},
		options: make([]protocol.DHTOption, 0),
		logger:  logger,
	}, nil
}

// WithPort sets the DHT listening port
func (b *DHTBuilder) WithPort(port int) *DHTBuilder {
	if port < 0 || port > 65535 {
		b.logger.Warn("Invalid DHT port provided, using default",
			zap.Int("provided", port),
			zap.Int("default", DefaultDHTPort))
		return b
	}
	b.config.port = port
	return b
}

// WithAddress sets the DHT listening address
func (b *DHTBuilder) WithAddress(address string) *DHTBuilder {
	if address == "" {
		return b
	}

	if err := validateAddress(address); err != nil {
		b.logger.Warn("Invalid DHT address provided",
			zap.String("address", address),
			zap.Error(err))
		return b
	}

	b.config.address = address
	return b
}

// WithSeedNodes sets the seed nodes for DHT network joining
func (b *DHTBuilder) WithSeedNodes(nodes []string) *DHTBuilder {
	if nodes == nil {
		b.config.seedNodes = []string{}
		return b
	}

	// Validate seed nodes
	validNodes := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node == "" {
			continue
		}
		if err := validateAddress(node); err != nil {
			b.logger.Warn("Invalid seed node address, skipping",
				zap.String("node", node),
				zap.Error(err))
			continue
		}
		validNodes = append(validNodes, node)
	}

	b.config.seedNodes = validNodes
	return b
}

// WithNodeID sets the DHT node ID (empty for random)
func (b *DHTBuilder) WithNodeID(id string) *DHTBuilder {
	b.config.nodeID = id
	return b
}

// WithPeerProtocolPort sets the port for DHT blob protocol clients
func (b *DHTBuilder) WithPeerProtocolPort(port int) *DHTBuilder {
	if port < 1 || port > 65535 {
		b.logger.Warn("Invalid peer protocol port provided, using default",
			zap.Int("provided", port),
			zap.Int("default", DefaultPeerPort))
		return b
	}
	b.config.peerProtocolPort = port
	return b
}

// WithRPCPort sets the DHT RPC server port (0 to disable)
func (b *DHTBuilder) WithRPCPort(port int) *DHTBuilder {
	if port < 0 || port > 65535 {
		b.logger.Warn("Invalid RPC port provided, using default",
			zap.Int("provided", port),
			zap.Int("default", 0))
		return b
	}
	b.config.rpcPort = port
	return b
}

// WithReannounceTime sets the DHT blob reannouncement interval
func (b *DHTBuilder) WithReannounceTime(interval time.Duration) *DHTBuilder {
	if interval <= 0 {
		b.logger.Warn("Invalid reannounce time provided, using default",
			zap.Duration("provided", interval),
			zap.Duration("default", 50*time.Minute))
		return b
	}
	b.config.reannounceTime = interval
	return b
}

// WithAnnounceRate sets the maximum DHT announces per second
func (b *DHTBuilder) WithAnnounceRate(rate int) *DHTBuilder {
	if rate <= 0 {
		b.logger.Warn("Invalid announce rate provided, using default",
			zap.Int("provided", rate),
			zap.Int("default", 10))
		return b
	}
	b.config.announceRate = rate
	return b
}

// WithLogger sets the logger for DHT operations
func (b *DHTBuilder) WithLogger(logger *zap.Logger) *DHTBuilder {
	if logger != nil {
		b.config.logger = logger.Named("dht")
	}
	return b
}

// WithWatchdog sets the DHT watchdog for contact validation
func (b *DHTBuilder) WithWatchdog(watchdog watchdog.DHTWatchdog) *DHTBuilder {
	b.config.watchdog = watchdog
	return b
}

// WithOptions adds advanced protocol options for power users
// These options are applied after all builder configuration
func (b *DHTBuilder) WithOptions(options ...protocol.DHTOption) *DHTBuilder {
	// Simple nil check without reflection
	for _, option := range options {
		if option != nil {
			b.options = append(b.options, option)
		}
	}
	return b
}

// WithValidation enables or disables configuration validation
func (b *DHTBuilder) WithValidation(enabled bool) *DHTBuilder {
	b.config.validationEnabled = enabled
	return b
}

// BuildNode creates a configured DHT node from the builder settings
func (b *DHTBuilder) BuildNode() (protocol.DHTNode, error) {
	if err := b.validateConfig(); err != nil {
		return nil, fmt.Errorf("DHT configuration validation failed: %w", err)
	}

	// Build protocol options from builder config
	options := b.buildProtocolOptions()

	// Add any additional options provided via WithOptions
	options = append(options, b.options...)

	// Create DHT node with all options
	return protocol.NewDHTNodeWithDefaults(options...)
}

// BuildConfig creates a protocol DHTConfig from the builder settings
func (b *DHTBuilder) BuildConfig() (*protocol.DHTConfig, error) {
	if err := b.validateConfig(); err != nil {
		return nil, fmt.Errorf("DHT configuration validation failed: %w", err)
	}

	// Create protocol config
	config := &protocol.DHTConfig{
		Address:          b.getEffectiveAddress(),
		SeedNodes:        b.config.seedNodes,
		NodeID:           b.config.nodeID,
		PeerProtocolPort: b.config.peerProtocolPort,
		RPCPort:          b.config.rpcPort,
		ReannounceTime:   b.config.reannounceTime,
		AnnounceRate:     b.config.announceRate,
		Logger:           convertToLogrus(b.config.logger),
		Watchdog:         b.config.watchdog,
	}

	return config, nil
}

// IsConfigured returns true if the builder has meaningful configuration
func (b *DHTBuilder) IsConfigured() bool {
	return b.config.port > 0 ||
		b.config.address != "" ||
		len(b.config.seedNodes) > 0 ||
		b.config.nodeID != "" ||
		b.config.peerProtocolPort != DefaultPeerPort ||
		b.config.rpcPort != 0
}

// Copy creates a deep copy of the DHTBuilder
func (b *DHTBuilder) Copy() *DHTBuilder {
	// Copy config
	configCopy := *b.config

	// Copy slices
	if b.config.seedNodes != nil {
		configCopy.seedNodes = make([]string, len(b.config.seedNodes))
		copy(configCopy.seedNodes, b.config.seedNodes)
	}

	// Copy options
	optionsCopy := make([]protocol.DHTOption, len(b.options))
	copy(optionsCopy, b.options)

	return &DHTBuilder{
		config:  &configCopy,
		options: optionsCopy,
		logger:  b.logger,
	}
}

// validateConfig performs comprehensive validation of the builder configuration
func (b *DHTBuilder) validateConfig() error {
	if !b.config.validationEnabled {
		return nil
	}

	// Validate port
	if b.config.port < 0 || b.config.port > 65535 {
		return fmt.Errorf("DHT port must be between 0 and 65535, got %d", b.config.port)
	}

	// Validate peer protocol port
	if b.config.peerProtocolPort < 1 || b.config.peerProtocolPort > 65535 {
		return fmt.Errorf("peer protocol port must be between 1 and 65535, got %d", b.config.peerProtocolPort)
	}

	// Validate RPC port
	if b.config.rpcPort < 0 || b.config.rpcPort > 65535 {
		return fmt.Errorf("RPC port must be between 0 and 65535, got %d", b.config.rpcPort)
	}

	// Validate reannounce time
	if b.config.reannounceTime <= 0 {
		return fmt.Errorf("reannounce time must be positive, got %v", b.config.reannounceTime)
	}

	// Validate announce rate
	if b.config.announceRate <= 0 {
		return fmt.Errorf("announce rate must be positive, got %d", b.config.announceRate)
	}

	// Validate address if provided
	if b.config.address != "" {
		if err := validateAddress(b.config.address); err != nil {
			return fmt.Errorf("invalid DHT address: %w", err)
		}
	}

	// Validate seed nodes
	for i, node := range b.config.seedNodes {
		if node == "" {
			return fmt.Errorf("seed node at index %d is empty", i)
		}
		if err := validateAddress(node); err != nil {
			return fmt.Errorf("invalid seed node at index %d '%s': %w", i, node, err)
		}
	}

	return nil
}

// buildProtocolOptions converts builder config to protocol DHT options
func (b *DHTBuilder) buildProtocolOptions() []protocol.DHTOption {
	var options []protocol.DHTOption

	// Add address option
	if address := b.getEffectiveAddress(); address != "" {
		options = append(options, protocol.WithDHTAddress(address))
	}

	// Add seed nodes option
	if len(b.config.seedNodes) > 0 {
		options = append(options, protocol.WithDHTSeedNodes(b.config.seedNodes))
	}

	// Add node ID option
	if b.config.nodeID != "" {
		options = append(options, protocol.WithDHTNodeID(b.config.nodeID))
	}

	// Add peer protocol port option
	options = append(options, protocol.WithDHTPeerProtocolPort(b.config.peerProtocolPort))

	// Add RPC port option
	options = append(options, protocol.WithDHTRPCPort(b.config.rpcPort))

	// Add reannounce time option
	options = append(options, protocol.WithDHTReannounceTime(b.config.reannounceTime))

	// Add announce rate option
	options = append(options, protocol.WithDHTAnnounceRate(b.config.announceRate))

	// Add logger option
	if b.config.logger != nil {
		options = append(options, protocol.WithDHTLogger(b.config.logger))
	}

	// Add watchdog option
	if b.config.watchdog != nil {
		options = append(options, protocol.WithDHTWatchdog(b.config.watchdog))
	}

	return options
}

// getEffectiveAddress returns the effective DHT address
func (b *DHTBuilder) getEffectiveAddress() string {
	if b.config.address != "" {
		return b.config.address
	}

	// Use default address with configured port
	host := extractDHTHost(protocol.DefaultDHTAddress, b.logger)
	return net.JoinHostPort(host, strconv.Itoa(b.config.port))
}

// validateAddress validates an address string with enhanced hostname checking
func validateAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("must be in host:port format: %w", err)
	}

	// Validate port
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return fmt.Errorf("port must be numeric: %w", err)
	}
	if portNum < 1 || portNum > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", portNum)
	}

	// Enhanced host validation
	if net.ParseIP(host) == nil {
		// Allow localhost and hostnames with dots (FQDNs)
		if host != "localhost" && !strings.Contains(host, ".") {
			return fmt.Errorf("host '%s' appears invalid (not IP, localhost, or FQDN)", host)
		}
	}

	return nil
}

// convertToLogrus converts zap.Logger to logrus.Logger for DHT compatibility
// This is a placeholder - in a real implementation, you'd use the existing adapter
func convertToLogrus(zapLogger *zap.Logger) *logrus.Logger {
	// For now, return nil - the protocol package handles nil logger gracefully
	// In a full implementation, you'd use the existing logger adapter
	return nil
}
