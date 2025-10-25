package errors

import "errors"

// Common errors
var (
	ErrBlobNotFound     = errors.New("blob not found")
	ErrInvalidHash      = errors.New("invalid hash")
	ErrInvalidSize      = errors.New("invalid size")
	ErrStreamCorrupted  = errors.New("stream corrupted")
	ErrAccessDenied     = errors.New("access denied")
	ErrInvalidManifest  = errors.New("invalid manifest")
	ErrHashMismatch     = errors.New("hash mismatch")
	ErrConnectionFailed = errors.New("connection failed")
	ErrTimeout          = errors.New("operation timeout")
	ErrInvalidConfig    = errors.New("invalid configuration")
)

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
