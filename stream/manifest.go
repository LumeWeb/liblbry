package stream

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	lbrycrypto "go.lumeweb.com/liblbry/crypto"
)

// ManifestCreator defines the interface for creating and parsing SD blob manifests
type ManifestCreator interface {
	CreateManifest(source io.Reader, size int64) (*SDBlob, []byte, error)
	CreateManifestFromPath(path string) (*SDBlob, []byte, error)
	ParseManifest(data []byte) (*SDBlob, error)
}

// DefaultManifestCreator implements the ManifestCreator interface
type DefaultManifestCreator struct{}

// defaultManifestCreatorInstance is the package-level default manifest creator instance
var defaultManifestCreatorInstance = NewManifestCreator()

// DefaultManifestCreatorInstance returns the package-level default ManifestCreator instance
func DefaultManifestCreatorInstance() ManifestCreator {
	return defaultManifestCreatorInstance
}

// NewManifestCreator returns a new ManifestCreator
func NewManifestCreator() ManifestCreator {
	return &DefaultManifestCreator{}
}

// CreateManifest creates an SD blob manifest from a reader
func (m *DefaultManifestCreator) CreateManifest(source io.Reader, size int64) (*SDBlob, []byte, error) {
	// Generate a random key for encryption
	key, err := lbrycrypto.GenerateKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate key: %w", err)
	}

	if size > int64(math.MaxInt) {
		return nil, nil, fmt.Errorf("size hint overflows int: %d", size)
	}

	// Create encoder with the generated key before processing blobs
	encoder := NewEncoderWithIVs(source, key, nil).SourceSizeHint(int(size))

	// Process all blobs
	for {
		_, err := encoder.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("failed to read blob: %w", err)
		}
	}

	// Get the SD blob
	sd := encoder.SDBlob()
	
	// Ensure the key is properly assigned to the SD blob
	sd.Key = key

	// Serialize the SD blob
	sdBlobData, err := sd.ToBlob()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize SD blob: %w", err)
	}

	return sd, sdBlobData, nil
}

// CreateManifestFromPath creates an SD blob manifest from a file path
func (m *DefaultManifestCreator) CreateManifestFromPath(path string) (_ *SDBlob, _ []byte, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			err = errors.Join(err, fmt.Errorf("failed to close file %q: %w", path, cerr))
		}
	}()

	stat, err := file.Stat()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to stat file: %w", err)
	}

	return m.CreateManifest(file, stat.Size())
}

// ParseManifest parses SD blob data into an SDBlob struct
func (m *DefaultManifestCreator) ParseManifest(data []byte) (*SDBlob, error) {
	var sd SDBlob
	err := sd.FromBlob(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SD blob: %w", err)
	}

	return &sd, nil
}
