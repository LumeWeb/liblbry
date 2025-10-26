package protocol

import "time"

// Common test constants
const (
	testStoreName    = "test"
	testHost         = "127.0.0.1:0"
	testTimeout      = 5 * time.Second
	testBufferSize   = 8192
	shortTestTimeout = 10 * time.Millisecond
)

// IP address constants
const (
	testAllowedIP = "127.0.0.1"
	testDeniedIP  = "192.168.1.1"
	testLocalIP   = "127.0.0.1"
)

// Blob hash constants
const (
	validBlobHash1 = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	validBlobHash2 = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	validBlobHash3 = "123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0"

	invalidBlobHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	shortBlobHash   = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcde"
)

// Response constants
const (
	emptyAvailableBlobsResponse = `{"available_blobs":[]}`
)
