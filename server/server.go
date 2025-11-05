package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/storage"
	"go.uber.org/zap"
)

// Default protocol ports
const (
	DefaultPeerPort      = 5567
	DefaultReflectorPort = 5566
	DefaultDHTPort       = 4444
)

// Protocol names
const (
	ProtocolPeer      = "peer"
	ProtocolReflector = "reflector"
	ProtocolDHT       = "dht"
)

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
	Port int
}

// DefaultServer implements the Server interface
type DefaultServer struct {
	storage       storage.BlobStore
	acquirer      liblbry.BlobAcquirer
	accessControl storage.AccessControl
	protocols     map[string]interface{}
	logger        *zap.Logger

	// Runtime state
	listeners map[string]net.Listener
	servers   map[string]interface{} // protocol servers
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
}

// Start starts the server and all configured protocols
func (s *DefaultServer) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.listeners = make(map[string]net.Listener)
	s.servers = make(map[string]interface{})

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
			if stopErr := s.Stop(ctx); stopErr != nil {
				s.logger.Error("Error stopping server after startup failure", zap.Error(stopErr))
			}
			return fmt.Errorf("failed to start %s protocol: %w", strings.Title(name), err)
		}
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
		)
	})
}

// startReflector starts the Reflector protocol handler
func (s *DefaultServer) startReflector(config *ReflectorConfig) error {
	return s.startTCPProtocol(ProtocolReflector, config.Port, func() protocol.ConnectionHandler {
		return protocol.NewReflectorServer(s.storage,
			protocol.WithReflectorAccessControl(s.accessControl),
			protocol.WithReflectorLogger(s.logger.Named("reflector")),
		)
	})
}

// startDHT starts the DHT protocol handler
func (s *DefaultServer) startDHT(config *DHTConfig) error {
	// Create DHT node with configuration
	dhtNode, err := protocol.NewDHTNodeWithDefaults(
		protocol.WithDHTAddress(fmt.Sprintf("0.0.0.0:%d", config.Port)),
		protocol.WithDHTLogger(s.logger.Named("dht")),
	)
	if err != nil {
		return fmt.Errorf("failed to create DHT node: %w", err)
	}

	// Start the DHT node
	if err := dhtNode.Start(); err != nil {
		return fmt.Errorf("failed to start DHT node: %w", err)
	}

	// Store the DHT node in servers map for later access
	s.servers[ProtocolDHT] = dhtNode

	s.logger.Info("DHT protocol started", zap.Int("port", config.Port))
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
			go server.HandleConnection(conn)
		}
	}()

	s.logger.Info("Protocol started", zap.String("protocol", protocolName), zap.Int("port", port))
	return nil
}
