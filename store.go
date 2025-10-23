package liblbry

// StoreFactory defines the interface for creating blob storage instances
type StoreFactory interface {
	CreateStore(config map[string]any) (BlobStore, error)
	Name() string
}
