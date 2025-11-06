package storage

// BlobStore defines the interface for blob storage operations
type BlobStore interface {
	Has(hash string) (bool, error)
	Get(hash string) ([]byte, error)
	Put(hash string, data []byte) error
	PutSD(hash string, data []byte) error
	Name() string
	// List returns a paginated list of blob hashes starting at the given offset.
	// The order of hashes is implementation-defined and may vary between calls.
	// If the same hash exists in both regular and SD blob stores, it may appear once or twice depending on the implementation.
	// Returns an empty slice if offset is beyond the available data.
	List(offset, limit int) ([]string, error)
}
