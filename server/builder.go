package server

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"strings"

	"github.com/knadh/koanf/v2"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/blob/transfer"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/storage/disk"
	"go.lumeweb.com/liblbry/storage/memory"
	"go.uber.org/zap"
)

// ServerBuilder provides a fluent interface for building a Server
type ServerBuilder struct {
	storage            storage.BlobStore
	acquirer           liblbry.BlobAcquirer
	acquirerFactory    AcquirerFactory
	useDefaultAcquirer bool
	accessControl      storage.AccessControl
	config             map[string]any
	logger             *zap.Logger
	dhtWorkers         int
	dhtBatchSize       int
	transferOptions    []transfer.TransferOption
	dhtOptions         []protocol.DHTOption
}

// NewServerBuilder creates a new ServerBuilder instance
func NewServerBuilder() *ServerBuilder {
	return &ServerBuilder{
		storage:            nil,
		acquirer:           nil,
		acquirerFactory:    nil,
		useDefaultAcquirer: false,
		accessControl:      nil,
		config:             make(map[string]any),
		logger:             zap.NewNop(),                    // Default to no-op logger
		dhtWorkers:         DefaultDHTAnnouncerWorkers,      // Default value
		dhtBatchSize:       DefaultDHTAnnouncementBatchSize, // Default batch size
		transferOptions:    make([]transfer.TransferOption, 0),
		dhtOptions:         make([]protocol.DHTOption, 0),
	}
}

// WithStorage sets the blob storage for the server
func (b *ServerBuilder) WithStorage(store storage.BlobStore) *ServerBuilder {
	b.storage = store
	return b
}

// WithAcquirer sets the blob acquirer for the server
func (b *ServerBuilder) WithAcquirer(acquirer liblbry.BlobAcquirer) *ServerBuilder {
	b.acquirer = acquirer
	return b
}

// WithAcquirerFactory sets a factory function that creates the blob acquirer
// This allows the acquirer to be created after the DHT is available during server build
func (b *ServerBuilder) WithAcquirerFactory(factory AcquirerFactory) *ServerBuilder {
	b.acquirerFactory = factory
	return b
}

// WithDefaultAcquirer enables creation of a default acquirer with sensible defaults
// The default acquirer will be configured with transfers based on enabled protocols.
// Note: DHT-based transfers require a DHT node to be configured; without DHT,
// the acquirer will be created with an empty transfer set.
func (b *ServerBuilder) WithDefaultAcquirer() *ServerBuilder {
	b.useDefaultAcquirer = true
	return b
}

// WithAccessControl sets the access control for the server
func (b *ServerBuilder) WithAccessControl(ac storage.AccessControl) *ServerBuilder {
	b.accessControl = ac
	return b
}

// getOrCreateDHTConfig retrieves the existing DHT config or creates a new one with default values
// Returns nil if an existing DHT node (from WithExistingDHT) is already configured
func (b *ServerBuilder) getOrCreateDHTConfig() *DHTConfig {
	// Check if DHT protocol config already exists
	dhtConfig, exists := b.config[ProtocolDHT]

	if exists {
		// If it exists, check if it's a DHTNode (from WithExistingDHT)
		if _, isNode := dhtConfig.(protocol.DHTNode); isNode {
			// This means WithExistingDHT was used, so we ignore conflicting DHT configuration
			// and prioritize the existing DHT node
			return nil
		}

		// If it's a *DHTConfig, return it
		if dhtCfg, ok := dhtConfig.(*DHTConfig); ok {
			return dhtCfg
		}
		// If it's neither a *DHTConfig nor a DHTNode, create a new one (shouldn't happen in practice)
	}

	// If it doesn't exist, create new config with default values
	newConfig := &DHTConfig{
		Port:      DefaultDHTPort,
		Address:   "",
		SeedNodes: []string{}, // Empty slice instead of nil
	}
	b.config[ProtocolDHT] = newConfig

	return newConfig
}

// applyDHTOptionsToConfig applies DHT options to a protocol config and returns the modified config
// This helper function DRYs the pattern of creating temporary configs to extract option values
func applyDHTOptionsToConfig(config *protocol.DHTConfig, options []protocol.DHTOption) *protocol.DHTConfig {
	// Apply all DHT options to the config
	for _, option := range options {
		option(config)
	}
	return config
}

// extractSeedNodesFromOptions extracts seed nodes from stored DHT options and applies them to server config.
// This function uses a targeted approach that only processes WithDHTSeedNodes options to avoid
// validation panics from other options like WithDHTAddress.
func (b *ServerBuilder) extractSeedNodesFromOptions(dhtConfig *DHTConfig) {
	seedNodes := b.extractSeedNodesFromOptionsOnly()
	if len(seedNodes) > 0 {
		dhtConfig.SeedNodes = seedNodes
	}
}

// extractSeedNodesFromOptionsOnly extracts seed nodes from stored DHT options without applying
// any options that could trigger validation panics. This is a safer and more efficient approach
// than creating a temporary config and applying all options.
func (b *ServerBuilder) extractSeedNodesFromOptionsOnly() []string {
	for _, option := range b.dhtOptions {
		// Create a temporary config to test what this option does
		tempConfig := &protocol.DHTConfig{
			SeedNodes:        []string{},
			Address:          "127.0.0.1:4444",
			PeerProtocolPort: DefaultDHTPort,
			RPCPort:          DefaultDHTPort,
		}

		// Store original seed nodes to detect if this option changes them
		originalSeedNodes := make([]string, len(tempConfig.SeedNodes))
		copy(originalSeedNodes, tempConfig.SeedNodes)

		// Apply the option
		option(tempConfig)

		// If seed nodes changed and this wasn't just a no-op, this is likely a WithDHTSeedNodes option
		if !reflect.DeepEqual(originalSeedNodes, tempConfig.SeedNodes) && len(tempConfig.SeedNodes) > 0 {
			return tempConfig.SeedNodes
		}
	}
	return nil
}

// withProtocolConfig adds a protocol configuration with the specified port and default.
// If multiple ports are provided, only the first is used.
func (b *ServerBuilder) withProtocolConfig(protocolName string, defaultPort int, port []int, configFactory func(int) any) *ServerBuilder {
	p := defaultPort
	if len(port) > 0 {
		p = port[0]
	}
	b.config[protocolName] = configFactory(p)
	return b
}

// WithPeer adds a Peer protocol handler on the specified port
func (b *ServerBuilder) WithPeer(port ...int) *ServerBuilder {
	return b.withProtocolConfig(ProtocolPeer, DefaultPeerPort, port, func(p int) any {
		return &PeerConfig{Port: p}
	})
}

// WithReflector adds a Reflector protocol handler on the specified port
func (b *ServerBuilder) WithReflector(port ...int) *ServerBuilder {
	return b.withProtocolConfig(ProtocolReflector, DefaultReflectorPort, port, func(p int) any {
		return &ReflectorConfig{Port: p}
	})
}

// WithDHT adds a DHT protocol handler on the specified port
func (b *ServerBuilder) WithDHT(port ...int) *ServerBuilder {
	// Get the port to use
	p := DefaultDHTPort
	if len(port) > 0 {
		p = port[0]
	}

	// Get or create DHT config to ensure DHT protocol is enabled
	dhtConfig := b.getOrCreateDHTConfig()

	// Check if we're trying to configure DHT when an existing node was set
	if dhtConfig == nil {
		// This means WithExistingDHT was used, so we ignore conflicting DHT configuration
		// and prioritize the existing DHT node
		return b
	}

	// Update the port for backward compatibility
	dhtConfig.Port = p

	// Extract seed nodes from stored DHT options and apply to server config
	// This allows immediate verification in tests
	b.extractSeedNodesFromOptions(dhtConfig)

	// Also add the DHT address option using the port for consistency with new approach
	// Use net.SplitHostPort for robustness against IPv6 literals and future format changes
	host, _, err := net.SplitHostPort(protocol.DefaultDHTAddress)
	if err != nil {
		// Fallback to the original approach if SplitHostPort fails
		host = strings.Split(protocol.DefaultDHTAddress, ":")[0]
	}
	address := net.JoinHostPort(host, fmt.Sprintf("%d", p))
	return b.WithDHTOptions(protocol.WithDHTAddress(address))
}

// WithDHTAddress sets the DHT address
func (b *ServerBuilder) WithDHTAddress(address string) *ServerBuilder {
	// Use the protocol package DHT option internally
	return b.WithDHTOptions(protocol.WithDHTAddress(address))
}

// WithFixedPeerPort adds a fixed peer port in addition to the regular peer port
func (b *ServerBuilder) WithFixedPeerPort(port int) *ServerBuilder {
	// Use the same pattern as other config methods - get or create the config
	peerConfig := b.getOrCreatePeerConfig()
	peerConfig.FixedPort = port
	return b
}

// getOrCreatePeerConfig retrieves the existing PeerConfig or creates a new one with default values
func (b *ServerBuilder) getOrCreatePeerConfig() *PeerConfig {
	// Check if Peer protocol config already exists
	peerConfig, exists := b.config[ProtocolPeer]

	if exists {
		// If it exists, return the existing config
		if cfg, ok := peerConfig.(*PeerConfig); ok {
			return cfg
		}
		// If it's not the right type, create a new one (shouldn't happen in practice)
	}

	// If it doesn't exist, create new config with default values
	newConfig := &PeerConfig{
		Port:      DefaultPeerPort,
		FixedPort: 0, // Default to disabled
	}
	b.config[ProtocolPeer] = newConfig

	return newConfig
}

// WithLogger sets the logger for the server
func (b *ServerBuilder) WithLogger(logger *zap.Logger) *ServerBuilder {
	if logger == nil {
		return b
	}
	b.logger = logger
	return b
}

// addOptionsWithNilCheck is a helper function that adds options to a slice while filtering out nil values
// This DRYs up the pattern used across multiple option methods
func addOptionsWithNilCheck[T any](logger *zap.Logger, options []T, newOptions []T) []T {
	for _, option := range newOptions {
		// Use reflection to check for nil since direct comparison with generic type doesn't work
		if any(option) == nil {
			logger.Warn("Ignoring nil option")
			continue
		}
		options = append(options, option)
	}
	return options
}

// WithTransferOptions adds transfer options that will be applied to all transfer implementations
// These options are applied during transfer creation and allow fine-tuning of transfer behavior
func (b *ServerBuilder) WithTransferOptions(options ...transfer.TransferOption) *ServerBuilder {
	b.transferOptions = addOptionsWithNilCheck(b.logger, b.transferOptions, options)
	return b
}

// WithDHTOptions adds DHT options that will be applied to the DHT node configuration
// These options are applied during DHT node creation and allow fine-tuning of DHT behavior
//
// Example usage:
//
//	builder := NewServerBuilder().
//	  WithDHT(4444).
//	  WithDHTOptions(
//	      protocol.WithDHTSeedNodes([]string{"node1.example.com:4444"}),
//	      protocol.WithDHTLogger(logger),
//	      protocol.WithDHTWatchdog(watchdog),
//	  )
//
// This replaces the need for individual wrapper methods and allows direct use of
// protocol package DHT options for more flexibility.
func (b *ServerBuilder) WithDHTOptions(options ...protocol.DHTOption) *ServerBuilder {
	b.dhtOptions = addOptionsWithNilCheck(b.logger, b.dhtOptions, options)
	return b
}

// WithDHTWorkers sets the number of workers for DHT announcements
func (b *ServerBuilder) WithDHTWorkers(workers int) *ServerBuilder {
	if workers <= 0 {
		workers = DefaultDHTAnnouncerWorkers
	}
	b.dhtWorkers = workers
	return b
}

// WithDHTBatchSize sets the batch size for DHT announcements
func (b *ServerBuilder) WithDHTBatchSize(batchSize int) *ServerBuilder {
	if batchSize <= 0 {
		batchSize = DefaultDHTAnnouncementBatchSize
	}
	b.dhtBatchSize = batchSize
	return b
}

// WithExistingDHT configures the server to use an existing DHT node
// This allows integration with externally managed DHT instances
//
// Example:
//
//	builder := NewServerBuilder().
//	  WithExistingDHT(existingDHTNode).
//	  WithStorage(storage).
//	  WithAcquirer(acquirer)
//
// The existing DHT node will be used directly instead of creating a new one
func (b *ServerBuilder) WithExistingDHT(dhtNode protocol.DHTNode) *ServerBuilder {
	b.config[ProtocolDHT] = dhtNode
	return b
}

// createDefaultTransfers creates a slice of transfer.Transfer instances based on enabled protocols.
// Returns an empty slice if no DHT node is provided, as DHT is required for peer-based transfers.
func (b *ServerBuilder) createDefaultTransfers(dhtNode protocol.DHTNode) ([]transfer.Transfer, error) {
	return createDefaultTransfersWithLogger(dhtNode, b.logger, b.transferOptions)
}

// createDefaultTransfersWithLogger creates a slice of transfer.Transfer instances based on enabled protocols.
// This is a standalone helper function that avoids capturing the ServerBuilder instance.
// Returns an empty slice if no DHT node is provided, as DHT is required for peer-based transfers.
func createDefaultTransfersWithLogger(dhtNode protocol.DHTNode, logger *zap.Logger, transferOptions []transfer.TransferOption) ([]transfer.Transfer, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	var transfers []transfer.Transfer

	// Add peer transfer if DHT is enabled
	if dhtNode != nil {
		// Create a default peer client factory
		peerClientFactory := protocol.DefaultPeerClientFactory(
			protocol.WithClientLogger(logger.Named("peer_client")),
		)
		peerTransfer, err := transfer.NewPeerTransfer(dhtNode, peerClientFactory,
			transfer.WithPeerTransferLogger(logger.Named("peer_transfer")),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create peer transfer: %w", err)
		}

		// Apply transfer options to the peer transfer
		for _, option := range transferOptions {
			if err := option.Apply(peerTransfer); err != nil {
				logger.Warn(
					"Failed to apply transfer option",
					zap.Error(err),
					zap.String("option_type", fmt.Sprintf("%T", option)),
				)
				// Continue with other options even if one fails
			}
		}

		transfers = append(transfers, peerTransfer)
	}

	return transfers, nil
}

// Build creates a Server instance from the builder configuration
func (b *ServerBuilder) Build() (Server, error) {
	if b.storage == nil {
		return nil, errors.New("storage is required")
	}

	if len(b.config) == 0 {
		return nil, errors.New("at least one protocol must be configured")
	}

	// Set safe default for accessControl if nil to prevent runtime panics
	// when protocol servers call AccessControl.Allow()
	if b.accessControl == nil {
		b.accessControl = storage.NewAllowAllAccess()
	}

	// Validate acquirer configuration
	if b.acquirer != nil && b.acquirerFactory != nil {
		return nil, errors.New("cannot specify both acquirer and acquirer factory")
	}
	if b.acquirer != nil && b.useDefaultAcquirer {
		return nil, errors.New("cannot specify both acquirer and default acquirer")
	}
	if b.acquirerFactory != nil && b.useDefaultAcquirer {
		return nil, errors.New("cannot specify both acquirer factory and default acquirer")
	}

	// Copy DHT options to avoid reference issues
	dhtOptions := make([]protocol.DHTOption, len(b.dhtOptions))
	copy(dhtOptions, b.dhtOptions)

	server := &DefaultServer{
		storage:       b.storage,
		acquirer:      b.acquirer,
		accessControl: b.accessControl,
		config:        b.config,
		logger:        b.logger.Named("server"),
		dhtWorkers:    b.dhtWorkers,
		dhtBatchSize:  b.dhtBatchSize,
		dhtOptions:    dhtOptions,
	}

	// Set acquirer creation functions for lazy initialization
	if b.acquirerFactory != nil {
		server.acquirerFactory = b.acquirerFactory
	}
	if b.useDefaultAcquirer {
		// Capture only the logger and transfer options to avoid long-lived builder capture
		logger := b.logger
		transferOptions := make([]transfer.TransferOption, len(b.transferOptions))
		copy(transferOptions, b.transferOptions)
		server.acquirerFactory = func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
			transfers, err := createDefaultTransfersWithLogger(dhtNode, logger, transferOptions)
			if err != nil {
				return nil, fmt.Errorf("failed to create default transfers: %w", err)
			}
			return liblbry.NewBlobAcquirer(transfers, store)
		}
	}

	return server, nil
}

// Preset helper functions for common server configurations

// baseMemoryBuilder creates a server builder with common memory storage defaults:
// memory storage + AllowAllAccess for convenient non-production setups
func baseMemoryBuilder() *ServerBuilder {
	return NewServerBuilder().
		WithStorage(memory.NewMemoryStore()).
		WithAccessControl(storage.NewAllowAllAccess())
}

// DevelopmentBuilder creates a server builder with development-friendly defaults
func DevelopmentBuilder() *ServerBuilder {
	return baseMemoryBuilder().WithPeer()
}

// ProductionBuilder creates a server builder with production-ready defaults
func ProductionBuilder(storagePath string, logger *zap.Logger) (*ServerBuilder, error) {
	return ProductionBuilderWithAccess(storagePath, logger, storage.NewAllowAllAccess())
}

// ProductionBuilderWithAccess creates a server builder with production-ready defaults and custom access control
func ProductionBuilderWithAccess(storagePath string, logger *zap.Logger, accessControl storage.AccessControl) (*ServerBuilder, error) {
	// Use provided logger or default to no-op logger
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create disk storage factory using the helper from store.go
	factory, err := liblbry.CreateStorageFactory[disk.DiskStoreFactory](logger)
	if err != nil {
		return nil, err
	}

	// Create configuration with the storage path
	config := koanf.New(".")
	if err := config.Set("path", storagePath); err != nil {
		return nil, err
	}

	// Create the disk store
	store, err := factory.CreateStore(config)
	if err != nil {
		return nil, err
	}

	return NewServerBuilder().
		WithStorage(store).
		WithAccessControl(accessControl).
		WithPeer().
		WithReflector().
		WithLogger(logger), nil
}

// TestBuilder creates a minimal server builder for testing with a default Peer protocol
// Callers can add additional config using the builder methods if needed
func TestBuilder() *ServerBuilder {
	return baseMemoryBuilder().WithPeer()
}

// PeerOnlyBuilder creates a server with only Peer protocol
func PeerOnlyBuilder(port ...int) *ServerBuilder {
	return baseMemoryBuilder().WithPeer(port...)
}

// ReflectorOnlyBuilder creates a server with only Reflector protocol
func ReflectorOnlyBuilder(port ...int) *ServerBuilder {
	return baseMemoryBuilder().WithReflector(port...)
}

// AllProtocolsBuilder creates a server with all config enabled
func AllProtocolsBuilder(peerPort, reflectorPort, dhtPort int) *ServerBuilder {
	return baseMemoryBuilder().
		WithPeer(peerPort).
		WithReflector(reflectorPort).
		WithDHT(dhtPort)
}
