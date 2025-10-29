package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.uber.org/zap"
)

// ReflectorClient defines the interface for interacting with LBRY reflector servers
type ReflectorClient interface {
	// Connect establishes a connection to the reflector server
	Connect(address string) error
	// Close terminates the connection to the reflector server
	Close() error
	// SendBlob uploads a regular blob to the reflector server
	SendBlob(hash string, data []byte) error
	// SendSDBlob uploads an SD blob to the reflector server
	SendSDBlob(hash string, data []byte) error
}

// DefaultReflectorClient implements the ReflectorClient interface
type DefaultReflectorClient struct {
	conn            net.Conn
	mutex           sync.Mutex
	logger          *zap.Logger
	timeout         time.Duration
	version         int
	dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)
}

// ReflectorClientOption configures the reflector client
type ReflectorClientOption func(*DefaultReflectorClient)

// NewReflectorClient creates a new reflector client instance
func NewReflectorClient(options ...ReflectorClientOption) ReflectorClient {
	client := &DefaultReflectorClient{
		logger:  zap.NewNop(),
		timeout: 30 * time.Second,
		version: 1, // Default to protocol version 1
	}

	for _, option := range options {
		option(client)
	}

	return client
}

// WithReflectorClientLogger sets the zap logger for the client
func WithReflectorClientLogger(logger *zap.Logger) ReflectorClientOption {
	return func(c *DefaultReflectorClient) {
		c.logger = logger
	}
}

// WithReflectorClientTimeout sets the connection timeout
func WithReflectorClientTimeout(timeout time.Duration) ReflectorClientOption {
	return func(c *DefaultReflectorClient) {
		c.timeout = timeout
	}
}

// WithReflectorClientVersion sets the protocol version
func WithReflectorClientVersion(version int) ReflectorClientOption {
	return func(c *DefaultReflectorClient) {
		c.version = version
	}
}

// WithReflectorClientDialContext sets a custom dial function for the client
func WithReflectorClientDialContext(dialFunc func(ctx context.Context, network, address string) (net.Conn, error)) ReflectorClientOption {
	return func(c *DefaultReflectorClient) {
		c.dialContextFunc = dialFunc
	}
}

// Connect establishes a connection to the reflector server
func (c *DefaultReflectorClient) Connect(address string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn != nil {
		return errors.New("connection already established")
	}

	var conn net.Conn
	var err error

	if c.dialContextFunc != nil {
		ctx, cancel := context.WithTimeout(context.Background(), c.timeout)
		defer cancel()
		conn, err = c.dialContextFunc(ctx, "tcp", address)
	} else {
		conn, err = net.DialTimeout("tcp", address, c.timeout)
	}

	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	c.conn = conn

	// Perform handshake
	if err := c.doHandshake(); err != nil {
		c.conn.Close()
		c.conn = nil
		return fmt.Errorf("handshake failed: %w", err)
	}

	return nil
}

// Close terminates the connection
func (c *DefaultReflectorClient) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	return err
}

// SendBlob uploads a regular blob to the reflector server
func (c *DefaultReflectorClient) SendBlob(hash string, data []byte) error {
	return c.sendBlob(hash, data, false)
}

// SendSDBlob uploads an SD blob to the reflector server
func (c *DefaultReflectorClient) SendSDBlob(hash string, data []byte) error {
	return c.sendBlob(hash, data, true)
}

// sendBlob handles the common blob upload logic
func (c *DefaultReflectorClient) sendBlob(hash string, data []byte, isSdBlob bool) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.conn == nil {
		return errors.New("not connected")
	}

	if len(data) > blob.MaxBlobSize {
		return fmt.Errorf("blob exceeds maximum size of %d bytes", blob.MaxBlobSize)
	}

	// Send blob request
	if err := c.sendBlobRequest(hash, len(data), isSdBlob); err != nil {
		return fmt.Errorf("failed to send blob request: %w", err)
	}

	// Read blob response
	shouldSend, err := c.readBlobResponse(isSdBlob)
	if err != nil {
		return fmt.Errorf("failed to read blob response: %w", err)
	}
	if !shouldSend {
		return liblbryerrors.ErrBlobExists // Server doesn't want this blob
	}

	// Send blob data
	if err := c.sendBlobData(data); err != nil {
		return fmt.Errorf("failed to send blob data: %w", err)
	}

	// Read transfer response
	received, err := c.readTransferResponse(isSdBlob)
	if err != nil {
		return fmt.Errorf("failed to read transfer response: %w", err)
	}
	if !received {
		return errors.New("server failed to receive blob")
	}

	return nil
}

// doHandshake performs the protocol handshake
func (c *DefaultReflectorClient) doHandshake() error {
	// Send handshake
	handshake := HandshakeRequestResponse{Version: &c.version}
	if err := c.writeJSON(handshake); err != nil {
		return err
	}

	// Read response
	var response HandshakeRequestResponse
	if err := c.readJSON(&response); err != nil {
		return err
	}

	if response.Version == nil {
		return errors.New("invalid handshake response: missing version")
	}

	if *response.Version != c.version {
		return fmt.Errorf("protocol version mismatch: server=%d client=%d", *response.Version, c.version)
	}

	return nil
}

// sendBlobRequest sends a blob upload request
func (c *DefaultReflectorClient) sendBlobRequest(hash string, size int, isSdBlob bool) error {
	var request interface{}
	if isSdBlob {
		request = SendBlobRequest{SdBlobHash: hash, SdBlobSize: size}
	} else {
		request = SendBlobRequest{BlobHash: hash, BlobSize: size}
	}
	return c.writeJSON(request)
}

// readBlobResponse reads the server's response to a blob request
func (c *DefaultReflectorClient) readBlobResponse(isSdBlob bool) (bool, error) {
	if isSdBlob {
		var response SendSDBlobResponse
		if err := c.readJSON(&response); err != nil {
			// EOF errors from readJSON during blob response indicate validation errors
			// (the reference server closes connection on validation errors)
			if errors.Is(err, io.EOF) {
				return false, liblbryerrors.ErrServerValidation
			}
			return false, err
		}
		return response.SendSdBlob, nil
	}

	var response SendBlobResponse
	if err := c.readJSON(&response); err != nil {
		// EOF errors from readJSON during blob response indicate validation errors
		// (the reference server closes connection on validation errors)
		if errors.Is(err, io.EOF) {
			return false, liblbryerrors.ErrServerValidation
		}
		return false, err
	}
	return response.SendBlob, nil
}

// sendBlobData sends the raw blob data
func (c *DefaultReflectorClient) sendBlobData(data []byte) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}

	_, err := c.conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}

// readTransferResponse reads the server's confirmation after blob transfer
func (c *DefaultReflectorClient) readTransferResponse(isSdBlob bool) (bool, error) {
	if isSdBlob {
		var response SDBlobTransferResponse
		if err := c.readJSON(&response); err != nil {
			return false, err
		}
		return response.ReceivedSdBlob, nil
	}

	var response BlobTransferResponse
	if err := c.readJSON(&response); err != nil {
		return false, err
	}
	return response.ReceivedBlob, nil
}

// writeJSON writes a JSON message to the connection
func (c *DefaultReflectorClient) writeJSON(v interface{}) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}

	// First encode the interface{} to JSON bytes
	jsonData, err := json.Marshal(v)
	if err != nil {
		return err
	}

	// Then write those bytes directly to the connection
	_, err = c.conn.Write(jsonData)
	return err
}

// readJSON reads a JSON message from the connection
func (c *DefaultReflectorClient) readJSON(v interface{}) error {
	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}

	dec := json.NewDecoder(c.conn)
	return dec.Decode(v)
}
// containsIgnoreCase checks if a string contains a substring ignoring case
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
