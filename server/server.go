package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/gammazero/workerpool"
	"go.lumeweb.com/liblbry"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/storage"

	"go.uber.org/zap"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Default protocol ports
const (
	DefaultPeerPort      = 5567
	DefaultReflectorPort = 5566
	DefaultDHTPort       = 4444
)

// DHT announcer constants
const (
	DefaultDHTAnnouncerWorkers      = 10
	DefaultDHTAnnouncementBatchSize = 1000
	BlobAnnouncementLogInterval     = 1000
)

// Protocol names
const (
	ProtocolPeer      = "peer"
	ProtocolReflector = "reflector"
	ProtocolDHT       = "dht"
)

// BlobManager defines the interface for blob management operations
type BlobManager interface {
	// AddBlob stores a blob and notifies about the addition
	AddBlob(hash string, data []byte) error
	// RemoveBlob deletes a blob and notifies about the removal
	RemoveBlob(hash string) error
}

// Server defines the interface for a liblbry server
type Server interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
}

// PeerConfig contains configuration for Peer protocol
type PeerConfig struct {
	Port int
}

// ReflectorConfig contains configuration for Reflector protocol
type ReflectorConfig struct {
	Port int
}

// DHTConfig contains configuration for DHT protocol
type DHTConfig struct {
	Port      int
	Address   string
	SeedNodes []string
}

// DefaultServer implements the Server interface
type DefaultServer struct {
	BlobManager // Embedded interface

	storage       storage.BlobStore
	acquirer      liblbry.BlobAcquirer
	accessControl storage.AccessControl
	protocols     map[string]any
	logger        *zap.Logger
	dhtWorkers    int

	// DHT management - kept at server level
	dhtAnnouncer protocol.DHTAnnouncer
	dhtNode      protocol.DHTNode
	notifier     protocol.Notifier
	dhtBatchSize int

	// Runtime state
	listeners map[string]net.Listener
	servers   map[string]any // protocol servers
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

// Start starts the server and all configured protocols
func (s *DefaultServer) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.listeners = make(map[string]net.Listener)
	s.servers = make(map[string]any)

	// Initialize notifier system
	s.createNotifier()

	for name, config := range s.protocols {
		var err error
		switch name {
		case ProtocolPeer:
			err = s.startPeer(config.(*PeerConfig))
		case ProtocolReflector:
			err = s.startReflector(config.(*ReflectorConfig))
		case ProtocolDHT:
			err = s.startDHT(config.(*DHTConfig))
		default:
			if stopErr := s.Stop(context.Background()); stopErr != nil {
				s.logger.Error("Error stopping server after startup failure", zap.Error(stopErr))
			}
			return fmt.Errorf("unknown protocol: %s", name)
		}

		if err != nil {
			// Create a fresh context for cleanup to ensure it runs to completion
			stopCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if stopErr := s.Stop(stopCtx); stopErr != nil {
				s.logger.Error("Error stopping server after startup failure", zap.Error(stopErr))
			}
			return fmt.Errorf("failed to start %s protocol: %w", cases.Title(language.Und).String(name), err)
		}
	}

	// Announce all blobs to DHT after all protocols are started
	if s.dhtAnnouncer != nil && s.storage != nil {
		s.announceBlobsToDHT(s.dhtWorkers, s.dhtBatchSize)
	}

	s.logger.Info("Server started successfully")
	return nil
}

// Stop stops the server and all configured protocols gracefully
func (s *DefaultServer) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
	}

	// Close all listeners first to stop accepting new connections
	for name, listener := range s.listeners {
		if err := listener.Close(); err != nil {
			s.logger.Error("Error closing listener", zap.String("protocol", name), zap.Error(err))
		}
	}

	// Shutdown DHT node if it exists
	if dhtNode, exists := s.servers[ProtocolDHT]; exists {
		if node, ok := dhtNode.(protocol.DHTNode); ok {
			node.Shutdown()
			s.logger.Info("DHT node shutdown completed")
		}
	}

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.logger.Info("Server stopped successfully")
		return nil
	case <-ctx.Done():
		s.logger.Info("Server stop timeout reached")
		return ctx.Err()
	}
}

// startPeer starts the Peer protocol handler
func (s *DefaultServer) startPeer(config *PeerConfig) error {
	return s.startTCPProtocol(ProtocolPeer, config.Port, func() protocol.ConnectionHandler {
		return protocol.NewPeerServer(s.storage,
			protocol.WithPeerAccessControl(s.accessControl),
			protocol.WithPeerLogger(s.logger.Named("peer")),
			protocol.WithPeerNotifier(s.notifier),
		)
	})
}

// startReflector starts the Reflector protocol handler
func (s *DefaultServer) startReflector(config *ReflectorConfig) error {
	return s.startTCPProtocol(ProtocolReflector, config.Port, func() protocol.ConnectionHandler {
		return protocol.NewReflectorServer(s.storage,
			protocol.WithReflectorAccessControl(s.accessControl),
			protocol.WithReflectorLogger(s.logger.Named("reflector")),
			protocol.WithReflectorNotifier(s.notifier),
		)
	})
}

// startDHT starts the DHT protocol handler
func (s *DefaultServer) startDHT(config *DHTConfig) error {
	// Check if peer protocol is configured and get the port
	var peerProtocolPort int
	if peerConfig, ok := s.protocols[ProtocolPeer]; ok {
		if peerCfg, ok := peerConfig.(*PeerConfig); ok {
			peerProtocolPort = peerCfg.Port
		}
	}

	// If no peer port was found, use a default
	if peerProtocolPort == 0 {
		peerProtocolPort = DefaultPeerPort
	}

	// Build options slice dynamically
	var opts []protocol.DHTOption
	
	// Determine the address to use
	address := config.Address
	if address == "" {
		// Use default host with specified port
		address = fmt.Sprintf("%s:%d", strings.Split(protocol.DefaultDHTAddress, ":")[0], config.Port)
	}
	
	opts = append(opts,
		protocol.WithDHTAddress(address),
		protocol.WithDHTPeerProtocolPort(peerProtocolPort),
		protocol.WithDHTLogger(s.logger.Named("dht")),
	)

	// Conditionally add seed nodes if they exist
	if len(config.SeedNodes) > 0 {
		opts = append(opts, protocol.WithDHTSeedNodes(config.SeedNodes))
	}

	// Create DHT node with all options at once
	dhtNode, err := protocol.NewDHTNodeWithDefaults(opts...)
	if err != nil {
		return fmt.Errorf("failed to create DHT node: %w", err)
	}

	// Start the DHT node
	if err := dhtNode.Start(); err != nil {
		return fmt.Errorf("failed to start DHT node: %w", err)
	}

	// Store the DHT node in servers map for later access
	s.servers[ProtocolDHT] = dhtNode
	s.dhtNode = dhtNode

	// Create DHT announcer
	s.dhtAnnouncer = protocol.NewDefaultDHTAnnouncer(dhtNode)

	// Register DHT notifier with the existing group notifier
	if s.notifier != nil {
		if group, ok := s.notifier.(*protocol.GroupNotifier); ok {
			group.AddNotifier(protocol.NewDHTNotifier(s.dhtAnnouncer, s.logger.Named("dht-notifier")))
		}
	}

	s.logger.Info("DHT protocol started", zap.Int("port", config.Port), zap.Int("peer_port", peerProtocolPort))
	return nil
}

// createNotifier initializes the notifier system
func (s *DefaultServer) createNotifier() {
	// Create a GroupNotifier to manage multiple notifiers
	groupNotifier := protocol.NewGroupNotifier()

	// Add logging notifier
	loggingNotifier := protocol.NewLoggingNotifier(s.logger.Named("notifier"))

	// Add the logging notifier to the group
	groupNotifier.AddNotifier(loggingNotifier)

	s.notifier = groupNotifier
}

// announceBlobsToDHT enumerates all blobs from storage and announces them to the DHT
func (s *DefaultServer) announceBlobsToDHT(workerCount int, batchSize int) {
	s.logger.Debug("Starting DHT blob announcement process")

	offset := 0
	totalAnnounced := 0

	// Create a worker pool with configurable workers for concurrent blob announcements
	pool := workerpool.New(workerCount)

	for {
		// Get a batch of blob hashes using pagination
		blobHashes, err := s.storage.List(offset, batchSize)
		if err != nil {
			if liblbryerrors.IsEndOfListError(err) {
				// Reached end of list, break the loop
				break
			}
			s.logger.Error("Failed to list blobs from storage", zap.Error(err))
			pool.StopWait()
			return
		}

		if len(blobHashes) == 0 {
			// No more blobs to process
			break
		}

		s.logger.Debug("Processing batch of blobs",
			zap.Int("batch_size", len(blobHashes)),
			zap.Int("offset", offset))

		// Submit all blob announcements in this batch to the worker pool
		for i, hash := range blobHashes {
			hash := hash // capture loop variable
			globalIndex := offset + i

			pool.Submit(func() {
				// Announce to DHT using the LBRY hash directly
				// The DHT announcer expects LBRY hash format, not multihash
				err := s.dhtAnnouncer.AnnounceBlob(hash)
				if err != nil {
					s.logger.Warn("Failed to announce blob to DHT",
						zap.String("hash", hash),
						zap.Error(err),
						zap.Int("global_index", globalIndex))
					return
				}

				// Log successful announcement every BlobAnnouncementLogInterval blobs
				if globalIndex%BlobAnnouncementLogInterval == 0 {
					s.logger.Debug("Announced blob to DHT",
						zap.String("hash", hash),
						zap.Int("global_index", globalIndex))
				}
			})
		}

		totalAnnounced += len(blobHashes)
		offset += batchSize
	}

	// Wait for all jobs to complete
	pool.StopWait()

	s.logger.Debug("Completed DHT blob announcement process", zap.Int("total_announced", totalAnnounced))
}

// AddBlob stores a blob and notifies about the addition
func (s *DefaultServer) AddBlob(hash string, data []byte) error {
	// Validate hash
	if hash == "" {
		return fmt.Errorf("hash cannot be empty")
	}
	if len(data) == 0 {
		return fmt.Errorf("blob data cannot be empty")
	}

	// Store the blob
	err := s.storage.Put(hash, data)
	if err != nil {
		s.logger.Error("Failed to store blob",
			zap.String("hash", hash),
			zap.Error(err))
		return fmt.Errorf("failed to store blob %s: %w", hash, err)
	}

	s.logger.Debug("Successfully stored blob", zap.String("hash", hash))

	// Notify about blob addition
	protocol.NotifyBlob(s.notifier, s.logger, protocol.NOTIFY_BLOB_ADDED, hash)

	return nil
}

// RemoveBlob deletes a blob and notifies about the removal
func (s *DefaultServer) RemoveBlob(hash string) error {
	// Validate hash
	if hash == "" {
		return fmt.Errorf("hash cannot be empty")
	}

	// Delete the blob from storage
	err := s.storage.Delete(hash)
	if err != nil {
		s.logger.Error("Failed to delete blob",
			zap.String("hash", hash),
			zap.Error(err))
		return fmt.Errorf("failed to delete blob %s: %w", hash, err)
	}

	s.logger.Debug("Successfully deleted blob", zap.String("hash", hash))

	// Notify about blob removal
	protocol.NotifyBlob(s.notifier, s.logger, protocol.NOTIFY_BLOB_REMOVED, hash)

	return nil
}

// startTCPProtocol starts a generic TCP protocol handler
func (s *DefaultServer) startTCPProtocol(protocolName string, port int, serverFactory func() protocol.ConnectionHandler) error {
	addr := fmt.Sprintf(":%d", port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.listeners[protocolName] = listener

	// Create protocol server
	server := serverFactory()
	s.servers[protocolName] = server

	// Start handling connections
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := listener.Accept()
			if err != nil {
				// Check if listener was closed
				select {
				case <-s.ctx.Done():
					return
				default:
					s.logger.Error("Error accepting connection", zap.String("protocol", protocolName), zap.Error(err))
					continue
				}
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				server.HandleConnection(conn)
			}()
		}
	}()

	s.logger.Info("Protocol started", zap.String("protocol", protocolName), zap.Int("port", port))
	return nil
}
