package stream

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lbrycrypto "go.lumeweb.com/liblbry/crypto"
)

func TestDefaultManifestCreator_CreateManifest(t *testing.T) {
	creator := NewManifestCreator()

	// Test with simple data
	testData := []byte("test data for manifest creation")
	reader := bytes.NewReader(testData)
	size := int64(len(testData))

	sd, sdData, err := creator.CreateManifest(reader, size)
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// Verify SD blob structure
	// StreamName and SuggestedFileName are not populated by the manifest creator (json:"-" fields)
	assert.Empty(t, sd.StreamName)
	assert.Empty(t, sd.SuggestedFileName)

	// Key should be populated and of expected size
	assert.NotEmpty(t, sd.Key)
	assert.Len(t, sd.Key, lbrycrypto.KeySize)

	// StreamHash should be computed
	assert.NotEmpty(t, sd.StreamHash)

	// StreamType should be set to lbryfile
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)

	// Verify blob infos exist and have at least the terminating blob
	assert.NotEmpty(t, sd.BlobInfos)
	assert.True(t, len(sd.BlobInfos) >= 1)

	// Verify the SD blob can be parsed back
	parsedSD, err := creator.ParseManifest(sdData)
	require.NoError(t, err)

	// When parsed back, StreamName and SuggestedFileName will be empty strings (not nil)
	assert.Equal(t, "", parsedSD.StreamName)
	assert.Equal(t, "", parsedSD.SuggestedFileName)

	assert.Equal(t, sd.StreamType, parsedSD.StreamType)
	assert.True(t, bytes.Equal(sd.StreamHash, parsedSD.StreamHash))
	assert.True(t, bytes.Equal(sd.Key, parsedSD.Key))
}

func TestDefaultManifestCreator_CreateManifestFromPath(t *testing.T) {
	creator := NewManifestCreator()

	// Create a temporary test file
	tmpFile, err := os.CreateTemp("", "manifest_test")
	require.NoError(t, err)
	defer func() {
		err := os.Remove(tmpFile.Name())
		require.NoError(t, err)
	}()

	testData := []byte("test file data for manifest creation from path")
	_, err = tmpFile.Write(testData)
	require.NoError(t, err)
	err = tmpFile.Close()
	require.NoError(t, err)

	sd, sdData, err := creator.CreateManifestFromPath(tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// Verify SD blob structure
	// StreamName and SuggestedFileName are not populated by the manifest creator (json:"-" fields)
	assert.Empty(t, sd.StreamName)
	assert.Empty(t, sd.SuggestedFileName)

	// Key should be populated and of expected size
	assert.NotEmpty(t, sd.Key)
	assert.Len(t, sd.Key, lbrycrypto.KeySize)

	// StreamHash should be computed
	assert.NotEmpty(t, sd.StreamHash)

	// StreamType should be set to lbryfile
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)

	// Verify blob infos exist and have at least the terminating blob
	assert.NotEmpty(t, sd.BlobInfos)
	assert.True(t, len(sd.BlobInfos) >= 1)
}

func TestDefaultManifestCreator_CreateManifestFromPath_NotFound(t *testing.T) {
	creator := NewManifestCreator()

	_, _, err := creator.CreateManifestFromPath("/non/existent/file")
	assert.Error(t, err)
}

func TestDefaultManifestCreator_ParseManifest(t *testing.T) {
	creator := NewManifestCreator()

	// Create a valid SD blob data for testing
	testData := []byte("test data")
	reader := bytes.NewReader(testData)
	size := int64(len(testData))

	_, sdData, err := creator.CreateManifest(reader, size)
	require.NoError(t, err)

	// Parse the manifest
	sd, err := creator.ParseManifest(sdData)
	require.NoError(t, err)
	require.NotNil(t, sd)

	// Verify fields are populated
	// StreamName and SuggestedFileName are not populated by the manifest creator (json:"-" fields)
	assert.Empty(t, sd.StreamName)
	assert.Empty(t, sd.SuggestedFileName)

	// BlobInfos should exist
	assert.NotEmpty(t, sd.BlobInfos)

	// StreamType should be lbryfile
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)

	// Key should be populated and of expected size
	assert.NotEmpty(t, sd.Key)
	assert.Len(t, sd.Key, lbrycrypto.KeySize)

	// StreamHash should be computed
	assert.NotEmpty(t, sd.StreamHash)
}

func TestDefaultManifestCreator_ParseManifest_InvalidData(t *testing.T) {
	creator := NewManifestCreator()

	// Test with invalid JSON data
	invalidData := []byte("invalid json data")
	_, err := creator.ParseManifest(invalidData)
	assert.Error(t, err)

	// Test with empty data
	_, err = creator.ParseManifest([]byte{})
	assert.Error(t, err)

	// Test with malformed JSON
	malformedData := []byte(`{"stream_type": "lbryfile", "blobs": [}`)
	_, err = creator.ParseManifest(malformedData)
	assert.Error(t, err)
}

func TestDefaultManifestCreator_DefaultInstance(t *testing.T) {
	instance := DefaultManifestCreatorInstance()
	assert.NotNil(t, instance)

	// Verify it's the same instance on subsequent calls
	instance2 := DefaultManifestCreatorInstance()
	assert.Equal(t, instance, instance2)
}

func TestDefaultManifestCreator_CreateManifest_EmptyData(t *testing.T) {
	creator := NewManifestCreator()

	reader := bytes.NewReader([]byte{})
	size := int64(0)

	sd, sdData, err := creator.CreateManifest(reader, size)
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// For empty data, we should still have a valid SD blob with at least the terminating blob
	assert.NotEmpty(t, sd.BlobInfos)
	assert.Equal(t, 1, len(sd.BlobInfos)) // Should only have the terminating 0-length blob
	assert.Equal(t, 0, sd.BlobInfos[0].Length)

	// Verify other fields are properly set
	assert.Empty(t, sd.StreamName)
	assert.Empty(t, sd.SuggestedFileName)
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
	assert.NotEmpty(t, sd.Key)
	assert.NotEmpty(t, sd.StreamHash)
}

func TestDefaultManifestCreator_CreateManifest_LargeData(t *testing.T) {
	creator := NewManifestCreator()

	// Test with data that requires multiple blobs
	testData := make([]byte, maxBlobDataSize*3+100) // 3 full blobs + 1 partial blob
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	reader := bytes.NewReader(testData)
	size := int64(len(testData))

	sd, sdData, err := creator.CreateManifest(reader, size)
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// Should have 5 blob infos (3 content blobs + 1 partial blob + 1 terminating blob)
	assert.Equal(t, 5, len(sd.BlobInfos))

	// Last blob should be the terminating 0-length blob
	assert.Equal(t, 0, sd.BlobInfos[len(sd.BlobInfos)-1].Length)

	// Other blobs should have proper lengths
	for i := 0; i < len(sd.BlobInfos)-1; i++ {
		assert.True(t, sd.BlobInfos[i].Length > 0)
		assert.NotEmpty(t, sd.BlobInfos[i].BlobHash)
		assert.NotEmpty(t, sd.BlobInfos[i].IV)
	}

	// Verify SD blob structure
	assert.Empty(t, sd.StreamName)
	assert.Empty(t, sd.SuggestedFileName)
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
	assert.NotEmpty(t, sd.Key)
	assert.Len(t, sd.Key, lbrycrypto.KeySize)
	assert.NotEmpty(t, sd.StreamHash)
}

func TestDefaultManifestCreator_CreateManifest_ErrorHandling(t *testing.T) {
	creator := NewManifestCreator()

	// Test with negative size
	testData := []byte("test data")
	reader := bytes.NewReader(testData)
	_, _, err := creator.CreateManifest(reader, -1)
	require.NoError(t, err) // Negative size should not cause error in this implementation

	// Test with invalid reader that returns an error
	_invalidReader := &invalidReader{}
	_, _, err = creator.CreateManifest(_invalidReader, 100)
	assert.Error(t, err)
}

// invalidReader is a test helper that always returns an error
type invalidReader struct{}

func (r *invalidReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read error")
}
