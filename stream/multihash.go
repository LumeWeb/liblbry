package stream

import (
	"encoding/hex"
	"fmt"

	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
)

const (
	// SHA384MultihashCode is the multihash code for SHA-384 (SHA-2 family)
	SHA384MultihashCode = 0x20
)

// ToMultihash converts an LBRY hash to a CID v1 multihash
func ToMultihash(lbryHash string) (string, error) {
	hasher := NewHasher()
	if !hasher.IsValid(lbryHash) {
		return "", fmt.Errorf("invalid LBRY hash: %s", lbryHash)
	}

	// Decode the hex string to bytes
	hashBytes, err := hex.DecodeString(lbryHash)
	if err != nil {
		return "", fmt.Errorf("failed to decode LBRY hash: %w", err)
	}

	// Create multihash
	mh, err := multihash.Encode(hashBytes, SHA384MultihashCode)
	if err != nil {
		return "", fmt.Errorf("failed to encode multihash: %w", err)
	}

	// Create CID v1 with raw codec
	c := cid.NewCidV1(cid.Raw, mh)

	return c.String(), nil
}

// FromMultihash converts a CID v1 multihash to an LBRY hash
func FromMultihash(multihashStr string) (string, error) {
	// Parse the CID
	c, err := cid.Decode(multihashStr)
	if err != nil {
		return "", fmt.Errorf("failed to decode CID: %w", err)
	}

	// Check CID version and codec
	if c.Version() != 1 {
		return "", fmt.Errorf("unsupported CID version: %d", c.Version())
	}
	if c.Type() != cid.Raw {
		return "", fmt.Errorf("unsupported codec: %d", c.Type())
	}

	// Extract multihash
	mh := c.Hash()

	// Decode the multihash to access its fields
	decoded, err := multihash.Decode(mh)
	if err != nil {
		return "", fmt.Errorf("failed to decode multihash: %w", err)
	}

	// Check if it's SHA-384
	if decoded.Code != SHA384MultihashCode {
		return "", fmt.Errorf("unsupported multihash code: %d", decoded.Code)
	}

	// Convert to LBRY hash (hex string)
	return hex.EncodeToString(decoded.Digest), nil
}

// IsValidMultihash checks if the given string is a valid CID v1 multihash
func IsValidMultihash(hash string) bool {
	_, err := FromMultihash(hash)
	return err == nil
}

// Package-level hasher instance for reuse
var hasher = NewHasher()

// IdentifyHash determines the type of the given hash
func IdentifyHash(hash string) HashType {
	if hasher.IsValid(hash) {
		return HashTypeLBRY
	}

	if IsValidMultihash(hash) {
		return HashTypeMultihash
	}

	return HashTypeUnknown
}

// ValidateHash checks if the given hash is valid according to its type
func ValidateHash(hash string) bool {
	return IdentifyHash(hash) != HashTypeUnknown
}
