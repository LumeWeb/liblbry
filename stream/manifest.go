package stream

import (
	"fmt"
	"io"
	"math"
	"os"

	lbrycrypto "go.lumeweb.com/liblbry/crypto"
)

// ParseManifest parses SD blob data into an SDBlob struct
func ParseManifest(data []byte) (*SDBlob, error) {
	var sd SDBlob
	err := sd.FromBlob(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SD blob: %w", err)
	}

	return &sd, nil
}

// BuildManifestOption is a function type for configuring manifest building
type BuildManifestOption func(*buildManifestConfig)

type buildManifestConfig struct {
	streamType        string
	streamName        string
	suggestedFileName string
}

// WithBuildManifestStreamType sets the stream type for BuildManifest
func WithBuildManifestStreamType(streamType string) BuildManifestOption {
	return func(c *buildManifestConfig) {
		c.streamType = streamType
	}
}

// WithBuildManifestStreamName sets the stream name for BuildManifest
func WithBuildManifestStreamName(streamName string) BuildManifestOption {
	return func(c *buildManifestConfig) {
		c.streamName = streamName
	}
}

// WithBuildManifestSuggestedFileName sets the suggested file name for BuildManifest
func WithBuildManifestSuggestedFileName(suggestedFileName string) BuildManifestOption {
	return func(c *buildManifestConfig) {
		c.suggestedFileName = suggestedFileName
	}
}

// BuildManifest creates an SD blob manifest from pre-computed blob infos
// This is useful when you have blob information from storage/DHT and want to reconstruct a manifest
func BuildManifest(blobInfos []BlobInfo, key []byte, opts ...BuildManifestOption) (*SDBlob, error) {
	if len(blobInfos) == 0 {
		return nil, fmt.Errorf("blob infos cannot be empty")
	}

	if len(key) != lbrycrypto.AES256KeySize && len(key) != lbrycrypto.AES128KeySize {
		return nil, fmt.Errorf("invalid key size: expected %d or %d bytes, got %d bytes", lbrycrypto.AES256KeySize, lbrycrypto.AES128KeySize, len(key))
	}

	// Apply options
	config := &buildManifestConfig{
		streamType: StreamTypeLBRYFile,
	}
	for _, opt := range opts {
		opt(config)
	}

	// Create SD blob
	sd := &SDBlob{
		StreamName:        config.streamName,
		BlobInfos:         blobInfos,
		StreamType:        config.streamType,
		Key:               key,
		SuggestedFileName: config.suggestedFileName,
	}

	// Update stream hash
	sd.UpdateStreamHash()

	return sd, nil
}

// EncoderOption is a function type for configuring encoder behavior in CreateManifestFromSource
type EncoderOption func(*Encoder)

// WithEncoderChunkSize sets a custom chunk size for the encoder
func WithEncoderChunkSize(size int) EncoderOption {
	return func(e *Encoder) {
		e.SourceSizeHint(size)
	}
}

// WithSerializationProfile sets the JSON serialization profile for the encoder
// Use profileOldSort for compatibility with legacy Python SDK SD blobs
func WithSerializationProfile(profile serializationProfile) EncoderOption {
	return func(e *Encoder) {
		e.sd.SetProfile(profile)
	}
}

// CreateManifestFromSource creates a manifest from a source reader using the encoder
// This is a convenience function for the common use case of creating a manifest from data
func CreateManifestFromSource(source io.Reader, size int64, opts ...EncoderOption) (*SDBlob, []byte, error) {
	key, err := lbrycrypto.GenerateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	if size > int64(math.MaxInt32) {
		return nil, nil, fmt.Errorf("size hint overflows int: %d", size)
	}

	encoder := NewEncoderWithIVs(source, key, nil).SourceSizeHint(int(size))

	for _, opt := range opts {
		opt(encoder)
	}

	for {
		_, err := encoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read blob: %w", err)
		}
	}

	sd := encoder.SDBlob()
	sdBlobData, err := sd.ToBlob()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize SD blob: %w", err)
	}

	return sd, sdBlobData, nil
}

// CreateManifestFromPath creates a manifest from a file path
func CreateManifestFromPath(path string, opts ...EncoderOption) (*SDBlob, []byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return CreateManifestFromSource(file, stat.Size(), opts...)
}
