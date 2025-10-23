package stream

// HashType represents different hash formats
type HashType int

const (
	HashTypeUnknown HashType = iota
	HashTypeLBRY
	HashTypeMultihash
)
