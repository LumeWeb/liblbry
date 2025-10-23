package stream

import (
	"encoding/hex"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToMultihash(t *testing.T) {
	tests := []struct {
		name        string
		lbryHash    string
		expectError bool
		errorMsg    string
	}{
		// Valid LBRY hashes
		{
			name:        "valid LBRY hash 1",
			lbryHash:    LBRYTestHashes[LBRYHashKey1],
			expectError: false,
		},
		{
			name:        "valid LBRY hash 2",
			lbryHash:    LBRYTestHashes[LBRYHashKey2],
			expectError: false,
		},
		{
			name:        "valid LBRY hash 3",
			lbryHash:    LBRYTestHashes[LBRYHashKey3],
			expectError: false,
		},
		{
			name:        "valid LBRY hash 4",
			lbryHash:    LBRYTestHashes[LBRYHashKey4],
			expectError: false,
		},
		// Invalid LBRY hashes
		{
			name:        "invalid wrong length",
			lbryHash:    InvalidHashLengths[InvalidLengthKeyWrongLength],
			expectError: true,
		},
		{
			name:        "invalid non-hex characters",
			lbryHash:    InvalidHashHex[InvalidHexKeyInvalidG],
			expectError: true,
		},
		{
			name:        "invalid uppercase characters",
			lbryHash:    InvalidHashUppercase[InvalidUppercaseKeyAllUppercase],
			expectError: true,
		},
		{
			name:        "invalid empty",
			lbryHash:    InvalidHashLengths[InvalidLengthKeyEmpty],
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			multihashStr, err := ToMultihash(tt.lbryHash)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, multihashStr)

				// Verify it's a valid CID
				assert.True(t, IsValidMultihash(multihashStr))

				// Verify round-trip conversion works
				back, err := FromMultihash(multihashStr)
				require.NoError(t, err)
				assert.Equal(t, tt.lbryHash, back)
			}
		})
	}
}

func TestFromMultihash(t *testing.T) {
	tests := []struct {
		name          string
		multihashStr  string
		expected      string
		expectError   bool
		errorContains string
	}{
		// Valid multihashes (converted from LBRY hashes)
		{
			name:         "valid multihash 1",
			multihashStr: generateMultihashForTest(t, LBRYTestHashes[LBRYHashKey1]),
			expected:     LBRYTestHashes[LBRYHashKey1],
			expectError:  false,
		},
		{
			name:         "valid multihash 2",
			multihashStr: generateMultihashForTest(t, LBRYTestHashes[LBRYHashKey2]),
			expected:     LBRYTestHashes[LBRYHashKey2],
			expectError:  false,
		},
		// Invalid multihashes
		{
			name:          "invalid CID",
			multihashStr:  "invalidcid",
			expectError:   true,
			errorContains: "failed to decode CID",
		},
		{
			name:          "unsupported CID version",
			multihashStr:  "QmPswobA38Q7pfUo8M7dDQDnzydz73e13Vp77jGw2mR7pi",
			expectError:   true,
			errorContains: "unsupported CID version",
		},
		{
			name:          "empty multihash",
			multihashStr:  "",
			expectError:   true,
			errorContains: "failed to decode CID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := FromMultihash(tt.multihashStr)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestMultihashRoundTripConversion(t *testing.T) {
	tests := []struct {
		name     string
		lbryHash string
	}{
		{"roundtrip 1", LBRYTestHashes[LBRYHashKey1]},
		{"roundtrip 2", LBRYTestHashes[LBRYHashKey2]},
		{"roundtrip 3", LBRYTestHashes[LBRYHashKey3]},
		{"roundtrip 4", LBRYTestHashes[LBRYHashKey4]},
		{"roundtrip 5", LBRYTestHashes[LBRYHashKey5]},
		{"roundtrip 6", LBRYTestHashes[LBRYHashKey6]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// LBRY -> Multihash
			multihashStr, err := ToMultihash(tt.lbryHash)
			require.NoError(t, err)
			assert.NotEmpty(t, multihashStr)

			// Multihash -> LBRY
			back, err := FromMultihash(multihashStr)
			require.NoError(t, err)
			assert.Equal(t, tt.lbryHash, back)

			// Verify it's a valid CID
			assert.True(t, IsValidMultihash(multihashStr))
		})
	}
}

func TestMultihashValidation(t *testing.T) {
	// Generate valid multihashes from LBRY test hashes
	lbryHashKeys := GetLBRYTestHashKeys()
	validMultihashes := make([]string, 0, len(lbryHashKeys))
	for _, key := range lbryHashKeys[:2] { // Use first 2 for efficiency
		lbryHash := LBRYTestHashes[key]
		multihashStr, err := ToMultihash(lbryHash)
		require.NoError(t, err)
		validMultihashes = append(validMultihashes, multihashStr)
	}

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		// Valid multihashes
		{
			name:     "valid multihash 1",
			hash:     validMultihashes[0],
			expected: true,
		},
		{
			name:     "valid multihash 2",
			hash:     validMultihashes[1],
			expected: true,
		},
		// Invalid multihashes
		{
			name:     "invalid string",
			hash:     InvalidMultihashes[InvalidMultihashKeyInvalid],
			expected: false,
		},
		{
			name:     "empty string",
			hash:     InvalidMultihashes[InvalidMultihashKeyEmpty],
			expected: false,
		},
		{
			name:     "CIDv0 incompatible",
			hash:     InvalidMultihashes[InvalidMultihashKeyCIDv0],
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidMultihash(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMultihashEncodingConsistency(t *testing.T) {
	tests := []struct {
		name     string
		lbryHash string
	}{
		{"consistency 1", LBRYTestHashes[LBRYHashKey1]},
		{"consistency 2", LBRYTestHashes[LBRYHashKey2]},
		{"consistency 3", LBRYTestHashes[LBRYHashKey3]},
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

			// Our conversion
			actualMultihash, err := ToMultihash(tt.lbryHash)
			require.NoError(t, err)

			// They should be identical
			assert.Equal(t, expectedMultihash, actualMultihash)
		})
	}
}

func TestMultihashErrorConditions(t *testing.T) {
	tests := []struct {
		name          string
		setupFunc     func(t *testing.T) string
		expectError   bool
		errorContains string
	}{
		{
			name: "unsupported codec",
			setupFunc: func(t *testing.T) string {
				hashBytes := make([]byte, 48)
				for i := range hashBytes {
					hashBytes[i] = byte(i)
				}

				mh, err := multihash.Encode(hashBytes, SHA384MultihashCode)
				require.NoError(t, err)

				c := cid.NewCidV1(cid.DagCBOR, mh) // Using DagCBOR instead of Raw
				return c.String()
			},
			expectError:   true,
			errorContains: "unsupported codec",
		},
		{
			name: "unsupported multihash code",
			setupFunc: func(t *testing.T) string {
				hashBytes := make([]byte, 32) // SHA-256 produces 32 bytes
				for i := range hashBytes {
					hashBytes[i] = byte(i)
				}

				mh, err := multihash.Encode(hashBytes, multihash.SHA2_256)
				require.NoError(t, err)

				c := cid.NewCidV1(cid.Raw, mh)
				return c.String()
			},
			expectError:   true,
			errorContains: "unsupported multihash code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			multihashStr := tt.setupFunc(t)
			_, err := FromMultihash(multihashStr)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Helper function to generate multihash for testing
func generateMultihashForTest(t *testing.T, lbryHash string) string {
	multihashStr, err := ToMultihash(lbryHash)
	if err != nil {
		t.Fatalf("Failed to generate multihash for test: %v", err)
	}
	return multihashStr
}
