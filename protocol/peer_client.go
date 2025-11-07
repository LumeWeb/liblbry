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
	conn            Conn
	reader          *bufio.Reader
	timeout         time.Duration
	logger          *zap.Logger
	connected       bool
	dialFunc        func(network, address string) (net.Conn, error)
	dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)
	mutex           sync.RWMutex
}

// ClientOption configures the peer client
type ClientOption func(*DefaultPeerClient)

// NewPeerClient creates a new peer client instance with optional configuration
func NewPeerClient(options ...ClientOption) PeerClient {
	client := &DefaultPeerClient{
		timeout:   DefaultTimeout,
		logger:    nil, // Will be set to a named logger
		connected: false,
	}

	// Apply options
	for _, option := range options {
		option(client)
	}

	// Create a named logger for the client if none was provided
	if client.logger == nil {
		client.logger = zap.NewNop().Named("peer-client")
	} else {
		client.logger = client.logger.Named("peer-client")
	}

	// Set default dial functions only if none were provided
	if client.dialFunc == nil {
		client.dialFunc = func(network, address string) (net.Conn, error) {
			return net.DialTimeout(network, address, client.timeout)
		}
	}

	if client.dialContextFunc == nil {
		client.dialContextFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := net.DialTimeout(network, address, client.timeout)
			if err != nil {
				return nil, err
			}

			// If context has a deadline, apply it to the connection
			if deadline, ok := ctx.Deadline(); ok {
				_ = conn.SetDeadline(deadline)
			}

			return conn, nil
		}
	}

	return client
}

// WithClientTimeout sets the connection timeout
func WithClientTimeout(timeout time.Duration) ClientOption {
	return func(c *DefaultPeerClient) {
		c.timeout = timeout
	}
}

// WithClientLogger sets the zap logger specifically for the peer client
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

// WithClientDialContext sets a custom context-aware dial function for the client
func WithClientDialContext(dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)) ClientOption {
	return func(c *DefaultPeerClient) {
		c.dialContextFunc = dialContextFunc
	}
}

// Connect establishes a connection to the peer server
func (c *DefaultPeerClient) Connect(ctx context.Context, address string) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if c.connected {
		c.logger.Debug("client already connected", zap.String("address", address))
		return liblbryerrors.Err(liblbryerrors.ErrAlreadyConnected)
	}

	c.logger.Debug("connecting to peer", zap.String("address", address))

	var conn net.Conn
	var err error

	// Use context-aware dial function if available, otherwise fall back to regular dial function
	if c.dialContextFunc != nil {
		conn, err = c.dialContextFunc(ctx, "tcp", address)
	} else {
		conn, err = c.dialFunc("tcp", address)
	}

	if err != nil {
		c.logger.Debug("failed to connect to peer", zap.String("address", address), zap.Error(err))
		return liblbryerrors.Err(err)
	}

	// Assign connection and reader before applying context deadline
	c.conn = conn
	c.reader = bufio.NewReader(conn)

	// Apply context to connection
	if err := c.applyContextDeadline(ctx); err != nil {
		c.logger.Debug("failed to apply context deadline, closing connection", zap.String("address", address), zap.Error(err))
		// If applying context deadline fails, close connection and clear state
		if closeErr := c.conn.Close(); closeErr != nil {
			c.logger.Error("failed to close connection", zap.Error(closeErr))
		}
		c.conn = nil
		c.reader = nil
		return liblbryerrors.Err(err)
	}

	c.connected = true
	c.logger.Debug("successfully connected to peer", zap.String("address", address))
	return nil
}

// Close terminates the connection to the peer server
func (c *DefaultPeerClient) Close() error {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		c.logger.Debug("client not connected, nothing to close")
		return nil
	}

	c.logger.Debug("closing peer connection")
	err := c.conn.Close()
	c.connected = false
	c.conn = nil
	c.reader = nil

	if err != nil {
		c.logger.Debug("error closing peer connection", zap.Error(err))
		return liblbryerrors.Err(err)
	}

	c.logger.Debug("peer connection closed successfully")
	return nil
}

// GetBlob retrieves a blob from the peer server
func (c *DefaultPeerClient) GetBlob(ctx context.Context, hash string) ([]byte, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		c.logger.Debug("client not connected", zap.String("hash", hash))
		return nil, liblbryerrors.Err(liblbryerrors.ErrConnectionFailed)
	}

	// Validate blob hash length
	if len(hash) != blob.BlobHashHexLength {
		c.logger.Debug("invalid blob hash length", zap.String("hash", hash), zap.Int("length", len(hash)))
		return nil, liblbryerrors.Err(liblbryerrors.ErrInvalidHashLen)
	}

	// Apply context deadline if exists
	if err := c.applyContextDeadline(ctx); err != nil {
		c.logger.Debug("failed to apply context deadline", zap.String("hash", hash), zap.Error(err))
		return nil, c.wrapContextError(err)
	}

	// Check for context cancellation before sending request
	if ctx.Err() != nil {
		c.logger.Debug("context cancelled before sending request", zap.String("hash", hash), zap.Error(ctx.Err()))
		return nil, ctx.Err()
	}

	// Send blob request
	request := CompositeRequest{
		RequestedBlob: hash,
	}

	c.logger.Debug("sending blob request", zap.String("hash", hash))
	if err := c.sendRequest(ctx, request); err != nil {
		c.logger.Debug("failed to send blob request", zap.String("hash", hash), zap.Error(err))
		return nil, c.wrapAndReturnContextError(ctx, err)
	}

	// Read response
	c.logger.Debug("waiting for blob response", zap.String("hash", hash))
	response, blobData, err := c.readResponse(ctx)
	if err != nil {
		c.logger.Debug("failed to read blob response", zap.String("hash", hash), zap.Error(err))
		return nil, c.wrapAndReturnContextError(ctx, err)
	}

	// Check for context cancellation after reading response header
	if ctx.Err() != nil {
		c.logger.Debug("context cancelled after reading response", zap.String("hash", hash), zap.Error(ctx.Err()))
		return nil, ctx.Err()
	}

	// Check for error in response
	if response.IncomingBlob != nil && response.IncomingBlob.Error != "" {
		c.logger.Debug("server returned error for blob request", zap.String("hash", hash), zap.String("error", response.IncomingBlob.Error))
		err = liblbryerrors.DetectErrorType(response.IncomingBlob.Error)
		// Don't wrap known error types to preserve error identity for testing
		if liblbryerrors.IsKnownErrorType(err) {
			return nil, err
		}
		// Check specifically for blob not found errors using our helper
		if liblbryerrors.IsBlobNotFoundError(err) {
			return nil, liblbryerrors.ErrBlobNotFound
		}
		return nil, c.wrapAndReturnContextError(ctx, err)
	}

	if blobData == nil {
		c.logger.Debug("no blob data in response", zap.String("hash", hash))
		return nil, liblbryerrors.ErrNoBlobData
	}

	c.logger.Debug("successfully retrieved blob", zap.String("hash", hash), zap.Int("size", len(blobData)))
	return blobData, nil
}

// HasBlob checks if the peer server has a blob available
func (c *DefaultPeerClient) HasBlob(ctx context.Context, hash string) (bool, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()

	if !c.connected {
		c.logger.Debug("client not connected", zap.String("hash", hash))
		return false, liblbryerrors.Err(liblbryerrors.ErrConnectionFailed)
	}

	// Validate blob hash length
	if len(hash) != blob.BlobHashHexLength {
		c.logger.Debug("invalid blob hash length", zap.String("hash", hash), zap.Int("length", len(hash)))
		return false, liblbryerrors.Err(liblbryerrors.ErrInvalidHashLen)
	}

	// Apply context deadline if exists
	if err := c.applyContextDeadline(ctx); err != nil {
		c.logger.Debug("failed to apply context deadline", zap.String("hash", hash), zap.Error(err))
		return false, c.wrapContextError(err)
	}

	// Check for context cancellation before sending request
	if ctx.Err() != nil {
		c.logger.Debug("context cancelled before sending request", zap.String("hash", hash), zap.Error(ctx.Err()))
		return false, ctx.Err()
	}

	// Send availability request
	request := CompositeRequest{
		RequestedBlobs: []string{hash},
	}

	c.logger.Debug("checking blob availability", zap.String("hash", hash))
	if err := c.sendRequest(ctx, request); err != nil {
		c.logger.Debug("failed to send availability request", zap.String("hash", hash), zap.Error(err))
		return false, c.wrapAndReturnContextError(ctx, err)
	}

	// Read response
	c.logger.Debug("waiting for availability response", zap.String("hash", hash))
	response, _, err := c.readResponse(ctx)
	if err != nil {
		c.logger.Debug("failed to read availability response", zap.String("hash", hash), zap.Error(err))
		return false, c.wrapAndReturnContextError(ctx, err)
	}

	// Check for context cancellation after reading response
	if ctx.Err() != nil {
		c.logger.Debug("context cancelled after reading response", zap.String("hash", hash), zap.Error(ctx.Err()))
		return false, ctx.Err()
	}

	// Check if blob is in available list
	for _, availableHash := range response.AvailableBlobs {
		if availableHash == hash {
			c.logger.Debug("blob is available", zap.String("hash", hash))
			return true, nil
		}
	}

	c.logger.Debug("blob is not available", zap.String("hash", hash))
	return false, nil
}

// GetStream retrieves and reconstructs a stream from the peer server
func (c *DefaultPeerClient) GetStream(ctx context.Context, sdHash string) (stream.Stream, error) {
	c.logger.Debug("retrieving stream", zap.String("sd_hash", sdHash))

	// Validate SD blob hash length
	if len(sdHash) != blob.BlobHashHexLength {
		c.logger.Debug("invalid SD blob hash length", zap.String("sd_hash", sdHash), zap.Int("length", len(sdHash)))
		return nil, liblbryerrors.Err(liblbryerrors.ErrInvalidHashLen)
	}

	// Get SD blob first
	c.logger.Debug("fetching SD blob", zap.String("sd_hash", sdHash))
	sdBlobData, err := c.GetBlob(ctx, sdHash)
	if err != nil {
		c.logger.Debug("failed to fetch SD blob", zap.String("sd_hash", sdHash), zap.Error(err))
		return nil, c.wrapAndReturnContextError(ctx, err)
	}

	// Parse SD blob
	var sdBlob stream.SDBlob
	if err := json.Unmarshal(sdBlobData, &sdBlob); err != nil {
		c.logger.Debug("failed to parse SD blob", zap.String("sd_hash", sdHash), zap.Error(err))
		return nil, liblbryerrors.Err(err)
	}

	c.logger.Debug("SD blob parsed", zap.String("sd_hash", sdHash), zap.Int("blob_count", len(sdBlob.BlobInfos)))

	// Create stream with SD blob as first element
	resultStream := make(stream.Stream, 1, 1+len(sdBlob.BlobInfos))
	resultStream[0] = sdBlobData

	// Get all content blobs (excluding last null blob) only if we have content blobs
	end := len(sdBlob.BlobInfos) - 1
	if end > 0 {
		c.logger.Debug("fetching content blobs", zap.String("sd_hash", sdHash), zap.Int("count", end))
		// Get all content blobs (excluding last null blob)
		for i, blobInfo := range sdBlob.BlobInfos[:end] {
			// Check for context cancellation before fetching each blob
			if ctx.Err() != nil {
				c.logger.Debug("context cancelled while fetching content blobs", zap.String("sd_hash", sdHash), zap.Int("index", i), zap.Error(ctx.Err()))
				return nil, ctx.Err()
			}

			blobHash := hex.EncodeToString(blobInfo.BlobHash)
			c.logger.Debug("fetching content blob", zap.String("sd_hash", sdHash), zap.String("blob_hash", blobHash), zap.Int("index", i))

			blobData, err := c.GetBlob(ctx, blobHash)
			if err != nil {
				c.logger.Debug("failed to fetch content blob", zap.String("sd_hash", sdHash), zap.String("blob_hash", blobHash), zap.Int("index", i), zap.Error(err))
				return nil, c.wrapAndReturnContextError(ctx, err)
			}
			resultStream = append(resultStream, blobData)
		}
	}

	c.logger.Debug("stream retrieved successfully", zap.String("sd_hash", sdHash), zap.Int("total_blobs", len(resultStream)))
	return resultStream, nil
}

// wrapContextError wraps an error with liblbryerrors.Err only if it's not a context error
func (c *DefaultPeerClient) wrapContextError(err error) error {
	if liblbryerrors.Is(err, context.DeadlineExceeded) || liblbryerrors.Is(err, context.Canceled) {
		return err
	}
	return liblbryerrors.Err(err)
}

// wrapAndReturnContextError handles wrapping errors while preserving context errors
func (c *DefaultPeerClient) wrapAndReturnContextError(ctx context.Context, err error) error {
	// Check for context cancellation first
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Use wrapContextError for the actual error wrapping logic
	return c.wrapContextError(err)
}

// sendJSON sends a JSON message to the server
// This method should only be called when the client is already locked
func (c *DefaultPeerClient) sendJSON(v any) error {
	// Create a background context for deadline setting in this helper
	ctx := context.Background()
	if err := c.setMinDeadline(ctx); err != nil {
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
// This method should only be called when the client is already locked
func (c *DefaultPeerClient) readJSON(v any) error {
	// Create a background context for deadline setting in this helper
	ctx := context.Background()
	if err := c.setMinDeadline(ctx); err != nil {
		return err
	}

	decoder := json.NewDecoder(c.reader)
	return decoder.Decode(v)
}

// minDeadline returns the earlier of the context deadline (if present) and time.Now().Add(timeout)
func (c *DefaultPeerClient) minDeadline(ctx context.Context, timeout time.Duration) time.Time {
	sooner := time.Now().Add(timeout)
	if deadline, ok := ctx.Deadline(); ok && deadline.Before(sooner) {
		return deadline
	}
	return sooner
}

// setMinDeadline sets the connection deadline to the earlier of context deadline and default timeout
func (c *DefaultPeerClient) setMinDeadline(ctx context.Context) error {
	deadline := c.minDeadline(ctx, c.timeout)
	return c.conn.SetDeadline(deadline)
}

// applyContextDeadline applies the context deadline to the connection if one exists
func (c *DefaultPeerClient) applyContextDeadline(ctx context.Context) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := c.conn.SetDeadline(deadline); err != nil {
			return err
		}
	} else if c.timeout > 0 {
		if err := c.conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
			return err
		}
	}
	return nil
}

// sendRequest sends a request to the peer server
func (c *DefaultPeerClient) sendRequest(ctx context.Context, request CompositeRequest) error {
	// Check for context cancellation before sending data
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Set write deadline to the minimum of context deadline and timeout
	deadline := c.minDeadline(ctx, c.timeout)
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return c.wrapContextError(err)
	}

	// Marshal request
	data, err := json.Marshal(request)
	if err != nil {
		return liblbryerrors.Err(err)
	}

	// Send request
	_, err = c.conn.Write(data)
	if err != nil {
		return c.wrapContextError(err)
	}

	return nil
}

// readResponse reads a response from the peer server
func (c *DefaultPeerClient) readResponse(ctx context.Context) (CompositeResponse, []byte, error) {
	// Set read deadline to the minimum of context deadline and timeout
	deadline := c.minDeadline(ctx, c.timeout)

	if err := c.conn.SetReadDeadline(deadline); err != nil {
		return CompositeResponse{}, nil, c.wrapContextError(err)
	}

	// Read JSON response
	message, err := c.readNextMessage()
	if err != nil {
		return CompositeResponse{}, nil, c.wrapContextError(err)
	}

	// Parse response
	var response CompositeResponse

	if err := json.Unmarshal(message, &response); err != nil {
		return CompositeResponse{}, nil, c.wrapContextError(err)
	}

	// If we have an incoming blob, read the blob data
	var blobData []byte
	if response.IncomingBlob != nil && response.IncomingBlob.Length > 0 {
		// Set read deadline for blob data as well
		deadline := c.minDeadline(ctx, c.timeout)
		if err := c.conn.SetReadDeadline(deadline); err != nil {
			return CompositeResponse{}, nil, c.wrapContextError(err)
		}

		blobData = make([]byte, response.IncomingBlob.Length)
		_, err := io.ReadFull(c.reader, blobData)
		if err != nil {
			return CompositeResponse{}, nil, c.wrapContextError(err)
		}
	}

	return response, blobData, nil
}

// readNextMessage reads the next complete JSON message from the connection.
//
// Manual JSON parsing is required due to the LBRY peer protocol's byte-stream nature.
// The protocol sends messages as raw byte streams without framing, where a single
// transmission may contain a JSON response followed immediately by binary blob data.
//
// Using json.Decoder would break the protocol because:
// 1. Decoder.Buffer() is not exposed, preventing access to buffered but unconsumed data
// 2. Decoder.Read() consumes more bytes than just the JSON message during validation
// 3. When blob data follows JSON, Decoder would incorrectly consume part of the binary data
// 4. There's no way to "unread" or recover the over-consumed bytes for proper blob handling
//
// Buffer state management requirements:
// - Must preserve exact byte boundaries between JSON and binary data
// - Cannot rely on streaming readers that consume indeterminate amounts of data
// - Must validate JSON incrementally without consuming extra bytes
// - Must handle partial reads and network fragmentation properly
//
// Race condition prevention:
// Manual parsing prevents a race between JSON validation and data consumption.
// json.Decoder might read beyond the JSON message boundary to validate syntax,
// consuming bytes that belong to subsequent binary blob data. This would corrupt
// the blob transfer by missing initial bytes or reading incorrect lengths.
//
// Protocol format reference:
// The LBRY peer protocol uses mixed JSON/binary transmissions where:
// {JSON response}{optional binary blob data}
// Both parts are sent in a single Write() operation from the server side.
func (c *DefaultPeerClient) readNextMessage() ([]byte, error) {
	// Read first byte and check if it's '{'
	firstByte, err := c.reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if firstByte != '{' {
		return nil, liblbryerrors.ErrInvalidData
	}

	// Create buffer with first byte
	buffer := []byte{firstByte}

	// Read until we have a complete valid JSON message
	for {
		// Check if we've exceeded max request size
		if len(buffer) > MaxRequestSize {
			return nil, liblbryerrors.ErrRequestTooLarge
		}

		// Read until we find a closing brace
		chunk, err := c.reader.ReadBytes('}')
		if err != nil {
			return nil, err
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
