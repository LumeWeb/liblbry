package protocol

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/liblbry/protocol/mocks"

	"go.uber.org/zap"
)

// TestDefaultPeerClient_Reset_NotConnected tests Reset when client is not connected
func TestDefaultPeerClient_Reset_NotConnected(t *testing.T) {
	client := NewPeerClient(WithClientLogger(zap.NewNop())).(*DefaultPeerClient)

	// Client should not be connected initially
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)

	// Reset should not error on unconnected client
	err := client.Reset()
	assert.NoError(t, err)

	// State should remain clean
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)
}

// TestDefaultPeerClient_Reset_Connected tests Reset when client is connected
func TestDefaultPeerClient_Reset_Connected(t *testing.T) {
	mockConn := mocks.NewMockConn(t)
	mockConn.On("Close").Return(nil)

	client := &DefaultPeerClient{
		conn:      mockConn,
		connected: true,
		logger:    zap.NewNop(),
	}

	// Verify initial state
	assert.True(t, client.connected)
	assert.NotNil(t, client.conn)

	// Reset should close connection and clear state
	err := client.Reset()
	assert.NoError(t, err)

	// State should be reset
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)

	// Verify Close was called
	mockConn.AssertExpectations(t)
}

// TestDefaultPeerClient_Reset_AfterFailedConnection tests Reset after failed connection attempts
func TestDefaultPeerClient_Reset_AfterFailedConnection(t *testing.T) {
	// Create a client with a dial function that always fails
	client := NewPeerClient(
		WithClientDialFunc(func(network, address string) (net.Conn, error) {
			return nil, net.ErrClosed
		}),
		WithClientLogger(zap.NewNop()),
	).(*DefaultPeerClient)

	// Attempt to connect (should fail)
	ctx := context.Background()
	err := client.Connect(ctx, "invalid-address:1234")
	assert.Error(t, err)

	// Reset should still work even after failed connection
	err = client.Reset()
	assert.NoError(t, err)

	// State should be clean
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)
}

// TestDefaultPeerClient_Reset_AfterSuccessfulOperations tests Reset after successful operations
func TestDefaultPeerClient_Reset_AfterSuccessfulOperations(t *testing.T) {
	// This test would require a more complex setup with a real server
	// For now, we'll simulate the state after successful operations
	mockConn := mocks.NewMockConn(t)
	mockConn.On("Close").Return(nil)

	client := &DefaultPeerClient{
		conn:      mockConn,
		reader:    nil, // Would be set after successful connection
		connected: true,
		logger:    zap.NewNop(),
	}

	// Simulate some operations that might modify internal state
	client.connected = true

	// Reset should clean up everything
	err := client.Reset()
	assert.NoError(t, err)

	// Verify state is completely reset
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)

	mockConn.AssertExpectations(t)
}

// TestDefaultPeerClient_Reset_CanReconnect tests that client can reconnect after Reset
func TestDefaultPeerClient_Reset_CanReconnect(t *testing.T) {
	client := NewPeerClient(WithClientLogger(zap.NewNop())).(*DefaultPeerClient)

	// Simulate a previous connection
	mockConn := mocks.NewMockConn(t)
	mockConn.On("Close").Return(nil)
	client.conn = mockConn
	client.connected = true

	// Reset the client
	err := client.Reset()
	assert.NoError(t, err)

	// Verify state is clean
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)

	// Client should be able to "connect" again (simulated)
	// In a real scenario, this would involve actual network operations
	assert.False(t, client.connected)

	mockConn.AssertExpectations(t)
}

// TestDefaultPeerClient_Reset_ThreadSafety tests thread safety of Reset method
func TestDefaultPeerClient_Reset_ThreadSafety(t *testing.T) {
	client := NewPeerClient(WithClientLogger(zap.NewNop())).(*DefaultPeerClient)

	// Simulate a connected state
	mockConn := mocks.NewMockConn(t)
	mockConn.On("Close").Return(nil).Maybe()
	client.conn = mockConn
	client.connected = true

	var wg sync.WaitGroup
	numGoroutines := 10
	errors := make(chan error, numGoroutines)

	// Run multiple goroutines calling Reset concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := client.Reset()
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		assert.NoError(t, err)
	}

	// Final state should be clean
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)
}

// TestDefaultPeerClient_Reset_CloseError tests Reset when Close returns an error
func TestDefaultPeerClient_Reset_CloseError(t *testing.T) {
	mockConn := mocks.NewMockConn(t)
	closeError := net.ErrClosed
	mockConn.On("Close").Return(closeError)

	client := &DefaultPeerClient{
		conn:      mockConn,
		connected: true,
		logger:    zap.NewNop(),
	}

	// Reset should return the close error
	err := client.Reset()
	assert.Error(t, err)
	assert.Equal(t, closeError, err)

	// State should still be reset even if Close errors
	assert.False(t, client.connected)
	assert.Nil(t, client.conn)
	assert.Nil(t, client.reader)

	mockConn.AssertExpectations(t)
}

// TestDefaultPeerClient_Reset_MultipleCalls tests multiple Reset calls
func TestDefaultPeerClient_Reset_MultipleCalls(t *testing.T) {
	client := NewPeerClient(WithClientLogger(zap.NewNop())).(*DefaultPeerClient)

	// Multiple Reset calls should not error
	for i := 0; i < 5; i++ {
		err := client.Reset()
		assert.NoError(t, err)
		assert.False(t, client.connected)
		assert.Nil(t, client.conn)
		assert.Nil(t, client.reader)
	}
}

// TestDefaultPeerClientFactory tests the PeerClientFactory function type
func TestDefaultPeerClientFactory(t *testing.T) {
	// Test factory with no options
	factory := DefaultPeerClientFactory()
	client1 := factory()
	assert.NotNil(t, client1)

	// Test factory with options
	factoryWithOptions := DefaultPeerClientFactory(
		WithClientLogger(zap.NewNop().Named("test")),
		WithClientTimeout(5*time.Second),
	)
	client2 := factoryWithOptions()
	assert.NotNil(t, client2)

	// Factory should create different instances
	assert.NotSame(t, client1, client2)
}

// TestPeerClientFactory_CreatesResettableClients tests that factory creates clients with Reset method
func TestPeerClientFactory_CreatesResettableClients(t *testing.T) {
	factory := DefaultPeerClientFactory()
	client := factory()

	// Client should have Reset method
	assert.Implements(t, (*PeerClient)(nil), client)

	// Reset should be callable without error
	err := client.Reset()
	assert.NoError(t, err)
}
