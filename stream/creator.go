package stream

import (
	"io"
	"io/fs"
)

// StreamCreator defines the interface for creating streams from data sources
type StreamCreator interface {
	CreateStream(source io.Reader, size int64, opts ...StreamOption) (*StreamResult, error)
	CreateStreamFromFile(fsys fs.FS, path string, opts ...StreamOption) (*StreamResult, error)
	CreateStreamFromPath(path string, opts ...StreamOption) (*StreamResult, error)
}
