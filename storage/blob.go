package storage

import "context"

// BlobStore defines the interface for blob storage operations
type BlobStore interface {
	Has(ctx context.Context, hash string) (bool, error)
	Get(ctx context.Context, hash string) ([]byte, error)
	Put(ctx context.Context, hash string, data []byte) error
	PutSD(ctx context.Context, hash string, data []byte) error
	Name() string
	// List returns a paginated list of blob hashes starting at the given offset.
	// The order of hashes is implementation-defined and may vary between calls.
	// If the same hash exists in both regular and SD blob stores, it may appear once or twice depending on the implementation.
	// Returns an empty slice if offset is beyond the available data.
	List(ctx context.Context, offset, limit int) ([]string, error)
	// Delete removes a blob from storage.
	// If the blob exists in both regular and SD blob stores, it removes both.
	// If the blob is not found, it returns nil (no-op).
	// Only returns an error for invalid hash format or other unknown errors.
	Delete(ctx context.Context, hash string) error
}

// Blocklister defines the interface for checking if a blob is wanted
type Blocklister interface {
	// Wants checks if a blob is wanted
	Wants(hash string) (bool, error)
}

// NeededBlobChecker defines the interface for checking which blobs are needed for a known stream.
// This is used for intelligent stream synchronization where a node can determine what
// additional blobs are required to complete a stream without downloading the entire stream.
type NeededBlobChecker interface {
	// MissingBlobsForKnownStream returns a list of blob hashes that are needed
	// to complete a stream identified by the given SD blob hash.
	// This allows nodes to efficiently synchronize only the missing parts of streams
	// rather than downloading entire streams.
	MissingBlobsForKnownStream(streamHash string) ([]string, error)
}
