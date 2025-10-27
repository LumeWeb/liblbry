// Code adapted from https://github.com/LBRYFoundation/reflector.go - MIT License
//
// Adaptations made for liblbry:
//   - Implemented LBRY peer protocol client with composite request/response handling
//   - Added support for blob availability checking and data transfer
//   - Integrated with liblbry's stream and blob interfaces
//   - Enhanced with connection timeout handling and proper error management

package protocol

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// PeerClient defines the interface for peer protocol clients
type PeerClient interface {
	Connect(ctx context.Context, address string) error
	Close() error
	GetBlob(ctx context.Context, hash string) ([]byte, error)
	HasBlob(ctx context.Context, hash string) (bool, error)
	GetStream(ctx context.Context, sdHash string) (stream.Stream, error)
}

// Conn is an interface that embeds net.Conn for testability
type Conn interface {
	net.Conn
}

// DefaultPeerClient implements the PeerClient interface
type DefaultPeerClient struct {
	conn      Conn
	reader    *bufio.Reader
	timeout   time.Duration
	logger    *zap.Logger
	connected bool
	dialFunc  func(network, address string) (net.Conn, error)
	mutex     sync.RWMutex
}

// ClientOption configures the peer client
type ClientOption func(*DefaultPeerClient)

// NewPeerClient creates a new peer client instance with optional configuration
func NewPeerClient(options ...ClientOption) PeerClient {
	client := &DefaultPeerClient{
		timeout:   DefaultTimeout,
		logger:    zap.NewNop(), // Default to no-op logger
		connected: false,
		dialFunc: func(network, address string) (net.Conn, error) {
			return net.DialTimeout(network, address, DefaultTimeout)
		},
	}

	// Apply options
	for _, option := range options {
		option(client)
	}

	return client
}

// WithClientTimeout sets the connection timeout
func WithClientTimeout(timeout time.Duration) ClientOption {
	return func(c *DefaultPeerClient) {
		c.timeout = timeout
	}
}

// WithClientLogger sets the zap logger for the client
func WithClientLogger(logger *zap.Logger) ClientOption {
	return func(c *DefaultPeerClient) {
		c.logger = logger
	}
}

// WithClientDialFunc sets a custom dial function for the client
func WithClientDialFunc(dialFunc func(network, address string) (net.Conn, error)) ClientOption {
	return func(c *DefaultPeerClient) {
		c.dialFunc = dialFunc
	}
}

// Connect establishes a connection to the peer server
func (c *DefaultPeerClient) Connect(ctx context.Context, address string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.connected {
		return liblbryerrors.Err("already connected")
	}

	var conn net.Conn
	conn, err := c.dialFunc("tcp", address)
	if err != nil {
		return liblbryerrors.Err(err)
	}

	// Apply context to connection
	if err := c.applyContextDeadline(ctx); err != nil {
		conn.Close()
		return liblbryerrors.Err(err)
	}

	c.conn = conn
	c.reader = bufio.NewReader(conn)
	c.connected = true
	return nil
}

// Close terminates the connection to the peer server
func (c *DefaultPeerClient) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		return nil
	}

	err := c.conn.Close()
	c.connected = false
	return liblbryerrors.Err(err)
}

// GetBlob retrieves a blob from the peer server
func (c *DefaultPeerClient) GetBlob(ctx context.Context, hash string) ([]byte, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		return nil, liblbryerrors.Err("not connected")
	}

	// Validate blob hash length
	if len(hash) != blob.BlobHashHexLength {
		return nil, liblbryerrors.Err(ErrInvalidHashLen)
	}

	// Apply context deadline if exists
	if err := c.applyContextDeadline(ctx); err != nil {
		return nil, liblbryerrors.Err(err)
	}

	// Check for context cancellation before sending request
	if ctx.Err() != nil {
		return nil, liblbryerrors.Err(ctx.Err())
	}

	// Send blob request
	request := CompositeRequest{
		RequestedBlob: hash,
	}

	if err := c.sendRequest(ctx, request); err != nil {
		return nil, liblbryerrors.Err(err)
	}

	// Read response
	response, blobData, err := c.readResponse(ctx)
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}

	// Check for context cancellation after reading response header
	if ctx.Err() != nil {
		return nil, liblbryerrors.Err(ctx.Err())
	}

	// Check for error in response
	if response.IncomingBlob != nil && response.IncomingBlob.Error != "" {
		return nil, liblbryerrors.Err(response.IncomingBlob.Error)
	}

	if blobData == nil {
		return nil, liblbryerrors.Err("no blob data received")
	}

	return blobData, nil
}

// HasBlob checks if the peer server has a blob available
func (c *DefaultPeerClient) HasBlob(ctx context.Context, hash string) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		return false, liblbryerrors.Err("not connected")
	}

	// Validate blob hash length
	if len(hash) != blob.BlobHashHexLength {
		return false, liblbryerrors.Err(ErrInvalidHashLen)
	}

	// Apply context deadline if exists
	if err := c.applyContextDeadline(ctx); err != nil {
		return false, liblbryerrors.Err(err)
	}

	// Check for context cancellation before sending request
	if ctx.Err() != nil {
		return false, liblbryerrors.Err(ctx.Err())
	}

	// Send availability request
	request := CompositeRequest{
		RequestedBlobs: []string{hash},
	}

	if err := c.sendRequest(ctx, request); err != nil {
		return false, liblbryerrors.Err(err)
	}

	// Read response
	response, _, err := c.readResponse(ctx)
	if err != nil {
		return false, liblbryerrors.Err(err)
	}

	// Check for context cancellation after reading response
	if ctx.Err() != nil {
		return false, liblbryerrors.Err(ctx.Err())
	}

	// Check if blob is in available list
	for _, availableHash := range response.AvailableBlobs {
		if availableHash == hash {
			return true, nil
		}
	}

	return false, nil
}

// GetStream retrieves and reconstructs a stream from the peer server
func (c *DefaultPeerClient) GetStream(ctx context.Context, sdHash string) (stream.Stream, error) {
	// Validate SD blob hash length
	if len(sdHash) != blob.BlobHashHexLength {
		return nil, liblbryerrors.Err(ErrInvalidHashLen)
	}

	// Get SD blob first
	sdBlobData, err := c.GetBlob(ctx, sdHash)
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}

	// Parse SD blob
	var sdBlob stream.SDBlob
	if err := json.Unmarshal(sdBlobData, &sdBlob); err != nil {
		return nil, liblbryerrors.Err(err)
	}

	// Create stream with SD blob as first element (excluding last null blob)
	resultStream := make(stream.Stream, 1, len(sdBlob.BlobInfos)+1-1)
	resultStream[0] = sdBlobData

	// Get all content blobs (excluding last null blob)
	for _, blobInfo := range sdBlob.BlobInfos[:len(sdBlob.BlobInfos)-1] {
		// Check for context cancellation before fetching each blob
		if ctx.Err() != nil {
			return nil, liblbryerrors.Err(ctx.Err())
		}
		
		blobHash := hex.EncodeToString(blobInfo.BlobHash)

		blobData, err := c.GetBlob(ctx, blobHash)
		if err != nil {
			return nil, liblbryerrors.Err(err)
		}
		resultStream = append(resultStream, blobData)
	}

	return resultStream, nil
}

// sendJSON sends a JSON message to the server
func (c *DefaultPeerClient) sendJSON(v interface{}) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}

	data, err := json.Marshal(v)
	if err != nil {
		return err
	}

	_, err = c.conn.Write(data)
	return err
}

// readJSON reads a JSON message from the server
func (c *DefaultPeerClient) readJSON(v interface{}) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}

	decoder := json.NewDecoder(c.reader)
	return decoder.Decode(v)
}

// applyContextDeadline applies the context deadline to the connection if one exists
func (c *DefaultPeerClient) applyContextDeadline(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	return nil
}

// sendRequest sends a request to the peer server
func (c *DefaultPeerClient) sendRequest(ctx context.Context, request CompositeRequest) error {
	// Check for context cancellation before sending data
	if ctx.Err() != nil {
		return liblbryerrors.Err(ctx.Err())
	}

	// Set write deadline
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		return liblbryerrors.Err(err)
	}

	// Marshal request
	data, err := json.Marshal(request)
	if err != nil {
		return liblbryerrors.Err(err)
	}

	// Send request
	_, err = c.conn.Write(data)
	if err != nil {
		return liblbryerrors.Err(err)
	}

	return nil
}

// readResponse reads a response from the peer server
func (c *DefaultPeerClient) readResponse(ctx context.Context) (CompositeResponse, []byte, error) {
	// Check for context cancellation before reading JSON response
	if ctx.Err() != nil {
		return CompositeResponse{}, nil, liblbryerrors.Err(ctx.Err())
	}

	// Set read deadline
	if err := c.conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		return CompositeResponse{}, nil, liblbryerrors.Err(err)
	}

	// Read JSON response
	message, err := c.readNextMessage()
	if err != nil {
		return CompositeResponse{}, nil, liblbryerrors.Err(err)
	}

	// Log raw JSON for debugging
	if c.logger != nil {
		c.logger.Debug("Received raw JSON response",
			zap.String("json", string(message)),
			zap.ByteString("raw", message),
		)
	}

	// Parse response
	var response CompositeResponse
	if err := json.Unmarshal(message, &response); err != nil {
		c.logger.Error("Failed to unmarshal JSON response",
			zap.Error(err),
			zap.String("json", string(message)),
		)
		return CompositeResponse{}, nil, liblbryerrors.Err(err)
	}

	// If we have an incoming blob, read the blob data
	var blobData []byte
	if response.IncomingBlob != nil && response.IncomingBlob.Length > 0 {
		blobData = make([]byte, response.IncomingBlob.Length)
		_, err := io.ReadFull(c.reader, blobData)
		if err != nil {
			return CompositeResponse{}, nil, liblbryerrors.Err(err)
		}
	}

	return response, blobData, nil
}

// readNextMessage reads the next complete JSON message from the connection
func (c *DefaultPeerClient) readNextMessage() ([]byte, error) {
	// First byte must be '{' per protocol
	firstByte, err := c.reader.ReadByte()
	if err != nil {
		return nil, liblbryerrors.Err(err)
	}
	if firstByte != '{' {
		return nil, liblbryerrors.Err(ErrInvalidData)
	}

	// Create buffer with first byte
	buffer := []byte{firstByte}

	// Read until we have a complete valid JSON message
	for {
		// Check if we've exceeded max request size
		if len(buffer) > MaxRequestSize {
			return nil, liblbryerrors.Err(ErrRequestTooLarge)
		}

		// Read until we find a closing brace
		chunk, err := c.reader.ReadBytes('}')
		if err != nil {
			return nil, liblbryerrors.Err(err)
		}

		// Append chunk to buffer
		buffer = append(buffer, chunk...)

		// Check if we have valid JSON
		if json.Valid(buffer) {
			return buffer, nil
		}

		// If we don't have valid JSON yet, continue reading
	}
}
