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
	testLocalIP  = "127.0.0.1"
	testDeniedIP = "192.168.1.1"
)

// Blob hash constants (96 characters for SHA-384)
const (
	validBlobHash1 = "e1d67ee4c25586390921909115e78c46dd0b7cbef26f937a4cfeb75d7eb4f61fc6316ffdf9ba6c70c9fff43e8cd9a82d"
	validBlobHash2 = "68c0ff52fca66bc20c736e49967760d6378ce73aaf4b0a870f1c2142455629ab50dc49dae0b03c56a9bff7f270a2edf3"
	validBlobHash3 = "a1f69f0eb06072fb64733834c89d2f4e213ab060ffc57eeb25f696f57c073d2a749be2b48da96504c3dd86694cc7b7d7"

	invalidBlobHash = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	shortBlobHash   = "000000000000000000000000000000000000000000000"
)

// Response constants
const (
	emptyAvailableBlobsResponse = `{"available_blobs":[]}`
)
