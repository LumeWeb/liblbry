package liblbry

// BlobStore defines the interface for blob storage operations
type BlobStore interface {
	Has(hash string) (bool, error)
	Get(hash string) ([]byte, error)
	Put(hash string, data []byte) error
	PutSD(hash string, data []byte) error
	Name() string
}

// BlobTransfer defines the interface for blob acquisition/transfer
type BlobTransfer interface {
	Get(hash string) ([]byte, error)
	Name() string
}
