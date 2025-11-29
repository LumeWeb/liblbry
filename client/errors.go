package client

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/protocol"
	"go.lumeweb.com/liblbry/stream"
)

// Error types for StreamClient operations
var (
	ErrStreamNotFound   = liblbryerrors.Err("stream not found")
	ErrBlobAcquisition  = liblbryerrors.Err("failed to acquire blob")
	ErrStreamCorrupted  = liblbryerrors.Err("stream data is corrupted")
	ErrInvalidHash      = liblbryerrors.Err("invalid blob hash")
	ErrDecryptionFailed = liblbryerrors.Err("blob decryption failed")
	ErrContextCancelled = liblbryerrors.Err("operation cancelled by context")
	ErrNotSDBlob        = liblbryerrors.Err("blob is not an SD blob (appears to be binary content data)")
)

// Stream operation types for error reporting
const (
	OperationStorageHasCheck = "storage_has_check"
	OperationStorageGet      = "storage_get"
	OperationNetworkAcquire  = "network_acquire"
	OperationPrefetchAcquire = "prefetch_acquire"
	OperationValidation      = "validation"
	OperationAcquireSDBlob   = "acquire_sd_blob"
	OperationParseSDBlob     = "parse_sd_blob"
	OperationValidateSDBlob  = "validate_sd_blob"
	OperationDecryptBlob     = "decrypt_blob"
)

// DefaultRetryOptions returns sensible default retry options using retry-go
func DefaultRetryOptions() []retry.Option {
	return []retry.Option{
		retry.Attempts(3),
		retry.Delay(100 * time.Millisecond),
		retry.MaxDelay(5 * time.Second),
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(isRetryableError),
		retry.LastErrorOnly(true),
	}
}

// isRetryableError determines if an error should be retried
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check if this is a StreamError - unwrap it to check the underlying error
	var streamErr *StreamError
	if errors.As(err, &streamErr) {
		return isRetryableError(streamErr.Unwrap())
	}

	// Don't retry context cancellation
	if errors.Is(err, ErrContextCancelled) {
		return false
	}

	// Don't retry invalid data errors
	if errors.Is(err, stream.ErrInvalidSDBlob) ||
		errors.Is(err, ErrNotSDBlob) ||
		errors.Is(err, ErrStreamCorrupted) ||
		errors.Is(err, ErrInvalidHash) ||
		errors.Is(err, ErrDecryptionFailed) ||
		errors.Is(err, ErrStreamNotFound) {
		return false
	}

	// Retry network and temporary errors
	// For now, retry most errors except specific ones handled above
	return true
}

// RetryableOperation represents an operation that can be retried
type RetryableOperation func() error

// WithRetry executes an operation with retry logic using retry-go library
func WithRetry(ctx context.Context, options []retry.Option, operation RetryableOperation) error {
	if options == nil {
		options = DefaultRetryOptions()
	}
	options = append(options, retry.Context(ctx))
	return retry.Do(retry.RetryableFunc(operation), options...)
}

// StreamError wraps errors with additional context
type StreamError struct {
	Err       error
	Operation string
	SDBlob    string
	BlobHash  string
	Attempt   int
	Timestamp time.Time
}

func (e *StreamError) Error() string {
	if e.BlobHash != "" {
		return fmt.Sprintf("stream error during %s for blob %s in stream %s: %v",
			e.Operation, e.BlobHash, e.SDBlob, e.Err)
	}
	return fmt.Sprintf("stream error during %s for stream %s: %v",
		e.Operation, e.SDBlob, e.Err)
}

func (e *StreamError) Unwrap() error {
	return e.Err
}

// GetTimestamp returns when the error occurred
func (e *StreamError) GetTimestamp() time.Time {
	return e.Timestamp
}

// NewStreamError creates a new StreamError
func NewStreamError(operation, sdBlob, blobHash string, err error, attempt int) *StreamError {
	return &StreamError{
		Err:       err,
		Operation: operation,
		SDBlob:    sdBlob,
		BlobHash:  blobHash,
		Attempt:   attempt,
		Timestamp: time.Now(),
	}
}

// IsStreamError checks if error is a StreamError
func IsStreamError(err error) bool {
	var streamErr *StreamError
	return errors.As(err, &streamErr)
}

// GetStreamError extracts StreamError from wrapped error
func GetStreamError(err error) (*StreamError, bool) {
	var streamErr *StreamError
	if errors.As(err, &streamErr) {
		return streamErr, true
	}
	return nil, false
}

// ValidateBlobHash validates blob hash format using the robust protocol implementation
func ValidateBlobHash(hash string) error {
	if !protocol.ValidateBlobHash(hash) {
		return ErrInvalidHash
	}
	return nil
}
