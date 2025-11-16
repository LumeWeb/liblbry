package crypto

import (
	"crypto/sha512"
	"encoding/hex"
	"io"
)

const (
	// SHA384HexLength is the length of a SHA-384 hash in hexadecimal (48 bytes * 2)
	SHA384HexLength = 96
)

// Hasher interface defines the methods for hashing and validation
type Hasher interface {
	Hash(data []byte) string
	HashReader(reader io.Reader) (string, error)
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
	return h.encodeHash(hash[:])
}

// HashReader computes the SHA-384 hash of the data read from the reader
func (h *SHA384Hasher) HashReader(reader io.Reader) (string, error) {
	// Create a new SHA-384 hasher
	hasher := sha512.New384()
	
	// Copy data from reader to hasher in chunks to avoid loading everything into memory
	_, err := io.Copy(hasher, reader)
	if err != nil {
		return "", err
	}
	
	// Get the final hash
	hash := hasher.Sum(nil)
	return h.encodeHash(hash), nil
}

// encodeHash converts a byte slice to a hexadecimal string
func (h *SHA384Hasher) encodeHash(hash []byte) string {
	return hex.EncodeToString(hash)
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
