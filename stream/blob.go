package stream

// Adapted from https://github.com/lbryio/lbry.go

import "crypto/sha512"

const (
	MaxBlobSize       = 2097152 // 2mb, or 2 * 2^20
	BlobHashSize      = sha512.Size384
	BlobHashHexLength = BlobHashSize * 2 // in hex, each byte is 2 chars
)

// Blob represents a data blob with encryption capabilities
type Blob []byte
