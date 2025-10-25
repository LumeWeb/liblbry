package stream

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

// StreamCreator defines the interface for creating streams from data sources
type StreamCreator interface {
	CreateStream(source io.Reader, size int64, opts ...StreamOption) (*StreamResult, error)
	CreateStreamFromFile(fsys fs.FS, path string, opts ...StreamOption) (*StreamResult, error)
	CreateStreamFromPath(path string, opts ...StreamOption) (*StreamResult, error)
}

// DefaultStreamCreator implements StreamCreator interface
type DefaultStreamCreator struct {
	manifestCreator ManifestCreator
}

// NewStreamCreator creates a new DefaultStreamCreator instance
func NewStreamCreator(manifestCreator ManifestCreator) *DefaultStreamCreator {
	return &DefaultStreamCreator{
		manifestCreator: manifestCreator,
	}
}

// CreateStream creates a stream from an io.Reader
func (sc *DefaultStreamCreator) CreateStream(source io.Reader, size int64, opts ...StreamOption) (*StreamResult, error) {
	// Validate input
	if source == nil {
		return nil, liblbryerrors.Err("source reader cannot be nil")
	}

	// Apply options to config
	config := &StreamConfig{}
	for _, opt := range opts {
		opt(config)
	}

	// Generate a unique stream ID for chunks
	streamID, err := sc.generateStreamID()
	if err != nil {
		return nil, liblbryerrors.Err("failed to generate stream ID: %w", err)
	}

	// Wrap the source reader with progress tracking if needed
	var progressReader io.Reader = source
	if config.Progress != nil && size > 0 {
		progressReader = &progressTrackingReader{
			reader: source,
			size:   size,
			progressCallback: func(read int64) {
				progress := float64(read) / float64(size)
				if progress > 1.0 {
					progress = 1.0
				}
				config.Progress(progress)
			},
		}
	}

	// Create encoder
	encoder := NewEncoder(progressReader)

	// Set source size hint for better memory allocation
	if size > 0 {
		encoder.SourceSizeHint(int(size))
	}

	// Wrap chunk handler to add stream ID
	originalChunkHandler := config.ChunkHandler
	if originalChunkHandler != nil {
		config.ChunkHandler = func(chunk Chunk) error {
			chunk.StreamID = streamID
			return originalChunkHandler(chunk)
		}
	}

	// Encode the stream
	result, err := encoder.Encode(config)
	if err != nil {
		return nil, liblbryerrors.Err("failed to encode stream: %w", err)
	}

	return result, nil
}

// CreateStreamFromFile creates a stream from a file in an fs.FS
func (sc *DefaultStreamCreator) CreateStreamFromFile(fsys fs.FS, path string, opts ...StreamOption) (*StreamResult, error) {
	file, err := fsys.Open(path)
	if err != nil {
		return nil, liblbryerrors.Err("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info for size
	info, err := file.Stat()
	if err != nil {
		return nil, liblbryerrors.Err("failed to get file info: %w", err)
	}

	// Get filename for suggested file name
	filename := filepath.Base(path)

	return sc.createStreamWithMetadata(file, info.Size(), filename, opts...)
}

// CreateStreamFromPath creates a stream from a file path
func (sc *DefaultStreamCreator) CreateStreamFromPath(path string, opts ...StreamOption) (*StreamResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, liblbryerrors.Err("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info for size
	info, err := file.Stat()
	if err != nil {
		return nil, liblbryerrors.Err("failed to get file info: %w", err)
	}

	// Get filename for suggested file name
	filename := filepath.Base(path)

	return sc.createStreamWithMetadata(file, info.Size(), filename, opts...)
}

// createStreamWithMetadata is a helper that creates a stream with file metadata
func (sc *DefaultStreamCreator) createStreamWithMetadata(source io.Reader, size int64, filename string, opts ...StreamOption) (*StreamResult, error) {
	// Apply options to config
	config := &StreamConfig{}
	for _, opt := range opts {
		opt(config)
	}

	// If no existing SD blob is provided, create a manifest-based one
	if len(config.ExistingSDBlob) == 0 {
		// Create manifest from source
		sdBlob, sdBlobData, err := sc.manifestCreator.CreateManifest(source, size)
		if err != nil {
			return nil, liblbryerrors.Err("failed to create manifest: %w", err)
		}

		// Set stream name and suggested file name
		sdBlob.StreamName = filename
		sdBlob.SuggestedFileName = filename

		// Convert to blob data
		sdBlobData, err = sdBlob.ToBlob()
		if err != nil {
			return nil, liblbryerrors.Err("failed to convert SD blob to data: %w", err)
		}

		config.ExistingSDBlob = sdBlobData

		// Reset source since we consumed it for manifest creation
		if resetter, ok := source.(io.Seeker); ok {
			_, err = resetter.Seek(0, io.SeekStart)
			if err != nil {
				return nil, liblbryerrors.Err("failed to reset source reader: %w", err)
			}
		} else {
			return nil, liblbryerrors.Err("source reader does not support seeking")
		}
	}

	// Create stream with existing SD blob
	return sc.CreateStream(source, size, opts...)
}

// generateStreamID generates a unique identifier for a stream
func (sc *DefaultStreamCreator) generateStreamID() (string, error) {
	bytes := make([]byte, 16)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// progressTrackingReader wraps an io.Reader to track read progress
type progressTrackingReader struct {
	reader           io.Reader
	size             int64
	read             int64
	progressCallback func(int64)
}

// Read implements io.Reader interface with progress tracking
func (r *progressTrackingReader) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	if n > 0 {
		r.read += int64(n)
		if r.progressCallback != nil {
			r.progressCallback(r.read)
		}
	}
	return n, err
}
