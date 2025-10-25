package stream

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multibase"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/blob"
	lbrycrypto "go.lumeweb.com/liblbry/crypto"
)

func TestHashIdentificationCompatibility(t *testing.T) {
	// Generate multihashes for LBRY test hashes
	multihashStrings, err := GenerateMultihashesForLBRYHashes()
	require.NoError(t, err)

	tests := []struct {
		name     string
		hash     string
		expected HashType
	}{
		// LBRY hashes
		{"LBRY hash 1", LBRYTestHashes[LBRYHashKey1], HashTypeLBRY},
		{"LBRY hash 2", LBRYTestHashes[LBRYHashKey2], HashTypeLBRY},
		{"LBRY hash 3", LBRYTestHashes[LBRYHashKey3], HashTypeLBRY},
		{"LBRY hash 4", LBRYTestHashes[LBRYHashKey4], HashTypeLBRY},
		{"LBRY hash 5", LBRYTestHashes[LBRYHashKey5], HashTypeLBRY},
		{"LBRY hash 6", LBRYTestHashes[LBRYHashKey6], HashTypeLBRY},
		// Multihashes
		{"multihash 1", multihashStrings[0], HashTypeMultihash},
		{"multihash 2", multihashStrings[1], HashTypeMultihash},
		{"multihash 3", multihashStrings[2], HashTypeMultihash},
		{"multihash 4", multihashStrings[3], HashTypeMultihash},
		{"multihash 5", multihashStrings[4], HashTypeMultihash},
		{"multihash 6", multihashStrings[5], HashTypeMultihash},
		// Invalid hashes
		{"invalid too short", "abc123", HashTypeUnknown},
		{"invalid random", "thisisnotahashatall", HashTypeUnknown},
		{"invalid empty", "", HashTypeUnknown},
		{"invalid wrong length", strings.Repeat("a", blob.BlobHashHexLength-1), HashTypeUnknown},
		{"invalid uppercase", strings.ToUpper(LBRYTestHashes[LBRYHashKey1]), HashTypeUnknown},
		{"invalid non-hex", LBRYTestHashes[LBRYHashKey1][:blob.BlobHashHexLength-1] + "g", HashTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IdentifyHash(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHashValidationCompatibility(t *testing.T) {
	// Generate multihashes for LBRY test hashes
	multihashStrings, err := GenerateMultihashesForLBRYHashes()
	require.NoError(t, err)

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		// Valid LBRY hashes
		{"valid LBRY 1", LBRYTestHashes[LBRYHashKey1], true},
		{"valid LBRY 2", LBRYTestHashes[LBRYHashKey2], true},
		{"valid LBRY 3", LBRYTestHashes[LBRYHashKey3], true},
		{"valid LBRY 4", LBRYTestHashes[LBRYHashKey4], true},
		{"valid LBRY 5", LBRYTestHashes[LBRYHashKey5], true},
		{"valid LBRY 6", LBRYTestHashes[LBRYHashKey6], true},
		// Valid multihashes
		{"valid multihash 1", multihashStrings[0], true},
		{"valid multihash 2", multihashStrings[1], true},
		{"valid multihash 3", multihashStrings[2], true},
		{"valid multihash 4", multihashStrings[3], true},
		{"valid multihash 5", multihashStrings[4], true},
		{"valid multihash 6", multihashStrings[5], true},
		// Invalid hashes
		{"invalid too short", "abc123", false},
		{"invalid random", "thisisnotahashatall", false},
		{"invalid empty", "", false},
		{"invalid wrong length", strings.Repeat("a", blob.BlobHashHexLength-1), false},
		{"invalid uppercase", strings.ToUpper(LBRYTestHashes[LBRYHashKey1]), false},
		{"invalid non-hex", LBRYTestHashes[LBRYHashKey1][:blob.BlobHashHexLength-1] + "g", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateHash(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMultihashConversionCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		lbryHash string
	}{
		{"conversion 1", LBRYTestHashes[LBRYHashKey1]},
		{"conversion 2", LBRYTestHashes[LBRYHashKey2]},
		{"conversion 3", LBRYTestHashes[LBRYHashKey3]},
		{"conversion 4", LBRYTestHashes[LBRYHashKey4]},
		{"conversion 5", LBRYTestHashes[LBRYHashKey5]},
		{"conversion 6", LBRYTestHashes[LBRYHashKey6]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Decode the hex to get the actual bytes
			lbryBytes, err := hex.DecodeString(tt.lbryHash)
			require.NoError(t, err)

			// Create expected multihash manually
			mh, err := multihash.Encode(lbryBytes, SHA384MultihashCode)
			require.NoError(t, err)

			expectedCID := cid.NewCidV1(cid.Raw, mh)
			expectedMultihash := expectedCID.String()

			result, err := ToMultihash(tt.lbryHash)
			assert.NoError(t, err)
			assert.Equal(t, expectedMultihash, result)
		})
	}

	// Test error conditions
	errorTests := []struct {
		name     string
		lbryHash string
	}{
		{"invalid too short", "abc123"},
		{"invalid non-hex", "a2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6g"},
		{"invalid wrong length", strings.Repeat("a", blob.BlobHashHexLength-1)},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ToMultihash(tt.lbryHash)
			assert.Error(t, err)
		})
	}
}

func TestMultihashDecodingCompatibility(t *testing.T) {
	tests := []struct {
		name             string
		expectedLBRYHash string
	}{
		{"decoding 1", LBRYTestHashes[LBRYHashKey1]},
		{"decoding 2", LBRYTestHashes[LBRYHashKey2]},
		{"decoding 3", LBRYTestHashes[LBRYHashKey3]},
		{"decoding 4", LBRYTestHashes[LBRYHashKey4]},
		{"decoding 5", LBRYTestHashes[LBRYHashKey5]},
		{"decoding 6", LBRYTestHashes[LBRYHashKey6]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a valid multihash
			lbryBytes, err := hex.DecodeString(tt.expectedLBRYHash)
			require.NoError(t, err)

			mh, err := multihash.Encode(lbryBytes, SHA384MultihashCode)
			require.NoError(t, err)

			validMultihash := cid.NewCidV1(cid.Raw, mh).String()

			result, err := FromMultihash(validMultihash)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedLBRYHash, result)
		})
	}

	// Test error conditions
	errorTests := []struct {
		name          string
		setupFunc     func(t *testing.T) string
		errorContains string
	}{
		{
			name: "invalid CID",
			setupFunc: func(t *testing.T) string {
				return "invalidcid"
			},
			errorContains: "failed to decode CID",
		},
		{
			name: "unsupported codec",
			setupFunc: func(t *testing.T) string {
				hashBytes := make([]byte, 48)
				_, err := rand.Read(hashBytes)
				require.NoError(t, err)

				mh, err := multihash.Encode(hashBytes, SHA384MultihashCode)
				require.NoError(t, err)

				c := cid.NewCidV1(cid.DagCBOR, mh) // Using DagCBOR instead of Raw
				return c.String()
			},
			errorContains: "unsupported codec",
		},
		{
			name: "unsupported multihash code",
			setupFunc: func(t *testing.T) string {
				hashBytes := make([]byte, 32) // SHA-256 produces 32 bytes
				_, err := rand.Read(hashBytes)
				require.NoError(t, err)

				mh, err := multihash.Encode(hashBytes, multihash.SHA2_256)
				require.NoError(t, err)

				c := cid.NewCidV1(cid.Raw, mh)
				return c.String()
			},
			errorContains: "unsupported multihash code",
		},
	}

	for _, tt := range errorTests {
		t.Run(tt.name, func(t *testing.T) {
			multihashStr := tt.setupFunc(t)
			_, err := FromMultihash(multihashStr)
			assert.Error(t, err)
			if tt.errorContains != "" {
				assert.Contains(t, err.Error(), tt.errorContains)
			}
		})
	}
}

func TestMultihashValidationCompatibility(t *testing.T) {
	// Generate valid multihashes for LBRY test hashes
	validMultihashes, err := GenerateMultihashesForLBRYHashes()
	require.NoError(t, err)

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		// Valid multihashes
		{"valid multihash 1", validMultihashes[0], true},
		{"valid multihash 2", validMultihashes[1], true},
		{"valid multihash 3", validMultihashes[2], true},
		{"valid multihash 4", validMultihashes[3], true},
		{"valid multihash 5", validMultihashes[4], true},
		{"valid multihash 6", validMultihashes[5], true},
		// Invalid multihashes
		{"invalid CID", "invalidcid", false},
		{"empty string", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidMultihash(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasherCompatibility(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		// Valid hashes
		{"valid hash 1", LBRYTestHashes[LBRYHashKey1], true},
		{"valid hash 2", LBRYTestHashes[LBRYHashKey2], true},
		{"valid hash 3", LBRYTestHashes[LBRYHashKey3], true},
		{"valid hash 4", LBRYTestHashes[LBRYHashKey4], true},
		{"valid hash 5", LBRYTestHashes[LBRYHashKey5], true},
		{"valid hash 6", LBRYTestHashes[LBRYHashKey6], true},
		// Invalid hashes
		{"invalid wrong length", strings.Repeat("a", blob.BlobHashHexLength-1), false},
		{"invalid uppercase", strings.ToUpper(LBRYTestHashes[LBRYHashKey1]), false},
		{"invalid non-hex", LBRYTestHashes[LBRYHashKey1][:blob.BlobHashHexLength-1] + "g", false},
		{"invalid empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasher.IsValid(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCIDFormatCompatibility(t *testing.T) {
	lbryTestHash := LBRYTestHashes[LBRYHashKey1]

	// Create a multihash
	lbryBytes, err := hex.DecodeString(lbryTestHash)
	require.NoError(t, err)

	mh, err := multihash.Encode(lbryBytes, SHA384MultihashCode)
	require.NoError(t, err)

	c := cid.NewCidV1(cid.Raw, mh)

	// Test different base encodings
	base32CID, err := c.StringOfBase(multibase.Base32)
	require.NoError(t, err)
	base64CID, err := c.StringOfBase(multibase.Base64)
	require.NoError(t, err)

	tests := []struct {
		name     string
		hash     string
		expected HashType
	}{
		{"base32 format", base32CID, HashTypeMultihash},
		{"base64 format", base64CID, HashTypeMultihash},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Should be identifiable as multihashes
			assert.Equal(t, tt.expected, IdentifyHash(tt.hash))

			// Should be valid
			assert.True(t, ValidateHash(tt.hash))

			// Should convert back to the same LBRY hash
			result, err := FromMultihash(tt.hash)
			assert.NoError(t, err)
			assert.Equal(t, lbryTestHash, result)
		})
	}
}

func TestCompleteRoundTripCompatibility(t *testing.T) {
	tests := []struct {
		name     string
		lbryHash string
	}{
		{"roundtrip 1", LBRYTestHashes[LBRYHashKey1]},
		{"roundtrip 2", LBRYTestHashes[LBRYHashKey2]},
		{"roundtrip 3", LBRYTestHashes[LBRYHashKey3]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Convert to multihash
			multihashStr, err := ToMultihash(tt.lbryHash)
			assert.NoError(t, err)

			// Verify it's identified as a multihash
			assert.Equal(t, HashTypeMultihash, IdentifyHash(multihashStr))

			// Verify it's valid
			assert.True(t, ValidateHash(multihashStr))

			// Convert back to LBRY hash
			returnedLBRYHash, err := FromMultihash(multihashStr)
			assert.NoError(t, err)

			// Should be identical to original
			assert.Equal(t, tt.lbryHash, returnedLBRYHash)

			// Original should be identified as LBRY hash
			assert.Equal(t, HashTypeLBRY, IdentifyHash(tt.lbryHash))

			// Original should be valid
			assert.True(t, ValidateHash(tt.lbryHash))
		})
	}
}

func TestHashBoundaryConditionsCompatibility(t *testing.T) {
	tests := []struct {
		name          string
		hash          string
		expectedType  HashType
		expectedValid bool
	}{
		{
			name:          "all zeros",
			hash:          strings.Repeat("0", blob.BlobHashHexLength),
			expectedType:  HashTypeLBRY,
			expectedValid: true,
		},
		{
			name:          "all f's",
			hash:          strings.Repeat("f", blob.BlobHashHexLength),
			expectedType:  HashTypeLBRY,
			expectedValid: true,
		},
		{
			name:          "one char short",
			hash:          strings.Repeat("a", blob.BlobHashHexLength-1),
			expectedType:  HashTypeUnknown,
			expectedValid: false,
		},
		{
			name:          "one char long",
			hash:          strings.Repeat("a", blob.BlobHashHexLength+1),
			expectedType:  HashTypeUnknown,
			expectedValid: false,
		},
		{
			name:          "real LBRY hash",
			hash:          LBRYTestHashes[LBRYHashKey1],
			expectedType:  HashTypeLBRY,
			expectedValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test identification
			hashType := IdentifyHash(tt.hash)
			assert.Equal(t, tt.expectedType, hashType)

			// Test validation
			isValid := ValidateHash(tt.hash)
			assert.Equal(t, tt.expectedValid, isValid)
		})
	}
}
