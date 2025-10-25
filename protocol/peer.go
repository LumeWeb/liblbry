package protocol

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strings"
	"time"

	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// PeerServer defines the interface for handling peer connections
type PeerServer interface {
	HandleConnection(conn net.Conn)
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
	LbrycrdAddress = "127.0.0.1:50001"

	// Response constants
	PaymentRateAccepted = "RATE_ACCEPTED"
	PaymentRateTooLow   = "RATE_TOO_LOW"

	// Error constants
	ErrRequestTooLarge = "request is too large"
	ErrInvalidData     = "Invalid data"
	ErrBlobNotFound    = "blob not found"
	ErrAccessDenied    = "access denied"
	ErrInvalidHash     = "invalid hash"
	ErrInvalidHashLen  = "Invalid blob hash length"
	ErrBlobProtected   = "requested blob is protected"
)

// Protocol errors
var (
	errRequestTooLarge = fmt.Errorf(ErrRequestTooLarge)
	errInvalidData     = fmt.Errorf(ErrInvalidData)
)

// CompositeRequest represents a composite protocol message
type CompositeRequest struct {
	LBRYcrdAddress      bool     `json:"lbrycrd_address,omitempty"`
	RequestedBlobs      []string `json:"requested_blobs,omitempty"`
	BlobDataPaymentRate *float64 `json:"blob_data_payment_rate,omitempty"`
	RequestedBlob       string   `json:"requested_blob,omitempty"`
}

// CompositeResponse represents a composite protocol response
type CompositeResponse struct {
	LbrycrdAddress      string        `json:"lbrycrd_address,omitempty"`
	AvailableBlobs      []string      `json:"available_blobs"`
	BlobDataPaymentRate string        `json:"blob_data_payment_rate,omitempty"`
	IncomingBlob        *IncomingBlob `json:"incoming_blob,omitempty"`
}

// IncomingBlob represents blob data being transferred
type IncomingBlob struct {
	Error    string `json:"error,omitempty"`
	BlobHash string `json:"blob_hash"`
	Length   int    `json:"length"`
}

// DefaultPeerServer implements the PeerServer interface
type DefaultPeerServer struct {
	store             liblbry.BlobStore
	protector         Protector
	accessControl     liblbry.AccessControl
	connectionTimeout time.Duration
	logger            *zap.Logger
}

// ServerOption configures the peer server
type ServerOption func(*DefaultPeerServer)

// applyOptions applies all ServerOption functions to the server instance
// This helper function centralizes the option application logic,
// making it easier to maintain and extend. It follows the functional
// options pattern which provides a clean and flexible way to configure
// a server instance during construction.
func applyOptions(server *DefaultPeerServer, options []ServerOption) {
	for _, option := range options {
		option(server)
	}
}

// NewPeerServer creates a new peer server instance with optional configuration
func NewPeerServer(store liblbry.BlobStore, options ...ServerOption) PeerServer {
	server := &DefaultPeerServer{
		store:             store,
		protector:         nil, // Default to no protection
		accessControl:     nil, // Default to no access control
		connectionTimeout: DefaultTimeout,
		logger:            zap.NewNop(), // Default to no-op logger
	}

	// Apply options using helper function
	applyOptions(server, options)

	return server
}

// WithProtector sets the protector for content protection
func WithProtector(protector Protector) ServerOption {
	return func(s *DefaultPeerServer) {
		s.protector = protector
	}
}

// WithAccessControl sets the access control for peer connections
func WithAccessControl(accessControl liblbry.AccessControl) ServerOption {
	return func(s *DefaultPeerServer) {
		s.accessControl = accessControl
	}
}

// WithTimeout sets the connection timeout
func WithTimeout(timeout time.Duration) ServerOption {
	return func(s *DefaultPeerServer) {
		s.connectionTimeout = timeout
	}
}

// WithLogger sets the zap logger for the server
func WithLogger(logger *zap.Logger) ServerOption {
	return func(s *DefaultPeerServer) {
		s.logger = logger
	}
}

// HandleConnection handles an incoming peer connection
func (p *DefaultPeerServer) HandleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)
	for {
		// Set read deadline before reading message
		err := conn.SetReadDeadline(time.Now().Add(p.connectionTimeout))
		if err != nil {
			p.logger.Error("Error setting read deadline", zap.Error(err))
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
			p.sendError(conn, ErrRequestTooLarge)
			continue
		}

		// Parse request
		request, err := p.parseRequest(message)
		if err != nil {
			p.sendError(conn, ErrInvalidData)
			continue
		}

		// Get peer IP for access control
		peerIP := getPeerIP(conn)

		// Handle request
		response, blobData, err := p.handleRequest(request, peerIP)
		if err != nil {
			p.sendError(conn, err.Error())
			continue
		}

		// Set write deadline before writing response
		err = conn.SetWriteDeadline(time.Now().Add(p.connectionTimeout))
		if err != nil {
			p.logger.Error("Error setting write deadline", zap.Error(err))
			return
		}

		// Send response
		if err := p.sendResponse(conn, response, blobData); err != nil {
			if !strings.Contains(err.Error(), "connection reset by peer") && !strings.Contains(err.Error(), "broken pipe") {
				p.logger.Error("Error sending response", zap.Error(err))
			}
			return
		}

		// Clear write deadline after writing
		err = conn.SetWriteDeadline(time.Time{})
		if err != nil {
			p.logger.Error("Error clearing write deadline", zap.Error(err))
			return
		}
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
				return nil, errInvalidData
			}

			// Create buffer with first byte
			buffer := []byte{firstByte}

			// Read until we have a complete valid JSON message
			for {
				// Check if we've exceeded max request size
				if len(buffer) > MaxRequestSize {
					return nil, errRequestTooLarge
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
func (p *DefaultPeerServer) handleRequest(request CompositeRequest, peerIP string) (CompositeResponse, []byte, error) {
	response := CompositeResponse{
		AvailableBlobs: []string{}, // Always initialize as empty slice
	}

	// Handle blob data request (highest priority)
	if request.RequestedBlob != "" {
		incomingBlob, blobData, err := p.handleBlobDataRequest(request.RequestedBlob, peerIP)
		if err != nil {
			return CompositeResponse{}, nil, err
		}
		response.IncomingBlob = incomingBlob
		return response, blobData, nil
	}

	// Handle payment rate request
	if request.BlobDataPaymentRate != nil {
		paymentRateResponse, err := p.handleBlobPaymentRateRequest(request.RequestedBlobs, *request.BlobDataPaymentRate, peerIP)
		if err != nil {
			return CompositeResponse{}, nil, err
		}

		response.BlobDataPaymentRate = paymentRateResponse

		// Also handle availability if requested_blobs is present
		return p.handleBlobAvailabilityInResponse(request.RequestedBlobs, response, peerIP)
	}

	// Handle LBRYcrd address request
	if request.LBRYcrdAddress {
		response.LbrycrdAddress = LbrycrdAddress

		// Also handle availability if requested_blobs is present
		return p.handleBlobAvailabilityInResponse(request.RequestedBlobs, response, peerIP)
	}

	// Handle blob availability request
	if len(request.RequestedBlobs) > 0 {
		availableBlobs, err := p.handleBlobAvailabilityRequest(request.RequestedBlobs, peerIP)
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
func (p *DefaultPeerServer) handleBlobAvailabilityRequest(blobHashes []string, peerIP string) ([]string, error) {
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
		available, err := p.store.Has(hash)
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
func (p *DefaultPeerServer) handleBlobDataRequest(blobHash string, peerIP string) (*IncomingBlob, []byte, error) {
	// Validate hash length
	if len(blobHash) != blob.BlobHashHexLength {
		return p.createIncomingBlobError(blobHash, ErrInvalidHashLen), nil, nil
	}

	// Validate hash format
	if !stream.ValidateHash(blobHash) {
		return p.createIncomingBlobError(blobHash, ErrInvalidHash), nil, nil
	}

	// Check if blob is protected
	if p.isProtected(blobHash) {
		return p.createIncomingBlobError(blobHash, ErrBlobProtected), nil, nil
	}

	// Check access control
	if p.accessControl != nil && !p.accessControl.Allow(blobHash, peerIP) {
		return p.createIncomingBlobError(blobHash, ErrAccessDenied), nil, nil
	}

	// Get blob data
	data, err := p.store.Get(blobHash)
	if err != nil {
		// Log detailed error server-side
		p.logger.Error("Failed to retrieve blob", zap.String("blobHash", blobHash), zap.Error(err))
		// Return generic error to client
		return p.createIncomingBlobError(blobHash, "failed to retrieve blob"), nil, nil
	}

	if data == nil {
		return p.createIncomingBlobError(blobHash, ErrBlobNotFound), nil, nil
	}

	incomingBlob := &IncomingBlob{
		BlobHash: blobHash,
		Length:   len(data),
	}

	return incomingBlob, data, nil
}

// handleBlobPaymentRateRequest processes a payment rate negotiation request
func (p *DefaultPeerServer) handleBlobPaymentRateRequest(blobHashes []string, paymentRate float64, peerIP string) (string, error) {
	// Check if payment rate is negative
	if paymentRate < 0 {
		return PaymentRateTooLow, nil
	}

	// For now, we only handle the first blob hash in the list
	if len(blobHashes) == 0 {
		return PaymentRateAccepted, nil
	}

	blobHash := blobHashes[0]

	// Validate hash
	if !stream.ValidateHash(blobHash) {
		return PaymentRateTooLow, nil
	}

	// Check access control
	if p.accessControl != nil && !p.accessControl.Allow(blobHash, peerIP) {
		return PaymentRateTooLow, nil
	}

	// Check if blob exists
	available, err := p.store.Has(blobHash)
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

	// Send JSON response
	_, err = conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write response: %w", err)
	}

	// If blob data is provided, send it after the JSON response
	if blobData != nil {
		_, err = conn.Write(blobData)
		if err != nil {
			return fmt.Errorf("failed to write blob data: %w", err)
		}
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
func (p *DefaultPeerServer) handleBlobAvailabilityInResponse(blobHashes []string, response CompositeResponse, peerIP string) (CompositeResponse, []byte, error) {
	if len(blobHashes) > 0 {
		availableBlobs, err := p.handleBlobAvailabilityRequest(blobHashes, peerIP)
		if err != nil {
			return response, nil, err
		}
		response.AvailableBlobs = availableBlobs
	}
	return response, nil, nil
}
