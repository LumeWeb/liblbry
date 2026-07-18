package electrum

import (
	"crypto/sha256"
	"encoding/hex"
)

// Scripthash computes the Electrum scripthash for a scriptPubKey:
// SHA-256 of the script, then reverse the byte order, then hex-encode.
//
// This is the canonical address identifier in Electrum — it's how
// clients subscribe to address activity and look up UTXOs.
//
// Example:
//
//	Scripthash([]byte{0x76, 0xa9, ...}) -> "a7b8288564a83ecc34cdd4be7b24ea9ad28062d9"
func Scripthash(scriptPubKey []byte) string {
	h := sha256.Sum256(scriptPubKey)
	// Reverse bytes
	for i, j := 0, len(h)-1; i < j; i, j = i+1, j-1 {
		h[i], h[j] = h[j], h[i]
	}
	return hex.EncodeToString(h[:])
}
