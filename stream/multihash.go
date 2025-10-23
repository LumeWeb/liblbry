package stream

// MultihashConverter defines the interface for CID v1 multihash conversion
type MultihashConverter interface {
	ToMultihash(lbryHash string) (string, error)
	FromMultihash(multihash string) (string, error)
	IsValidMultihash(hash string) bool
}

// HashIdentifier defines the interface for determining hash type and validation
type HashIdentifier interface {
	Identify(hash string) HashType
	Validate(hash string) bool
}
