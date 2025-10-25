package crypto

import (
	"crypto/sha512"
	"encoding/hex"
)

const (
	// SHA384HexLength is the length of a SHA-384 hash in hexadecimal (48 bytes * 2)
	SHA384HexLength = 96
)

// Hasher interface defines the methods for hashing and validation
type Hasher interface {
	Hash(data []byte) string
	IsValid(hash string) bool
}

// SHA384Hasher implements the Hasher interface using SHA-384
type SHA384Hasher struct{}

// NewHasher creates a new SHA384Hasher instance
func NewHasher() Hasher {
	return &SHA384Hasher{}
}

// Hash computes the SHA-384 hash of the given data
func (h *SHA384Hasher) Hash(data []byte) string {
	hash := sha512.Sum384(data)
	return hex.EncodeToString(hash[:])
}

// IsValid checks if the given hash is a valid SHA-384 hash
func (h *SHA384Hasher) IsValid(hash string) bool {
	if len(hash) != SHA384HexLength {
		return false
	}

	// Check if all characters are valid lowercase hexadecimal
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}

	return true
}
