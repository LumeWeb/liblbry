package stream

import "io"

// ManifestCreator defines the interface for creating and parsing SD blob manifests
type ManifestCreator interface {
	CreateManifest(source io.Reader, size int64) (*SDBlob, []byte, error)
	CreateManifestFromPath(path string) (*SDBlob, []byte, error)
	ParseManifest(data []byte) (*SDBlob, error)
}
