package server

import (
	"errors"

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
func (b *ServerBuilder) WithAcquirerFactory(factory func(protocol.DHTNode, storage.BlobStore) (liblbry.BlobAcquirer, error)) *ServerBuilder {
	b.acquirerFactory = factory
	return b
}

// WithDefaultAcquirer enables creation of a default acquirer with sensible defaults
// The default acquirer will be configured with transfers based on enabled protocols
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

	// Get or create DHT config
	dhtConfig := b.getOrCreateDHTConfig()

	// Check if we're trying to configure DHT when an existing node was set
	if dhtConfig == nil {
		// This means WithExistingDHT was used, so we ignore conflicting DHT configuration
		// and prioritize the existing DHT node
		return b
	}

	// Update the port while preserving existing values
	dhtConfig.Port = p

	return b
}

// WithDHTSeedNodes sets seed nodes for the DHT protocol
func (b *ServerBuilder) WithDHTSeedNodes(seedNodes ...string) *ServerBuilder {
	// Handle nil seedNodes by converting to empty slice
	if seedNodes == nil {
		seedNodes = []string{}
	}

	// Get or create DHT config
	dhtConfig := b.getOrCreateDHTConfig()

	// Check if we're trying to configure DHT when an existing node was set
	if dhtConfig == nil {
		// This means WithExistingDHT was used, so we ignore conflicting DHT configuration
		// and prioritize the existing DHT node
		return b
	}

	// Update seed nodes
	dhtConfig.SeedNodes = seedNodes

	return b
}

// WithDHTAddress sets the DHT address
func (b *ServerBuilder) WithDHTAddress(address string) *ServerBuilder {
	// Get or create DHT config
	dhtConfig := b.getOrCreateDHTConfig()

	// Check if we're trying to configure DHT when an existing node was set
	if dhtConfig == nil {
		// This means WithExistingDHT was used, so we ignore conflicting DHT configuration
		// and prioritize the existing DHT node
		return b
	}

	// Update address
	dhtConfig.Address = address

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

// createDefaultTransfers creates a slice of transfer.Transfer instances based on enabled protocols
func (b *ServerBuilder) createDefaultTransfers(dhtNode protocol.DHTNode) []transfer.Transfer {
	var transfers []transfer.Transfer

	// Add peer transfer if DHT is enabled
	if dhtNode != nil {
		// Create a default peer client - this would need to be configurable in the future
		peerClient := protocol.NewPeerClient(
			protocol.WithClientLogger(b.logger.Named("peer_client")),
		)
		peerTransfer := transfer.NewPeerTransfer(dhtNode, peerClient,
			transfer.WithPeerTransferLogger(b.logger.Named("peer_transfer")),
		)
		transfers = append(transfers, peerTransfer)
	}

	return transfers
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

	server := &DefaultServer{
		storage:       b.storage,
		acquirer:      b.acquirer,
		accessControl: b.accessControl,
		config:        b.config,
		logger:        b.logger.Named("server"),
		dhtWorkers:    b.dhtWorkers,
		dhtBatchSize:  b.dhtBatchSize,
	}

	// Set acquirer creation functions for lazy initialization
	if b.acquirerFactory != nil {
		server.acquirerFactory = b.acquirerFactory
	}
	if b.useDefaultAcquirer {
		server.acquirerFactory = func(dhtNode protocol.DHTNode, store storage.BlobStore) (liblbry.BlobAcquirer, error) {
			transfers := b.createDefaultTransfers(dhtNode)
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
