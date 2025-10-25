package stream

import (
	"crypto/sha512"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/liblbry/blob"
	lbrycrypto "go.lumeweb.com/liblbry/crypto"
)

func TestSHA384Hasher_Hash(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty data",
			input:    KnownHashVectors[TestKeyEmpty].input,
			expected: KnownHashVectors[TestKeyEmpty].expected,
		},
		{
			name:     "hello world",
			input:    KnownHashVectors[TestKeyHelloWorld].input,
			expected: KnownHashVectors[TestKeyHelloWorld].expected,
		},
		{
			name:     "lbry",
			input:    KnownHashVectors[TestKeyLbry].input,
			expected: KnownHashVectors[TestKeyLbry].expected,
		},
		{
			name:     "test data for hashing",
			input:    KnownHashVectors[TestKeyTestData].input,
			expected: KnownHashVectors[TestKeyTestData].expected,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hasher.Hash([]byte(tc.input))
			assert.Equal(t, tc.expected, result)

			// Verify with standard library implementation
			stdHash := sha512.Sum384([]byte(tc.input))
			stdResult := hex.EncodeToString(stdHash[:])
			assert.Equal(t, stdResult, result, "Should match standard library implementation")
		})
	}
}

func TestSHA384Hasher_IsValid(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		// Valid hashes
		{
			name:     "valid empty string hash",
			hash:     ValidLBRYHashes[ValidHashKeyEmptyString],
			expected: true,
		},
		{
			name:     "valid hello world hash",
			hash:     ValidLBRYHashes[ValidHashKeyHelloWorld],
			expected: true,
		},
		{
			name:     "valid lbry hash",
			hash:     ValidLBRYHashes[ValidHashKeyLbry],
			expected: true,
		},
		{
			name:     "valid LBRY blob hash",
			hash:     ValidLBRYHashes[ValidHashKeyBlob],
			expected: true,
		},
		{
			name:     "valid LBRY stream hash",
			hash:     ValidLBRYHashes[ValidHashKeyStream],
			expected: true,
		},
		{
			name:     "valid LBRY blob info hash",
			hash:     ValidLBRYHashes[ValidHashKeyBlobInfo],
			expected: true,
		},
		{
			name:     "valid LBRY null stream hash",
			hash:     ValidLBRYHashes[ValidHashKeyNullStream],
			expected: true,
		},
		{
			name:     "valid all a's",
			hash:     strings.Repeat("a", 96),
			expected: true,
		},
		{
			name:     "valid all 0's",
			hash:     strings.Repeat("0", 96),
			expected: true,
		},
		// Invalid lengths
		{
			name:     "invalid empty",
			hash:     InvalidHashLengths[InvalidLengthKeyEmpty],
			expected: false,
		},
		{
			name:     "invalid too short",
			hash:     InvalidHashLengths[InvalidLengthKeyTooShort],
			expected: false,
		},
		{
			name:     "invalid wrong length",
			hash:     InvalidHashLengths[InvalidLengthKeyWrongLength],
			expected: false,
		},
		{
			name:     "invalid one char short",
			hash:     strings.Repeat("a", 95),
			expected: false,
		},
		{
			name:     "invalid one char long",
			hash:     strings.Repeat("a", 97),
			expected: false,
		},
		{
			name:     "invalid much too long",
			hash:     strings.Repeat("a", 100),
			expected: false,
		},
		// Invalid hex characters
		{
			name:     "invalid non-hex g",
			hash:     InvalidHashHex[InvalidHexKeyInvalidG],
			expected: false,
		},
		{
			name:     "invalid non-hex z",
			hash:     InvalidHashHex[InvalidHexKeyInvalidZ],
			expected: false,
		},
		{
			name:     "invalid mixed hex",
			hash:     InvalidHashHex[InvalidHexKeyMixed],
			expected: false,
		},
		// Uppercase characters
		{
			name:     "invalid all uppercase",
			hash:     InvalidHashUppercase[InvalidUppercaseKeyAllUppercase],
			expected: false,
		},
		{
			name:     "invalid mixed case",
			hash:     InvalidHashUppercase[InvalidUppercaseKeyMixedCase],
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasher.IsValid(tt.hash)
			assert.Equal(t, tt.expected, result, "Hash: %s", tt.hash)
		})
	}
}

func TestSHA384Hasher_HashAndValidateConsistency(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name string
		data string
	}{
		{"empty", TestDataForHashing[0]},
		{"hello", TestDataForHashing[1]},
		{"hello world", TestDataForHashing[2]},
		{"lbry", TestDataForHashing[3]},
		{"test data for hashing", TestDataForHashing[4]},
		{"long string", TestDataForHashing[5]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash := hasher.Hash([]byte(tt.data))
			assert.True(t, hasher.IsValid(hash), "Generated hash should be valid: %s", hash)
		})
	}
}

func TestBlobHashConstants(t *testing.T) {
	tests := []struct {
		name     string
		expected int
		actual   int
	}{
		{"SHA-384 byte size", 48, blob.BlobHashSize},
		{"SHA-384 hex length", 96, blob.BlobHashHexLength},
		{"hex length double byte size", blob.BlobHashSize * 2, blob.BlobHashHexLength},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.actual)
		})
	}
}

func TestHasherInterfaceCompatibility(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "generated hash length",
			testFunc: func(t *testing.T) {
				testHash := hasher.Hash([]byte("test"))
				assert.Equal(t, blob.BlobHashHexLength, len(testHash), "Generated hash should match expected length")
			},
		},
		{
			name: "generated hash validity",
			testFunc: func(t *testing.T) {
				testHash := hasher.Hash([]byte("test"))
				assert.True(t, hasher.IsValid(testHash), "Generated hash should be valid")
			},
		},
		{
			name: "truncated hash invalid",
			testFunc: func(t *testing.T) {
				testHash := hasher.Hash([]byte("test"))
				assert.False(t, hasher.IsValid(testHash[:95]), "Truncated hash should be invalid")
			},
		},
		{
			name: "extended hash invalid",
			testFunc: func(t *testing.T) {
				testHash := hasher.Hash([]byte("test"))
				assert.False(t, hasher.IsValid(testHash+"a"), "Extended hash should be invalid")
			},
		},
		{
			name: "LBRY blob hash valid",
			testFunc: func(t *testing.T) {
				assert.True(t, hasher.IsValid(LBRYTestHashes[LBRYHashKey1]), "LBRY blob hash should be valid")
			},
		},
		{
			name: "LBRY stream hash valid",
			testFunc: func(t *testing.T) {
				assert.True(t, hasher.IsValid("d756e860d8f49d03937c1a35a560636e792d97bee6f9660fc69e206cbcfe7f9297c5ede8428dd5f17d240e434eb557da"), "LBRY stream hash should be valid")
			},
		},
		{
			name: "LBRY blob info hash valid",
			testFunc: func(t *testing.T) {
				assert.True(t, hasher.IsValid("2c8cb2893668ef3ad30bda5b3361c0736d746d82fb16155d1510c4d2c5e4481d49ee747f155b2f1156849d422f13a7be"), "LBRY blob info hash should be valid")
			},
		},
		{
			name: "LBRY null stream hash valid",
			testFunc: func(t *testing.T) {
				assert.True(t, hasher.IsValid("4d9a9ce3d72af9f171c4233738e08440937cf906eb506a5d573c0e5500c58500b0a6cbaedc9be2c863750859c01d9954"), "LBRY null stream hash should be valid")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func TestReferenceImplementationCompatibility(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name string
		data string
	}{
		{"empty", TestDataForHashing[0]},
		{"hello world", TestDataForHashing[2]},
		{"lbry", TestDataForHashing[3]},
		{"test data", TestDataForHashing[4]},
		{"lbry test data", TestDataForHashing[4]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Our implementation
			ourHash := hasher.Hash([]byte(tt.data))

			// Standard library implementation
			stdHash := sha512.Sum384([]byte(tt.data))
			stdHashStr := hex.EncodeToString(stdHash[:])

			// They should be identical
			assert.Equal(t, stdHashStr, ourHash, "Should match standard library implementation")

			// Both should be valid
			assert.True(t, hasher.IsValid(ourHash), "Our hash should be valid")
			assert.True(t, hasher.IsValid(stdHashStr), "Standard library hash should be valid")
		})
	}
}

func TestEdgeCases(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name     string
		testFunc func(t *testing.T)
	}{
		{
			name: "binary data",
			testFunc: func(t *testing.T) {
				binaryData := []byte{0, 1, 2, 3, 4, 5, 255, 254, 253}
				hash := hasher.Hash(binaryData)
				assert.True(t, hasher.IsValid(hash), "Should handle binary data correctly")
			},
		},
		{
			name: "unicode data",
			testFunc: func(t *testing.T) {
				unicodeData := "Hello, 世界! 🌍"
				hash := hasher.Hash([]byte(unicodeData))
				assert.True(t, hasher.IsValid(hash), "Should handle unicode data correctly")
			},
		},
		{
			name: "large data",
			testFunc: func(t *testing.T) {
				largeData := make([]byte, 1024)
				for i := range largeData {
					largeData[i] = byte(i % 256)
				}
				hash := hasher.Hash(largeData)
				assert.True(t, hasher.IsValid(hash), "Should handle large data correctly")
				assert.Equal(t, 96, len(hash), "Hash should be exactly 96 characters")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.testFunc)
	}
}

func TestHashValidationBoundaryConditions(t *testing.T) {
	hasher := lbrycrypto.NewHasher()

	tests := []struct {
		name     string
		hash     string
		expected bool
	}{
		{
			name:     "invalid non-hex 96 chars",
			hash:     strings.Repeat("g", 96),
			expected: false,
		},
		{
			name:     "invalid mixed case",
			hash:     "A" + strings.Repeat("a", 95),
			expected: false,
		},
		{
			name:     "valid 96 lowercase hex",
			hash:     strings.Repeat("a", 96),
			expected: true,
		},
		{
			name:     "invalid 95 chars",
			hash:     strings.Repeat("a", 95),
			expected: false,
		},
		{
			name:     "valid 96 chars",
			hash:     strings.Repeat("a", 96),
			expected: true,
		},
		{
			name:     "invalid 97 chars",
			hash:     strings.Repeat("a", 97),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasher.IsValid(tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}
