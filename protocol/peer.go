// Code adapted from https://github.com/LBRYFoundation/reflector.go - MIT License
//
// Adaptations made for liblbry:
//   - Implemented LBRY peer protocol server with composite request/response handling
//   - Added support for blob availability checking, data transfer, and payment rate negotiation
//   - Integrated with liblbry's blob store and access control interfaces
//   - Enhanced with connection timeout handling and proper error management
//   - Added support for content protection and access control mechanisms
//   - Maintained protocol compatibility with LBRY reflector specification

package protocol

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// PeerServer defines the interface for handling peer connections
type PeerServer interface {
	ConnectionHandler
}

// wrapPeerContext creates a new context with peer source and IP address information
func wrapPeerContext(ctx context.Context, conn net.Conn) context.Context {
	ip := GetConnectionIP(conn)
	ctx = context.WithValue(ctx, SourceContextKey, SourcePeer)
	ctx = context.WithValue(ctx, IPAddressContextKey, ip)
	return ctx
}

// getPeerIP extracts the peer IP address from a connection
func getPeerIP(conn net.Conn) string {
	addr := conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If we can't split the address, try to parse it as an IP directly
		if ip, err := netip.ParseAddr(addr); err == nil {
			return ip.String()
		}
		return ""
	}

	// Validate that the host is a proper IP address
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String()
	}

	return ""
}

// Protocol constants
const (
	DefaultTimeout = 1 * time.Minute
	MaxRequestSize = 64 * 1024 // 64KB max request size
	// Response constants
	PaymentRateAccepted = "RATE_ACCEPTED"
	PaymentRateTooLow   = "RATE_TOO_LOW"
)

// Protocol errors
var (
	ErrBlobNotFound   = liblbryerrors.ErrBlobNotFound
	ErrInvalidHashLen = liblbryerrors.ErrInvalidHashLen
	ErrBlobProtected  = liblbryerrors.ErrBlobProtected
	ErrAccessDenied   = liblbryerrors.ErrAccessDenied
)

// DefaultPeerServer implements the PeerServer interface
type DefaultPeerServer struct {
	store             storage.BlobStore
	protector         Protector
	accessControl     storage.AccessControl
	notifier          Notifier
	connectionTimeout time.Duration
	logger            *zap.Logger
}

// ServerOption configures the peer server
type ServerOption func(*DefaultPeerServer)

// applyPeerOptions applies all ServerOption functions to the server instance
// This helper function centralizes the option application logic,
// making it easier to maintain and extend. It follows the functional
// options pattern which provides a clean and flexible way to configure
// a server instance during construction.
func applyPeerOptions(server *DefaultPeerServer, options []ServerOption) {
	for _, option := range options {
		option(server)
	}
}

// NewPeerServer creates a new peer server instance with optional configuration
func NewPeerServer(store storage.BlobStore, options ...ServerOption) PeerServer {
	server := &DefaultPeerServer{
		store:             store,
		protector:         nil, // Default to no protection
		accessControl:     nil, // Default to no access control
		connectionTimeout: DefaultTimeout,
		logger:            nil, // Will be set to a named logger
	}

	// Apply options using helper function
	applyPeerOptions(server, options)

	// Create a named logger for the server if none was provided
	if server.logger == nil {
		server.logger = zap.NewNop().Named("peer-server")
	} else {
		server.logger = server.logger.Named("peer-server")
	}

	return server
}

// WithPeerProtector sets the content protector for the peer server
func WithPeerProtector(protector Protector) ServerOption {
	return func(s *DefaultPeerServer) {
		s.protector = protector
	}
}

// WithPeerAccessControl sets the access control policy for peer connections
func WithPeerAccessControl(accessControl storage.AccessControl) ServerOption {
	return func(s *DefaultPeerServer) {
		s.accessControl = accessControl
	}
}

// WithPeerTimeout sets the connection timeout for peer server operations
func WithPeerTimeout(timeout time.Duration) ServerOption {
	return func(s *DefaultPeerServer) {
		s.connectionTimeout = timeout
	}
}

// WithPeerLogger sets the zap logger specifically for the peer server
func WithPeerLogger(logger *zap.Logger) ServerOption {
	return func(s *DefaultPeerServer) {
		s.logger = logger
	}
}

// WithPeerNotifier sets the notifier for the peer server
func WithPeerNotifier(notifier Notifier) ServerOption {
	return func(s *DefaultPeerServer) {
		s.notifier = notifier
	}
}

// HandleConnection handles an incoming peer connection
func (p *DefaultPeerServer) HandleConnection(conn net.Conn) {
	defer conn.Close()

	// Wrap context with peer source and IP address information
	ctx := wrapPeerContext(context.Background(), conn)

	reader := bufio.NewReader(conn)
	for {
		// Set read deadline before reading message
		err := conn.SetReadDeadline(time.Now().Add(p.connectionTimeout))
		if err != nil {
			ip, _ := GetIPAddressFromContext(ctx)
			p.logger.Error("Error setting read deadline",
				zap.Error(err),
				zap.String("source", string(SourcePeer)),
				zap.String("ip", ip))
			return
		}

		// Read next message using LBRY protocol format
		message, err := p.readNextMessage(reader)
		if err != nil {
			if err != io.EOF {
				p.logger.Error("Error reading from connection", zap.Error(err))
			}
			return
		}

		// Clear read deadline after reading
		err = conn.SetReadDeadline(time.Time{})
		if err != nil {
			p.logger.Error("Error clearing read deadline", zap.Error(err))
			return
		}

		// Check request size
		if len(message) > MaxRequestSize {
			p.sendError(conn, liblbryerrors.ErrRequestTooLarge.Error())
			continue
		}

		// Parse request
		request, err := p.parseRequest(message)
		if err != nil {
			p.sendError(conn, liblbryerrors.ErrInvalidData.Error())
			continue
		}

		// Get peer IP for access control
		peerIP := getPeerIP(conn)

		// Create context for this request with timeout, preserving source and IP info
		ctx, cancel := context.WithTimeout(ctx, p.connectionTimeout)

		// Handle request
		response, blobData, err := p.handleRequest(ctx, request, peerIP)
		if err != nil {
			p.sendError(conn, err.Error())
			cancel()
			continue
		}

		// Set write deadline before writing response
		err = conn.SetWriteDeadline(time.Now().Add(p.connectionTimeout))
		if err != nil {
			p.logger.Error("Error setting write deadline", zap.Error(err))
			cancel()
			return
		}

		// Send response
		if err := p.sendResponse(conn, response, blobData); err != nil {
			if !strings.Contains(err.Error(), "connection reset by peer") && !strings.Contains(err.Error(), "broken pipe") {
				p.logger.Error("Error sending response", zap.Error(err))
			}
			cancel()
			return
		}

		// Clear write deadline after writing
		err = conn.SetWriteDeadline(time.Time{})
		if err != nil {
			p.logger.Error("Error clearing write deadline", zap.Error(err))
			cancel()
			return
		}

		// Cancel the context for this iteration
		cancel()
	}
}

// IsValidJSON checks if a byte slice contains valid JSON
func IsValidJSON(b []byte) bool {
	var r json.RawMessage
	return json.Unmarshal(b, &r) == nil
}

// readNextMessage reads the next complete JSON message from the connection
func (p *DefaultPeerServer) readNextMessage(reader *bufio.Reader) ([]byte, error) {
	// Skip leading ASCII whitespace (space, tab, CR, LF)
	for {
		firstByte, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}

		// Check if byte is not ASCII whitespace
		if firstByte != ' ' && firstByte != '\t' && firstByte != '\r' && firstByte != '\n' {
			// Found non-whitespace byte, check if it's '{'
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
				chunk, err := reader.ReadBytes('}')
				if err != nil {
					return nil, err
				}

				// Append chunk to buffer
				buffer = append(buffer, chunk...)

				// Check if we have valid JSON
				if IsValidJSON(buffer) {
					return buffer, nil
				}

				// If we don't have valid JSON yet, continue reading
			}
		}
		// Continue loop to skip whitespace
	}
}

// parseRequest parses an incoming request
func (p *DefaultPeerServer) parseRequest(data []byte) (CompositeRequest, error) {
	var req CompositeRequest
	if err := json.Unmarshal(data, &req); err != nil {
		var je *json.SyntaxError
		if errors.As(err, &je) {
			return req, fmt.Errorf("invalid json request: offset %d in data %s", je.Offset, hex.EncodeToString(data))
		}
		return req, fmt.Errorf("failed to parse request: %w", err)
	}
	return req, nil
}

// handleRequest processes a parsed request and returns a response
func (p *DefaultPeerServer) handleRequest(ctx context.Context, request CompositeRequest, peerIP string) (CompositeResponse, []byte, error) {
	response := CompositeResponse{
		AvailableBlobs: []string{}, // Always initialize as empty slice
	}

	// Handle blob data request (highest priority)
	if request.RequestedBlob != "" {
		incomingBlob, blobData, err := p.handleBlobDataRequest(ctx, request.RequestedBlob, peerIP)
		if err != nil {
			return CompositeResponse{}, nil, err
		}
		response.IncomingBlob = incomingBlob
		return response, blobData, nil
	}

	// Handle payment rate request
	if request.BlobDataPaymentRate != nil {
		paymentRateResponse, err := p.handleBlobPaymentRateRequest(ctx, request.RequestedBlobs, *request.BlobDataPaymentRate, peerIP)
		if err != nil {
			return CompositeResponse{}, nil, err
		}

		response.BlobDataPaymentRate = paymentRateResponse

		// Also handle availability if requested_blobs is present
		return p.handleBlobAvailabilityInResponse(ctx, request.RequestedBlobs, response, peerIP)
	}

	// Handle blob availability request
	if len(request.RequestedBlobs) > 0 {
		availableBlobs, err := p.handleBlobAvailabilityRequest(ctx, request.RequestedBlobs, peerIP)
		if err != nil {
			return CompositeResponse{}, nil, err
		}
		response.AvailableBlobs = availableBlobs
		return response, nil, nil
	}

	// Return response with empty available_blobs if no specific fields were requested
	return response, nil, nil
}

// handleBlobAvailabilityRequest processes a blob availability check request
func (p *DefaultPeerServer) handleBlobAvailabilityRequest(ctx context.Context, blobHashes []string, peerIP string) ([]string, error) {
	availableBlobs := make([]string, 0)

	// Process each blob hash - handle both nil and empty slice cases
	// When blobHashes is nil, it means the field wasn't present in the request
	// When blobHashes is an empty slice, it means the field was present but empty
	for _, hash := range blobHashes {
		// Validate hash length
		if len(hash) != blob.BlobHashHexLength {
			continue
		}

		// Validate hash format
		if !stream.ValidateHash(hash) {
			continue
		}

		// Check if blob is protected
		if p.isProtected(hash) {
			continue
		}

		// Check access control
		if p.accessControl != nil && !p.accessControl.Allow(hash, peerIP) {
			continue
		}

		// Check if blob exists
		available, err := p.store.Has(ctx, hash)
		if err != nil {
			continue
		}

		if available {
			availableBlobs = append(availableBlobs, hash)
		}
	}

	return availableBlobs, nil
}

// isProtected checks if a blob hash is protected content
func (p *DefaultPeerServer) isProtected(hash string) bool {
	if p.protector != nil {
		return p.protector.IsProtected(hash)
	}
	return false
}

// handleBlobDataRequest processes a blob data request
func (p *DefaultPeerServer) handleBlobDataRequest(ctx context.Context, blobHash string, peerIP string) (*IncomingBlob, []byte, error) {
	// Validate hash length
	if len(blobHash) != blob.BlobHashHexLength {
		return p.createIncomingBlobError(blobHash, liblbryerrors.ErrInvalidHashLen.Error()), nil, nil
	}

	// Validate hash format
	if !stream.ValidateHash(blobHash) {
		return p.createIncomingBlobError(blobHash, liblbryerrors.ErrInvalidHash.Error()), nil, nil
	}

	// Check if blob is protected
	if p.isProtected(blobHash) {
		return p.createIncomingBlobError(blobHash, liblbryerrors.ErrBlobProtected.Error()), nil, nil
	}

	// Check access control
	if p.accessControl != nil && !p.accessControl.Allow(blobHash, peerIP) {
		return p.createIncomingBlobError(blobHash, liblbryerrors.ErrAccessDenied.Error()), nil, nil
	}

	// Get blob data
	data, err := p.store.Get(ctx, blobHash)
	if err != nil {
		// Log detailed error server-side
		p.logger.Error("Failed to retrieve blob", zap.String("blobHash", blobHash), zap.Error(err))
		// Check if this is a "not found" error and return the appropriate error message
		if err.Error() == liblbryerrors.ErrBlobNotFound.Error() || strings.Contains(err.Error(), "blob not found") {
			return p.createIncomingBlobError(blobHash, liblbryerrors.ErrBlobNotFound.Error()), nil, nil
		}
		// Return generic error to client for other errors
		return p.createIncomingBlobError(blobHash, "failed to retrieve blob"), nil, nil
	}

	if data == nil {
		return p.createIncomingBlobError(blobHash, liblbryerrors.ErrBlobNotFound.Error()), nil, nil
	}

	incomingBlob := &IncomingBlob{
		BlobHash: blobHash,
		Length:   len(data),
	}

	// Notify about blob availability if notifier is configured
	NotifyBlob(p.notifier, p.logger, NOTIFY_BLOB_ADDED, blobHash)

	return incomingBlob, data, nil
}

// handleBlobPaymentRateRequest processes a payment rate negotiation request
func (p *DefaultPeerServer) handleBlobPaymentRateRequest(ctx context.Context, blobHashes []string, paymentRate float64, peerIP string) (string, error) {
	// Check if payment rate is negative
	if paymentRate < 0 {
		return PaymentRateTooLow, nil
	}

	// For now, we only handle the first blob hash in the list
	if len(blobHashes) == 0 {
		return PaymentRateAccepted, nil
	}

	blobHash := blobHashes[0]

	// Validate hash length
	if len(blobHash) != blob.BlobHashHexLength {
		return PaymentRateTooLow, nil
	}

	// Validate hash format
	if !stream.ValidateHash(blobHash) {
		return PaymentRateTooLow, nil
	}

	// Check if blob is protected
	if p.isProtected(blobHash) {
		return liblbryerrors.ErrBlobProtected.Error(), nil
	}

	// Check access control
	if p.accessControl != nil && !p.accessControl.Allow(blobHash, peerIP) {
		return PaymentRateTooLow, nil
	}

	// Check if blob exists
	available, err := p.store.Has(ctx, blobHash)
	if err != nil || !available {
		return PaymentRateTooLow, nil
	}

	return PaymentRateAccepted, nil
}

// sendResponse sends a response back to the client
func (p *DefaultPeerServer) sendResponse(conn net.Conn, response CompositeResponse, blobData []byte) error {
	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// If blob data is provided, send JSON response and blob data in a single atomic write
	if blobData != nil {
		data = append(data, blobData...)
	}

	// Send response (and blob data if provided)
	_, err = conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	return nil
}

// sendError sends an error response back to the client
func (p *DefaultPeerServer) sendError(conn net.Conn, errorMsg string) {
	// Ensure error writes respect connection timeout
	if err := conn.SetWriteDeadline(time.Now().Add(p.connectionTimeout)); err != nil {
		p.logger.Error("Error setting write deadline", zap.Error(err))
		return
	}
	defer func() {
		if err := conn.SetWriteDeadline(time.Time{}); err != nil {
			p.logger.Error("Error clearing write deadline", zap.Error(err))
		}
	}()

	response := createErrorResponse(errorMsg)
	data, err := json.Marshal(response)
	if err != nil {
		p.logger.Error("Failed to marshal error response", zap.Error(err))
		return
	}

	_, err = conn.Write(data)
	if err != nil {
		p.logger.Error("Failed to send error response", zap.Error(err))
	}
}

// createErrorResponse creates a standardized error response
func createErrorResponse(errorMsg string) CompositeResponse {
	return CompositeResponse{
		IncomingBlob: &IncomingBlob{
			Error: errorMsg,
		},
	}
}

// createIncomingBlobError creates a standardized IncomingBlob error response
func (p *DefaultPeerServer) createIncomingBlobError(blobHash string, errorMsg string) *IncomingBlob {
	return &IncomingBlob{
		Error:    errorMsg,
		BlobHash: blobHash,
		Length:   0,
	}
}

// handleBlobAvailabilityInResponse handles blob availability checking and updates response
func (p *DefaultPeerServer) handleBlobAvailabilityInResponse(ctx context.Context, blobHashes []string, response CompositeResponse, peerIP string) (CompositeResponse, []byte, error) {
	if len(blobHashes) > 0 {
		availableBlobs, err := p.handleBlobAvailabilityRequest(ctx, blobHashes, peerIP)
		if err != nil {
			return response, nil, err
		}
		response.AvailableBlobs = availableBlobs
	}
	return response, nil, nil
}
