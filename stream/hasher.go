package stream

// Hasher defines the interface for LBRY native SHA-384 hashing
type Hasher interface {
	Hash(data []byte) string
	IsValid(hash string) bool
}
