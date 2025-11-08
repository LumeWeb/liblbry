package server

import (
	"errors"

	"github.com/knadh/koanf/v2"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/storage/disk"
	"go.lumeweb.com/liblbry/storage/memory"
	"go.uber.org/zap"
)

// ServerBuilder provides a fluent interface for building a Server
type ServerBuilder struct {
	storage       storage.BlobStore
	acquirer      liblbry.BlobAcquirer
	accessControl storage.AccessControl
	protocols     map[string]any
	logger        *zap.Logger
	dhtWorkers    int
	dhtBatchSize  int
}

// NewServerBuilder creates a new ServerBuilder instance
func NewServerBuilder() *ServerBuilder {
	return &ServerBuilder{
		storage:       nil,
		acquirer:      nil,
		accessControl: nil,
		protocols:     make(map[string]any),
		logger:        zap.NewNop(),                    // Default to no-op logger
		dhtWorkers:    DefaultDHTAnnouncerWorkers,      // Default value
		dhtBatchSize:  DefaultDHTAnnouncementBatchSize, // Default batch size
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

// WithAccessControl sets the access control for the server
func (b *ServerBuilder) WithAccessControl(ac storage.AccessControl) *ServerBuilder {
	b.accessControl = ac
	return b
}

// getOrCreateDHTConfig retrieves the existing DHT config or creates a new one with default values
func (b *ServerBuilder) getOrCreateDHTConfig() *DHTConfig {
	// Check if DHT protocol config already exists
	dhtConfig, exists := b.protocols[ProtocolDHT]
	
	if exists {
		// If it exists, return the existing config
		if dhtCfg, ok := dhtConfig.(*DHTConfig); ok {
			return dhtCfg
		}
		// If it's not the right type, create a new one (shouldn't happen in practice)
	}
	
	// If it doesn't exist, create new config with default values
	newConfig := &DHTConfig{
		Port:      DefaultDHTPort,
		Address:   "",
		SeedNodes: []string{}, // Empty slice instead of nil
	}
	b.protocols[ProtocolDHT] = newConfig
	
	return newConfig
}

// withProtocolConfig adds a protocol configuration with the specified port and default.
// If multiple ports are provided, only the first is used.
func (b *ServerBuilder) withProtocolConfig(protocolName string, defaultPort int, port []int, configFactory func(int) any) *ServerBuilder {
	p := defaultPort
	if len(port) > 0 {
		p = port[0]
	}
	b.protocols[protocolName] = configFactory(p)
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
	
	// Update seed nodes
	dhtConfig.SeedNodes = seedNodes
	
	return b
}

// WithDHTAddress sets the DHT address
func (b *ServerBuilder) WithDHTAddress(address string) *ServerBuilder {
	// Get or create DHT config
	dhtConfig := b.getOrCreateDHTConfig()
	
	// Update address
	dhtConfig.Address = address
	
	return b
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

// Build creates a Server instance from the builder configuration
func (b *ServerBuilder) Build() (Server, error) {
	if b.storage == nil {
		return nil, errors.New("storage is required")
	}

	if len(b.protocols) == 0 {
		return nil, errors.New("at least one protocol must be configured")
	}

	// Set safe default for accessControl if nil to prevent runtime panics
	// when protocol servers call AccessControl.Allow()
	if b.accessControl == nil {
		b.accessControl = storage.NewAllowAllAccess()
	}

	return &DefaultServer{
		storage:       b.storage,
		acquirer:      b.acquirer,
		accessControl: b.accessControl,
		protocols:     b.protocols,
		logger:        b.logger.Named("server"),
		dhtWorkers:    b.dhtWorkers,
		dhtBatchSize:  b.dhtBatchSize,
	}, nil
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
// Callers can add additional protocols using the builder methods if needed
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

// AllProtocolsBuilder creates a server with all protocols enabled
func AllProtocolsBuilder(peerPort, reflectorPort, dhtPort int) *ServerBuilder {
	return baseMemoryBuilder().
		WithPeer(peerPort).
		WithReflector(reflectorPort).
		WithDHT(dhtPort)
}
