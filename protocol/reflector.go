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
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/crypto"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// ReflectorServer defines the interface for handling reflector connections
type ReflectorServer interface {
	ConnectionHandler
}

// wrapReflectorContext creates a new context with reflector source and IP address information
func wrapReflectorContext(ctx context.Context, conn net.Conn) context.Context {
	ip := GetConnectionIP(conn)
	ctx = context.WithValue(ctx, SourceContextKey, SourceReflector)
	ctx = context.WithValue(ctx, IPAddressContextKey, ip)
	return ctx
}

// Protocol constants
const (
	DefaultReflectorTimeout = 30 * time.Second
	MaxBlobSize             = blob.MaxBlobSize

	// Timeout calculation constants
	BaseTimeout   = 10 * time.Second // Base timeout for any operation
	TimeoutPerMiB = 5 * time.Second  // Additional time per MiB of data
	MiB           = 1024 * 1024      // Bytes in a MiB

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
	ErrNegativeBlobSize   = errors.New("negative blob size received")
)

// DefaultReflectorServer implements the ReflectorServer interface
type DefaultReflectorServer struct {
	store             storage.BlobStore
	accessControl     storage.AccessControl
	connectionTimeout time.Duration
	logger            *zap.Logger
	notifier          Notifier
}

// ReflectorServerOption configures the reflector server
type ReflectorServerOption func(*DefaultReflectorServer)

// NewReflectorServer creates a new reflector server instance with optional configuration
func NewReflectorServer(store storage.BlobStore, options ...ReflectorServerOption) ReflectorServer {
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
func WithReflectorAccessControl(accessControl storage.AccessControl) ReflectorServerOption {
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

// WithReflectorNotifier sets the notifier for the server
func WithReflectorNotifier(notifier Notifier) ReflectorServerOption {
	return func(s *DefaultReflectorServer) {
		s.notifier = notifier
	}
}

// HandleConnection handles an incoming reflector connection
func (r *DefaultReflectorServer) HandleConnection(conn net.Conn) {
	defer conn.Close()

	// Wrap context with reflector source and IP address information
	ctx := wrapReflectorContext(context.Background(), conn)

	reader := bufio.NewReader(conn)

	// Perform handshake
	if err := r.doHandshake(conn, reader); err != nil {
		ip, _ := GetIPAddressFromContext(ctx)
		r.logger.Error("Handshake failed",
			zap.Error(err),
			zap.String("source", string(SourceReflector)),
			zap.String("ip", ip))
		r.sendError(conn, err)
		return
	}

	// Handle blob uploads
	for {
		// Create context for each blob operation with timeout, preserving source and IP info
		ctx, cancel := context.WithTimeout(ctx, r.connectionTimeout)

		err := r.receiveBlob(ctx, conn, reader)
		cancel() // Cancel context when done with this blob

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
func (r *DefaultReflectorServer) receiveBlob(ctx context.Context, conn net.Conn, reader *bufio.Reader) error {
	// Read blob request
	blobSize, blobHash, isSdBlob, err := r.readBlobRequest(conn, reader)
	if err != nil {
		return err
	}

	peerIP, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return errors.Join(liblbryerrors.ErrFailedToParsePeerAddress, err)
	}

	// Check if we want this blob
	shouldSend, neededBlobs, err := r.shouldAcceptBlob(ctx, blobHash, isSdBlob, peerIP)
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
		return fmt.Errorf("error reading blob %s: %w", safeHashPrefix(blobHash), err)
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

	r.logger.Debug("Received blob", zap.String("hash", safeHashPrefix(blobHash)))

	// Store blob
	if isSdBlob {
		err = r.store.PutSD(ctx, blobHash, blobData)
	} else {
		err = r.store.Put(ctx, blobHash, blobData)
	}
	if err != nil {
		return errors.Join(liblbryerrors.ErrFailedToStoreBlob, fmt.Errorf("failed to store blob %s: %w", safeHashPrefix(blobHash), err))
	}

	// Notify about blob addition
	NotifyBlob(r.notifier, r.logger, NOTIFY_BLOB_ADDED, blobHash)

	// Send transfer success response
	return r.sendTransferResponse(conn, true, isSdBlob)
}

// readBlobRequest reads and validates a blob upload request
func (r *DefaultReflectorServer) readBlobRequest(conn net.Conn, reader *bufio.Reader) (int, string, bool, error) {
	var request SendBlobRequest
	if err := r.readJSON(conn, reader, &request); err != nil {
		return 0, "", false, errors.Join(ErrInvalidBlobRequest, err)
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
	if !ValidateBlobHash(blobHash) {
		return blobSize, blobHash, isSdBlob, errors.Join(ErrInvalidBlobRequest, liblbryerrors.ErrInvalidData)
	}
	if blobSize > MaxBlobSize {
		return blobSize, blobHash, isSdBlob, ErrBlobTooBig
	}
	if blobSize <= 0 {
		if blobSize == 0 {
			return blobSize, blobHash, isSdBlob, ErrZeroByteBlob
		}
		return blobSize, blobHash, isSdBlob, ErrNegativeBlobSize
	}

	return blobSize, blobHash, isSdBlob, nil
}

// shouldAcceptBlob determines if the server should accept a blob
func (r *DefaultReflectorServer) shouldAcceptBlob(ctx context.Context, blobHash string, isSdBlob bool, peerIP string) (bool, []string, error) {
	// Check access control
	if r.accessControl != nil && !r.accessControl.Allow(ctx, blobHash, peerIP) {
		return false, []string{}, nil
	}

	var wantsBlob bool
	var err error

	// Check if store implements Blocklister interface
	var decidedByBlocklister bool
	if blocklister, ok := r.store.(storage.Blocklister); ok {
		wantsBlob, err = blocklister.Wants(blobHash)
		if err != nil {
			return false, nil, errors.Join(liblbryerrors.ErrFailedToCheckBlobExistence, err)
		}
		decidedByBlocklister = true
	} else {
		// Fall back to Has() method
		blobExists, err := r.store.Has(ctx, blobHash)
		if err != nil {
			return false, nil, errors.Join(liblbryerrors.ErrFailedToCheckBlobExistence, err)
		}
		wantsBlob = !blobExists
		decidedByBlocklister = false
	}

	var neededBlobs []string

	// For SD blobs that we don't want, check if we need any blobs from the stream
	if isSdBlob && !wantsBlob {
		if neededChecker, ok := r.store.(storage.NeededBlobChecker); ok {
			neededBlobs, err = neededChecker.MissingBlobsForKnownStream(blobHash)
			if err != nil {
				return false, nil, errors.Join(liblbryerrors.ErrFailedToCheckNeededBlobs, err)
			}
		} else {
			// If we can't check for blobs in a stream, we have to say that SD blob is missing
			// If we say we have SD blob, they won't try to send any content blobs
			// Only override the decision if it came from the legacy Has() path, not from Blocklister
			if !decidedByBlocklister {
				wantsBlob = true
			}
		}
	}

	return wantsBlob, neededBlobs, nil
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
		return errors.Join(liblbryerrors.ErrFailedToMarshalBlobResponse, err)
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
		return errors.Join(liblbryerrors.ErrFailedToMarshalTransferResponse, err)
	}

	return r.writeData(conn, response)
}

// calculateTimeout calculates a timeout based on blob size
// Formula: base timeout + (blobSize / MiB) * timeout per MiB
// Minimum timeout is BaseTimeout (10s), maximum is capped at 5 minutes
func (r *DefaultReflectorServer) calculateTimeout(blobSize int) time.Duration {
	// Calculate additional time needed for the blob size
	additionalTime := time.Duration(blobSize/MiB) * TimeoutPerMiB

	// Ensure minimum of base timeout
	timeout := BaseTimeout + additionalTime

	// Cap at reasonable maximum (5 minutes)
	if timeout > 5*time.Minute {
		timeout = 5 * time.Minute
	}

	return timeout
}

// readRawBlob reads raw blob data from the connection with timeout
// The timeout is dynamically calculated based on blob size to accommodate
// slower connections transferring larger blobs.
func (r *DefaultReflectorServer) readRawBlob(conn net.Conn, reader *bufio.Reader, blobSize int) ([]byte, error) {
	if err := conn.SetReadDeadline(time.Now().Add(r.calculateTimeout(blobSize))); err != nil {
		return nil, errors.Join(liblbryerrors.ErrFailedToSetReadDeadline, err)
	}

	blob := make([]byte, blobSize)
	_, err := io.ReadFull(reader, blob)
	if err != nil {
		return nil, errors.Join(liblbryerrors.ErrFailedToReadBlobData, err)
	}

	return blob, nil
}

// readJSON reads and unmarshals JSON from the connection with timeout
func (r *DefaultReflectorServer) readJSON(conn net.Conn, reader *bufio.Reader, v any) error {
	if err := conn.SetReadDeadline(time.Now().Add(r.connectionTimeout)); err != nil {
		return errors.Join(liblbryerrors.ErrFailedToSetReadDeadline, err)
	}

	dec := json.NewDecoder(reader)
	if err := dec.Decode(v); err != nil {
		data, _ := io.ReadAll(dec.Buffered())
		if len(data) > 0 {
			return errors.Join(liblbryerrors.ErrFailedToDecodeJSON, fmt.Errorf("failed to decode JSON; data=%s: %w", hex.EncodeToString(data), err))
		}
		return errors.Join(liblbryerrors.ErrFailedToDecodeJSON, err)
	}
	return nil
}

// writeJSON marshals and writes JSON to the connection
func (r *DefaultReflectorServer) writeJSON(conn net.Conn, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return errors.Join(liblbryerrors.ErrFailedToMarshalJSON, err)
	}

	return r.writeData(conn, data)
}

// writeData writes raw data to the connection
func (r *DefaultReflectorServer) writeData(conn net.Conn, data []byte) error {
	if err := conn.SetWriteDeadline(time.Now().Add(r.connectionTimeout)); err != nil {
		return errors.Join(liblbryerrors.ErrFailedToSetWriteDeadline, err)
	}

	n, err := conn.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		return errors.Join(liblbryerrors.ErrFailedToWriteData, err)
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

// safeHashPrefix returns a safe prefix of a hash string (up to 8 characters)
func safeHashPrefix(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

// sendError sends an error response
func (r *DefaultReflectorServer) sendError(_ net.Conn, err error) {
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
