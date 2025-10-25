package crypto

import (
	"crypto/rand"
	"fmt"
)

// KeySize defines the standard key size for encryption operations
const KeySize = 32

// GenerateKey generates a cryptographically secure random key of KeySize bytes
func GenerateKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate key: %w", err)
	}
	return key, nil
}
