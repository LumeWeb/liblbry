package server

import (
	"errors"
	"fmt"
	"reflect"

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
	dhtBuilder         *DHTBuilder
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
		dhtBuilder:         nil, // DHTBuilder created on demand
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

// getOrCreateDHTBuilder retrieves existing DHTBuilder or creates a new one
func (b *ServerBuilder) getOrCreateDHTBuilder() (*DHTBuilder, error) {
	if b.dhtBuilder == nil {
		var err error
		b.dhtBuilder, err = NewDHTBuilder(b.logger)
		if err != nil {
			// Return the error to let caller handle it
			return nil, fmt.Errorf("failed to create DHTBuilder: %w", err)
		}
	}
	return b.dhtBuilder, nil
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
	// Check if existing DHT node was configured
	if _, exists := b.config[ProtocolDHT]; exists {
		if _, isNode := b.config[ProtocolDHT].(protocol.DHTNode); isNode {
			// WithExistingDHT was used, ignore conflicting configuration
			return b
		}
	}

	// Get the port to use
	p := DefaultDHTPort
	if len(port) > 0 {
		p = port[0]
	}

	// Get or create DHTBuilder and configure port
	dhtBuilder, err := b.getOrCreateDHTBuilder()
	if err != nil {
		b.logger.Error("Failed to create DHTBuilder", zap.Error(err))
		return b
	}
	dhtBuilder.WithPort(p)

	// Store DHT config in server config map for backward compatibility
	dhtConfig := &DHTConfig{
		Port:      p,
		Address:   "",
		SeedNodes: dhtBuilder.config.seedNodes,
	}
	b.config[ProtocolDHT] = dhtConfig

	return b
}

// WithDHTAddress sets the DHT address
func (b *ServerBuilder) WithDHTAddress(address string) *ServerBuilder {
	dhtBuilder, err := b.getOrCreateDHTBuilder()
	if err != nil {
		b.logger.Error("Failed to create DHTBuilder", zap.Error(err))
		return b
	}
	dhtBuilder.WithAddress(address)

	// Update server config if it exists
	if dhtConfig, exists := b.config[ProtocolDHT]; exists {
		if config, ok := dhtConfig.(*DHTConfig); ok {
			config.Address = address
		}
	}

	return b
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

	// Propagate logger to existing DHTBuilder to keep logging in sync
	if b.dhtBuilder != nil {
		b.dhtBuilder.WithLogger(logger)
	}

	return b
}

// addOptionsWithNilCheck is a helper function that adds options to a slice while filtering out nil values
// This DRYs up the pattern used across multiple option methods
func addOptionsWithNilCheck[T any](logger *zap.Logger, options []T, newOptions []T) []T {
	for _, option := range newOptions {
		// Use reflection to check for nil including typed-nil interface values
		v := reflect.ValueOf(option)
		if !v.IsValid() {
			logger.Warn("Ignoring nil option", zap.String("option_type", fmt.Sprintf("%T", option)))
			continue
		}

		// Check if the value type can be nil and if it is nil
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Func, reflect.Slice, reflect.Map, reflect.Chan:
			if v.IsNil() {
				logger.Warn("Ignoring nil option", zap.String("option_type", fmt.Sprintf("%T", option)))
				continue
			}
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

// DHTBuilder returns the internal DHTBuilder for advanced configuration
// This allows access to all DHTBuilder methods for fine-grained control
//
// Example usage:
//
//	builder := NewServerBuilder().
//	  DHTBuilder().
//	  WithPort(4444).
//	  WithSeedNodes([]string{"node1.example.com:4444"}).
//	  WithReannounceTime(30 * time.Minute)
func (b *ServerBuilder) DHTBuilder() *DHTBuilder {
	dhtBuilder, err := b.getOrCreateDHTBuilder()
	if err != nil {
		b.logger.Error("Failed to create DHTBuilder", zap.Error(err))
		return nil
	}
	return dhtBuilder
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
	dhtBuilder, err := b.getOrCreateDHTBuilder()
	if err != nil {
		b.logger.Error("Failed to create DHTBuilder", zap.Error(err))
		return b
	}
	dhtBuilder.WithOptions(options...)
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

	// Handle DHT configuration
	var dhtOptions []protocol.DHTOption
	if b.dhtBuilder != nil {
		// Check if DHTBuilder has meaningful configuration
		if b.dhtBuilder.IsConfigured() {
			// Build DHT options from DHTBuilder
			_, err := b.dhtBuilder.BuildConfig()
			if err != nil {
				return nil, fmt.Errorf("failed to build DHT configuration: %w", err)
			}

			// Update server config with DHTBuilder settings
			serverDHTConfig := &DHTConfig{
				Port:      b.dhtBuilder.config.port,
				Address:   b.dhtBuilder.config.address,
				SeedNodes: b.dhtBuilder.config.seedNodes,
			}
			b.config[ProtocolDHT] = serverDHTConfig

			// Get protocol options for server creation
			dhtOptions = b.dhtBuilder.buildProtocolOptions()
			// Merge any additional options provided via WithDHTOptions
			dhtOptions = append(dhtOptions, b.dhtBuilder.options...)
		} else {
			// DHTBuilder exists but not configured - use empty options
			dhtOptions = []protocol.DHTOption{}
		}
	}

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
