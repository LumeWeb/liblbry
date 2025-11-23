package connection

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	"go.lumeweb.com/liblbry/protocol"
	"go.uber.org/zap"
)

// ConnectionManager defines the interface for connection management functionality
type ConnectionManager interface {
	IsStopped() bool
	Stop()
	Start()
	GetClient() (protocol.PeerClient, error)
	ReturnClient(peerClient protocol.PeerClient)
	DownloadFromPeer(ctx context.Context, peerAddr, hash string) ([]byte, error)
	SetLogger(logger *zap.Logger)
}

// DefaultConnectionManager manages peer client pooling and lifecycle
type DefaultConnectionManager struct {
	clientFactory protocol.PeerClientFactory
	clientPool    *sync.Pool
	clientPoolMu  sync.RWMutex
	logger        *zap.Logger
	stopped       int32
	pooledClients map[protocol.PeerClient]struct{}
}

// NewConnectionManager creates a new DefaultConnectionManager instance
func NewConnectionManager(clientFactory protocol.PeerClientFactory, logger *zap.Logger) (*DefaultConnectionManager, error) {
	if clientFactory == nil {
		return nil, fmt.Errorf("clientFactory cannot be nil")
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	cm := &DefaultConnectionManager{
		clientFactory: clientFactory,
		logger:        logger,
	}

	// Initialize the client pool
	cm.createClientPool()

	return cm, nil
}

// IsStopped checks if the connection manager is stopped
func (cm *DefaultConnectionManager) IsStopped() bool {
	return atomic.LoadInt32(&cm.stopped) == 1
}

// Stop stops the connection manager and clears the client pool
func (cm *DefaultConnectionManager) Stop() {
	atomic.StoreInt32(&cm.stopped, 1)

	cm.clientPoolMu.Lock()
	defer cm.clientPoolMu.Unlock()

	// Close all tracked pooled clients to clean up resources
	for client := range cm.pooledClients {
		if err := client.Close(); err != nil && cm.logger != nil {
			cm.logger.Debug("Error closing pooled client during shutdown", zap.Error(err))
		}
		delete(cm.pooledClients, client)
	}

	// Clear the client pool reference to allow garbage collection
	// Note: We don't drain the pool because sync.Pool.Get() with a New function
	// will never return nil, which would cause an infinite loop
	cm.clientPool = nil
}

// Start starts the connection manager and recreates the client pool
func (cm *DefaultConnectionManager) Start() {
	atomic.StoreInt32(&cm.stopped, 0)
	cm.createClientPool()
}

// SetLogger updates the logger for this connection manager instance
func (cm *DefaultConnectionManager) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	cm.logger = logger
}

// GetClient safely gets and validates a peer client from the pool
func (cm *DefaultConnectionManager) GetClient() (protocol.PeerClient, error) {
	if cm.IsStopped() {
		return nil, fmt.Errorf("connection manager is stopped")
	}

	cm.clientPoolMu.Lock()
	defer cm.clientPoolMu.Unlock()

	// Check if client pool is available
	if cm.clientPool == nil {
		return nil, fmt.Errorf("client pool is not available")
	}

	// Get a peer client from the pool
	client := cm.clientPool.Get()
	if client == nil {
		return nil, fmt.Errorf("failed to get client from pool: client is nil")
	}

	// Type assert to protocol.PeerClient
	peerClient, ok := client.(protocol.PeerClient)
	if !ok {
		return nil, fmt.Errorf("failed to assert client as PeerClient: got %T", client)
	}

	// Remove client from tracking since it's now in use
	delete(cm.pooledClients, peerClient)

	return peerClient, nil
}

// ReturnClient safely returns a client to the pool if both pool and client are valid
func (cm *DefaultConnectionManager) ReturnClient(peerClient protocol.PeerClient) {
	if peerClient == nil {
		return
	}

	cm.returnClientToPool(peerClient)
}

// DownloadFromPeer attempts to download a blob from a specific peer
// This method handles the full lifecycle: get client, connect, download, return client
func (cm *DefaultConnectionManager) DownloadFromPeer(ctx context.Context, peerAddr, hash string) ([]byte, error) {
	if cm.IsStopped() {
		return nil, fmt.Errorf("connection manager is stopped")
	}

	// Get and validate peer client
	peerClient, err := cm.GetClient()
	if err != nil {
		return nil, err
	}

	// Attempt to connect and download
	if err = peerClient.Connect(ctx, peerAddr); err != nil {
		// Return client to pool even on error
		cm.ReturnClient(peerClient)
		// Preserve context errors without wrapping
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, fmt.Errorf("connect to peer %s: %w", peerAddr, err)
	}

	// Defer return client to pool when done
	defer cm.ReturnClient(peerClient)

	data, err := peerClient.GetBlob(ctx, hash)
	if err != nil {
		// Preserve context errors without wrapping
		if ctx.Err() != nil {
			return nil, err
		}
		return nil, fmt.Errorf("fetch blob from peer %s: %w", peerAddr, err)
	}

	return data, nil
}

// createClientPool creates a new sync.Pool using the stored factory
func (cm *DefaultConnectionManager) createClientPool() {
	cm.clientPoolMu.Lock()
	defer cm.clientPoolMu.Unlock()

	if cm.clientPool == nil {
		// Defensive check - this should never happen due to constructor validation
		if cm.clientFactory == nil {
			if cm.logger != nil {
				cm.logger.Error("clientFactory is nil - client pool not created")
			}
			return
		}
		cm.pooledClients = make(map[protocol.PeerClient]struct{})
		cm.clientPool = &sync.Pool{
			New: func() interface{} {
				return cm.clientFactory()
			},
		}
	}
}

// returnClientToPool resets the client and returns it to the pool
func (cm *DefaultConnectionManager) returnClientToPool(peerClient protocol.PeerClient) {
	// Decide under lock whether to pool or close
	cm.clientPoolMu.Lock()
	defer cm.clientPoolMu.Unlock()

	if cm.IsStopped() || cm.clientPool == nil {
		// Manager is stopped or pool unavailable: close instead of pooling
		if cm.logger != nil {
			cm.logger.Debug("Closing client because connection manager is stopped or pool is unavailable")
		}
		if err := peerClient.Close(); err != nil && cm.logger != nil {
			cm.logger.Debug("Error closing client on stop", zap.Error(err))
		}
		return
	}

	// If the client cannot be reset, close and discard it
	if resetErr := peerClient.Reset(); resetErr != nil {
		if cm.logger != nil {
			cm.logger.Debug("Discarding client due to reset failure", zap.Error(resetErr))
		}
		if err := peerClient.Close(); err != nil && cm.logger != nil {
			cm.logger.Debug("Error closing client after reset failure", zap.Error(err))
		}
		return
	}

	cm.clientPool.Put(peerClient)
	// Track the client for potential shutdown
	cm.pooledClients[peerClient] = struct{}{}
}
