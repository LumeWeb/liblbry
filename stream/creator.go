package stream

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

const maxFileSizeForMemory = 100 * 1024 * 1024 // 100MB

// StreamCreator defines the interface for creating streams from data sources
type StreamCreator interface {
	CreateStream(source io.Reader, size int64, opts ...StreamOption) (*StreamResult, error)
	CreateStreamFromFile(fsys fs.FS, path string, opts ...StreamOption) (*StreamResult, error)
	CreateStreamFromPath(path string, opts ...StreamOption) (*StreamResult, error)
}

// DefaultStreamCreator implements StreamCreator interface
type DefaultStreamCreator struct{}

// NewStreamCreator creates a new DefaultStreamCreator instance
func NewStreamCreator() *DefaultStreamCreator {
	return &DefaultStreamCreator{}
}

// CreateStream creates a stream from an io.Reader
func (sc *DefaultStreamCreator) CreateStream(source io.Reader, size int64, opts ...StreamOption) (*StreamResult, error) {
	// Validate input
	if source == nil {
		return nil, liblbryerrors.Err("source reader cannot be nil")
	}

	// Apply options to config
	config := &StreamConfig{}
	applyOpts(config, opts)

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

	// Check if file is seekable
	if _, ok := file.(io.Seeker); ok {
		// If seekable, proceed with original file
		return sc.createStreamWithMetadata(file, info.Size(), filename, opts...)
	}

	// For non-seekable files, enforce size limit to avoid unbounded memory use
	if info.Size() > maxFileSizeForMemory {
		return nil, liblbryerrors.Err("non-seekable file size %d exceeds maximum allowed size %d - use CreateStreamFromPath instead", info.Size(), maxFileSizeForMemory)
	}

	// Read all data into memory for non-seekable files
	data, err := io.ReadAll(file)
	if err != nil {
		return nil, liblbryerrors.Err("failed to read file data: %w", err)
	}

	// Create a seekable reader from the data
	reader := bytes.NewReader(data)

	return sc.createStreamWithMetadata(reader, int64(len(data)), filename, opts...)
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
	// Add stream name and suggested file name to options
	newOpts := appendOption(opts, WithStreamName(filename))
	newOpts = appendOption(newOpts, WithSuggestedFileName(filename))

	// Create stream directly with metadata
	return sc.CreateStream(source, size, newOpts...)
}

// applyOpts applies all StreamOption functions to the provided StreamConfig
func applyOpts(config *StreamConfig, opts []StreamOption) {
	for _, opt := range opts {
		opt(config)
	}
}

// appendOption creates a new slice of StreamOption with the provided option appended
func appendOption(opts []StreamOption, opt StreamOption) []StreamOption {
	newOpts := make([]StreamOption, len(opts)+1)
	copy(newOpts, opts)
	newOpts[len(opts)] = opt
	return newOpts
}

// generateStreamID generates a unique identifier for a stream
func (sc *DefaultStreamCreator) generateStreamID() (string, error) {
	_bytes := make([]byte, 16)
	_, err := rand.Read(_bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(_bytes), nil
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
