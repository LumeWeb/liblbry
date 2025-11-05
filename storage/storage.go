package storage

import (
	"github.com/knadh/koanf/v2"
)

// StoreFactory defines the interface for creating blob storage instances
type StoreFactory interface {
	CreateStore(config *koanf.Koanf) (BlobStore, error)
	Name() string
}
