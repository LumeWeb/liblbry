package stream

import "io"

// StreamReader provides reading and seeking capabilities for streams
type StreamReader interface {
	io.Reader
	io.Seeker
	Size() int64
	Hash() string
}

// StreamReaderFactory defines the interface for creating stream readers
type StreamReaderFactory interface {
	NewStreamReader(path string) (StreamReader, error)
	NewStreamReaderFromReader(r any) (StreamReader, error)
}
