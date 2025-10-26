// Code adapted from https://github.com/LBRYFoundation/reflector.go - MIT License
//
// Adaptations made for liblbry:
//   - Implemented LBRY reflector protocol server for blob uploads
//   - Added support for handshake, blob requests, and transfer responses
//   - Integrated with liblbry's blob store and access control interfaces
//   - Enhanced with connection timeout handling and proper error management
//   - Added support for SD blob handling and needed blob checking
//   - Maintained protocol compatibility with LBRY reflector specification

package protocol

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/crypto"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// ReflectorServer defines the interface for handling reflector connections
type ReflectorServer interface {
	HandleConnection(conn net.Conn)
}

// Protocol constants
const (
	DefaultReflectorTimeout = 5 * time.Second
	MaxBlobSize             = blob.MaxBlobSize

	// Protocol versions
	ProtocolVersion1 = 0
	ProtocolVersion2 = 1
)

// Protocol errors
var (
	ErrBlobTooBig         = fmt.Errorf("blob must be at most %d bytes", MaxBlobSize)
	ErrProtocolVersion    = errors.New("protocol version not supported")
	ErrHandshakeMissing   = errors.New("handshake is missing protocol version")
	ErrBlobHashEmpty      = errors.New("blob hash is empty")
	ErrZeroByteBlob       = errors.New("0-byte blob received")
	ErrHashMismatch       = errors.New("hash of received blob data does not match hash from send request")
	ErrInvalidHandshake   = errors.New("invalid handshake")
	ErrInvalidBlobRequest = errors.New("invalid blob request")
	ErrInvalidTransfer    = errors.New("invalid transfer response")
)

// HandshakeRequestResponse represents the handshake message
type HandshakeRequestResponse struct {
	Version *int `json:"version"`
}

// SendBlobRequest represents a blob upload request
type SendBlobRequest struct {
	BlobHash   string `json:"blob_hash,omitempty"`
	BlobSize   int    `json:"blob_size,omitempty"`
	SdBlobHash string `json:"sd_blob_hash,omitempty"`
	SdBlobSize int    `json:"sd_blob_size,omitempty"`
}

// SendBlobResponse represents a response to a regular blob request
type SendBlobResponse struct {
	SendBlob bool `json:"send_blob"`
}

// SendSDBlobResponse represents a response to an SD blob request
type SendSDBlobResponse struct {
	SendSdBlob  bool     `json:"send_sd_blob"`
	NeededBlobs []string `json:"needed_blobs,omitempty"`
}

// BlobTransferResponse represents a response after blob transfer
type BlobTransferResponse struct {
	ReceivedBlob bool `json:"received_blob"`
}

// SDBlobTransferResponse represents a response after SD blob transfer
type SDBlobTransferResponse struct {
	ReceivedSdBlob bool `json:"received_sd_blob"`
}

// DefaultReflectorServer implements the ReflectorServer interface
type DefaultReflectorServer struct {
	store             liblbry.BlobStore
	accessControl     liblbry.AccessControl
	connectionTimeout time.Duration
	logger            *zap.Logger
}

// ReflectorServerOption configures the reflector server
type ReflectorServerOption func(*DefaultReflectorServer)

// NewReflectorServer creates a new reflector server instance with optional configuration
func NewReflectorServer(store liblbry.BlobStore, options ...ReflectorServerOption) ReflectorServer {
	server := &DefaultReflectorServer{
		store:             store,
		accessControl:     nil, // Default to no access control
		connectionTimeout: DefaultReflectorTimeout,
		logger:            zap.NewNop(), // Default to no-op logger
	}

	applyReflectorOptions(server, options)

	return server
}

// WithReflectorAccessControl sets the access control for reflector connections
func WithReflectorAccessControl(accessControl liblbry.AccessControl) ReflectorServerOption {
	return func(s *DefaultReflectorServer) {
		s.accessControl = accessControl
	}
}

// WithReflectorTimeout sets the connection timeout
func WithReflectorTimeout(timeout time.Duration) ReflectorServerOption {
	return func(s *DefaultReflectorServer) {
		s.connectionTimeout = timeout
	}
}

// WithReflectorLogger sets the zap logger for the server
func WithReflectorLogger(logger *zap.Logger) ReflectorServerOption {
	return func(s *DefaultReflectorServer) {
		s.logger = logger
	}
}

// HandleConnection handles an incoming reflector connection
func (r *DefaultReflectorServer) HandleConnection(conn net.Conn) {
	defer conn.Close()

	reader := bufio.NewReader(conn)

	// Perform handshake
	if err := r.doHandshake(conn, reader); err != nil {
		r.logger.Error("Handshake failed", zap.Error(err))
		r.sendError(conn, err)
		return
	}

	// Handle blob uploads
	for {
		err := r.receiveBlob(conn, reader)
		if err != nil {
			if err == io.EOF {
				return // Normal connection close
			}
			r.logger.Error("Error receiving blob", zap.Error(err))
			r.sendError(conn, err)
			return
		}
	}
}

// doHandshake performs the protocol handshake
func (r *DefaultReflectorServer) doHandshake(conn net.Conn, reader *bufio.Reader) error {
	var handshake HandshakeRequestResponse
	if err := r.readJSON(conn, reader, &handshake); err != nil {
		return err
	}

	if handshake.Version == nil {
		return ErrHandshakeMissing
	}

	if *handshake.Version != ProtocolVersion1 && *handshake.Version != ProtocolVersion2 {
		return ErrProtocolVersion
	}

	// Respond with same version
	response := HandshakeRequestResponse{Version: handshake.Version}
	return r.writeJSON(conn, response)
}

// receiveBlob handles a blob upload request
func (r *DefaultReflectorServer) receiveBlob(conn net.Conn, reader *bufio.Reader) error {
	// Read blob request
	blobSize, blobHash, isSdBlob, err := r.readBlobRequest(conn, reader)
	if err != nil {
		return err
	}

	peerIP, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return fmt.Errorf("failed to parse peer address: %w", err)
	}

	// Check if we want this blob
	shouldSend, neededBlobs, err := r.shouldAcceptBlob(blobHash, isSdBlob, peerIP)
	if err != nil {
		return err
	}

	// Send response
	if err := r.sendBlobResponse(conn, shouldSend, isSdBlob, neededBlobs); err != nil {
		return err
	}

	if !shouldSend {
		return nil // We don't want this blob
	}

	// Read blob data
	blobData, err := r.readRawBlob(conn, reader, blobSize)
	if err != nil {
		// Send transfer failure response
		if sendErr := r.sendTransferResponse(conn, false, isSdBlob); sendErr != nil {
			r.logger.Error("Error sending transfer failure response", zap.Error(sendErr))
		}
		return fmt.Errorf("error reading blob %s: %w", blobHash[:8], err)
	}

	// Verify blob hash
	receivedHash := r.calculateBlobHash(blobData)
	if blobHash != receivedHash {
		// Send transfer failure response
		if sendErr := r.sendTransferResponse(conn, false, isSdBlob); sendErr != nil {
			r.logger.Error("Error sending transfer failure response", zap.Error(sendErr))
		}
		return ErrHashMismatch
	}

	r.logger.Debug("Received blob", zap.String("hash", blobHash[:8]))

	// Store blob
	if isSdBlob {
		err = r.store.PutSD(blobHash, blobData)
	} else {
		err = r.store.Put(blobHash, blobData)
	}
	if err != nil {
		return fmt.Errorf("failed to store blob %s: %w", blobHash[:8], err)
	}

	// Send transfer success response
	return r.sendTransferResponse(conn, true, isSdBlob)
}

// readBlobRequest reads and validates a blob upload request
func (r *DefaultReflectorServer) readBlobRequest(conn net.Conn, reader *bufio.Reader) (int, string, bool, error) {
	var request SendBlobRequest
	if err := r.readJSON(conn, reader, &request); err != nil {
		return 0, "", false, fmt.Errorf("%w: %v", ErrInvalidBlobRequest, err)
	}

	var blobHash string
	var blobSize int
	isSdBlob := request.SdBlobHash != ""

	if isSdBlob {
		blobSize = request.SdBlobSize
		blobHash = request.SdBlobHash
	} else {
		blobSize = request.BlobSize
		blobHash = request.BlobHash
	}

	if blobHash == "" {
		return blobSize, blobHash, isSdBlob, ErrBlobHashEmpty
	}
	if blobSize > MaxBlobSize {
		return blobSize, blobHash, isSdBlob, ErrBlobTooBig
	}
	if blobSize == 0 {
		return blobSize, blobHash, isSdBlob, ErrZeroByteBlob
	}

	return blobSize, blobHash, isSdBlob, nil
}

// shouldAcceptBlob determines if the server should accept a blob
func (r *DefaultReflectorServer) shouldAcceptBlob(blobHash string, isSdBlob bool, peerIP string) (bool, []string, error) {
	// Check access control
	if r.accessControl != nil && !r.accessControl.Allow(blobHash, peerIP) {
		return false, []string{}, nil
	}

	// Check if blob already exists
	blobExists, err := r.store.Has(blobHash)
	if err != nil {
		return false, nil, fmt.Errorf("failed to check blob existence: %w", err)
	}

	if blobExists {
		return false, nil, nil // Already have this blob
	}

	var neededBlobs []string

	// For SD blobs, check if we need any blobs from the stream
	if isSdBlob {
		// Try to get needed blobs if store supports it
		if neededChecker, ok := r.store.(interface {
			MissingBlobsForKnownStream(string) ([]string, error)
		}); ok {
			neededBlobs, err = neededChecker.MissingBlobsForKnownStream(blobHash)
			if err != nil {
				return false, nil, fmt.Errorf("failed to check needed blobs: %w", err)
			}
		}
		// If we can't check needed blobs, we'll accept the SD blob anyway
	}

	return true, neededBlobs, nil
}

// sendBlobResponse sends a response to a blob request
func (r *DefaultReflectorServer) sendBlobResponse(conn net.Conn, shouldSend, isSdBlob bool, neededBlobs []string) error {
	var response []byte
	var err error

	if isSdBlob {
		response, err = json.Marshal(SendSDBlobResponse{
			SendSdBlob:  shouldSend,
			NeededBlobs: neededBlobs,
		})
	} else {
		response, err = json.Marshal(SendBlobResponse{SendBlob: shouldSend})
	}
	if err != nil {
		return fmt.Errorf("failed to marshal blob response: %w", err)
	}

	return r.writeData(conn, response)
}

// sendTransferResponse sends a response after blob transfer
func (r *DefaultReflectorServer) sendTransferResponse(conn net.Conn, receivedBlob, isSdBlob bool) error {
	var response []byte
	var err error

	if isSdBlob {
		response, err = json.Marshal(SDBlobTransferResponse{ReceivedSdBlob: receivedBlob})
	} else {
		response, err = json.Marshal(BlobTransferResponse{ReceivedBlob: receivedBlob})
	}
	if err != nil {
		return fmt.Errorf("failed to marshal transfer response: %w", err)
	}

	return r.writeData(conn, response)
}

// readRawBlob reads raw blob data from the connection with timeout
func (r *DefaultReflectorServer) readRawBlob(conn net.Conn, reader *bufio.Reader, blobSize int) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(r.connectionTimeout)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	blob := make([]byte, blobSize)
	_, err := io.ReadFull(reader, blob)
	if err != nil {
		return nil, fmt.Errorf("failed to read blob data: %w", err)
	}

	return blob, nil
}

// readJSON reads and unmarshals JSON from the connection with timeout
func (r *DefaultReflectorServer) readJSON(conn net.Conn, reader *bufio.Reader, v interface{}) error {
	if err := conn.SetReadDeadline(time.Now().Add(r.connectionTimeout)); err != nil {
		return fmt.Errorf("failed to set read deadline: %w", err)
	}

	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		data, _ := io.ReadAll(dec.Buffered())
		if len(data) > 0 {
			return fmt.Errorf("failed to decode JSON: %w; data=%s", err, hex.EncodeToString(data))
		}
		return fmt.Errorf("failed to decode JSON: %w", err)
	}
	return nil
}

// writeJSON marshals and writes JSON to the connection
func (r *DefaultReflectorServer) writeJSON(conn net.Conn, v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	return r.writeData(conn, data)
}

// writeData writes raw data to the connection
func (r *DefaultReflectorServer) writeData(conn net.Conn, data []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(r.connectionTimeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	n, err := conn.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return fmt.Errorf("failed to write data: %w", err)
	}

	return nil
}

// applyReflectorOptions applies all ReflectorServerOption functions to the server instance
// This helper function centralizes the option application logic,
// making it easier to maintain and extend. It follows the functional
// options pattern which provides a clean and flexible way to configure
// a server instance during construction.
func applyReflectorOptions(server *DefaultReflectorServer, options []ReflectorServerOption) {
	for _, option := range options {
		option(server)
	}
}

// sendError sends an error response
func (r *DefaultReflectorServer) sendError(conn net.Conn, err error) {
	// Log the error
	r.logger.Error("Reflector server error", zap.Error(err))

	// For now, we don't send specific error responses in the reflector protocol
	// The client will detect the connection close or timeout
}

// calculateBlobHash calculates the SHA-384 hash of blob data
func (r *DefaultReflectorServer) calculateBlobHash(blobData []byte) string {
	hasher := crypto.NewHasher()
	return hasher.Hash(blobData)
}

// ValidateBlobHash checks if a blob hash is valid
func ValidateBlobHash(hash string) bool {
	if len(hash) != blob.BlobHashHexLength {
		return false
	}
	return stream.ValidateHash(hash)
}
