package errors

import (
	"errors"
	"fmt"
	"strings"
)

// Common errors
var (
	ErrBlobNotFound      = errors.New("blob not found")
	ErrBlobExists        = errors.New("blob already exists on server")
	ErrInvalidHash       = errors.New("invalid hash")
	ErrInvalidSize       = errors.New("invalid size")
	ErrStreamCorrupted   = errors.New("stream corrupted")
	ErrAccessDenied      = errors.New("access denied")
	ErrInvalidManifest   = errors.New("invalid manifest")
	ErrHashMismatch      = errors.New("hash mismatch")
	ErrConnectionFailed  = errors.New("connection failed")
	ErrTimeout           = errors.New("operation timeout")
	ErrInvalidConfig     = errors.New("invalid configuration")
	ErrRequestTooLarge   = errors.New("request is too large")
	ErrInvalidData       = errors.New("Invalid data")
	ErrInvalidHashLen    = errors.New("Invalid blob hash length")
	ErrBlobProtected     = errors.New("requested blob is protected")
	ErrNoBlobData        = errors.New("no blob data received")
	ErrAlreadyConnected  = errors.New("already connected")
	ErrServerValidation  = errors.New("server validation error")
	ErrAcquisitionFailed = errors.New("failed to acquire blob from all transfer methods")
	ErrInvalidOffset     = errors.New("offset must be non-negative")
	ErrInvalidLimit      = errors.New("limit must be positive")
)

// IsBlobNotFoundError checks if an error represents a blob not found condition
// It checks both for the exact error object and string containment
func IsBlobNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return err == ErrBlobNotFound || errStr == ErrBlobNotFound.Error() || strings.Contains(errStr, "blob not found")
}

// IsKnownErrorType checks if an error is a known protocol error that shouldn't be wrapped
func IsKnownErrorType(err error) bool {
	return err == ErrBlobNotFound ||
		err == ErrBlobExists ||
		err == ErrInvalidHash ||
		err == ErrInvalidSize ||
		err == ErrStreamCorrupted ||
		err == ErrAccessDenied ||
		err == ErrInvalidManifest ||
		err == ErrHashMismatch ||
		err == ErrConnectionFailed ||
		err == ErrTimeout ||
		err == ErrInvalidConfig ||
		err == ErrNoBlobData ||
		err == ErrAlreadyConnected ||
		err == ErrAcquisitionFailed
}

// DetectErrorType detects specific error types by string matching since errors come from different process
func DetectErrorType(errorMsg string) error {
	switch errorMsg {
	case ErrBlobNotFound.Error():
		return ErrBlobNotFound
	case ErrBlobExists.Error():
		return ErrBlobExists
	case ErrAccessDenied.Error():
		return ErrAccessDenied
	case ErrInvalidHash.Error():
		return ErrInvalidHash
	case ErrInvalidHashLen.Error():
		return ErrInvalidHashLen
	case ErrBlobProtected.Error():
		return ErrBlobProtected
	case ErrAcquisitionFailed.Error():
		return ErrAcquisitionFailed
	default:
		// For unknown errors, create a generic error
		return fmt.Errorf("%s", errorMsg)
	}
}

// Stream-specific errors (added from lbry.go)
var (
	ErrBlobTooBig             = errors.New("blob must be at most 2097152 bytes")
	ErrBlobEmpty              = errors.New("blob is empty")
	ErrInvalidIV              = errors.New("IV length must equal to block size")
	ErrInvalidBlockLength     = errors.New("invalid block length")
	ErrInvalidDataLength      = errors.New("invalid data length")
	ErrInvalidPadding         = errors.New("invalid padding")
	ErrStreamTooShort         = errors.New("stream must be at least 2 blobs long")
	ErrInvalidSDBlob          = errors.New("sd blob is not valid")
	ErrMissingTerminatingBlob = errors.New("sd blob is missing the terminating 0-length blob")
	ErrBlobCountMismatch      = errors.New("number of blobs in stream does not match number of blobs in sd info")
	ErrUnexpectedEmptyBlob    = errors.New("got 0-length blob before end of stream")
	ErrBlobsOutOfOrder        = errors.New("blobs are out of order in sd blob")
)
