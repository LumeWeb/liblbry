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

func TestParseManifest(t *testing.T) {
	// Create a valid SD blob data for testing
	testData := []byte("test data for parsing")
	_, sdBlobData, err := CreateManifestFromSource(bytes.NewReader(testData), int64(len(testData)))
	require.NoError(t, err)
	require.NotEmpty(t, sdBlobData)

	// Parse the manifest using package-level function
	parsedSD, err := ParseManifest(sdBlobData)
	require.NoError(t, err)
	require.NotNil(t, parsedSD)

	// Verify fields are populated
	assert.Equal(t, StreamTypeLBRYFile, parsedSD.StreamType)
	assert.NotEmpty(t, parsedSD.StreamHash)
	assert.NotEmpty(t, parsedSD.BlobInfos)
}

func TestParseManifest_InvalidData(t *testing.T) {
	// Test with invalid JSON data
	invalidData := []byte("invalid json data")
	_, err := ParseManifest(invalidData)
	assert.Error(t, err)

	// Test with empty data
	_, err = ParseManifest([]byte{})
	assert.Error(t, err)

	// Test with malformed JSON
	malformedData := []byte(`{"stream_type": "lbryfile", "blobs": [}`)
	_, err = ParseManifest(malformedData)
	assert.Error(t, err)
}

func TestValidateSDBlob_PackageLevel(t *testing.T) {
	// Create a valid SD blob
	testData := []byte("test data for validation")
	_, sdBlobData, err := CreateManifestFromSource(bytes.NewReader(testData), int64(len(testData)))
	require.NoError(t, err)

	// Validate the manifest using package-level function
	err = ValidateSDBlobBytes(sdBlobData)
	require.NoError(t, err)

	// Test with invalid data
	err = ValidateSDBlobBytes([]byte("invalid"))
	assert.Error(t, err)

	err = ValidateSDBlobBytes([]byte{})
	assert.Error(t, err)
}

func TestBuildManifest(t *testing.T) {
	// Create test blob infos
	key, err := lbrycrypto.GenerateKey()
	require.NoError(t, err)

	blobInfos := []BlobInfo{
		{
			BlobNum:  0,
			Length:   100,
			BlobHash: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
		{
			BlobNum:  1,
			Length:   50,
			BlobHash: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
		{
			BlobNum:  2,
			Length:   0, // Terminating blob
			BlobHash: []byte{},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
	}

	// Build manifest
	sd, err := BuildManifest(blobInfos, key)
	require.NoError(t, err)
	require.NotNil(t, sd)

	// Verify SD blob fields
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
	assert.True(t, bytes.Equal(key, sd.Key))
	assert.Equal(t, len(blobInfos), len(sd.BlobInfos))
	assert.NotEmpty(t, sd.StreamHash)
}

func TestBuildManifest_WithMetadata(t *testing.T) {
	// Create test blob infos
	key, err := lbrycrypto.GenerateKey()
	require.NoError(t, err)

	blobInfos := []BlobInfo{
		{
			BlobNum:  0,
			Length:   100,
			BlobHash: []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
		{
			BlobNum:  1,
			Length:   0, // Terminating blob
			BlobHash: []byte{},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
	}

	// Build manifest with metadata
	streamName := "test_stream"
	suggestedFileName := "test_file.txt"
	sd, err := BuildManifest(blobInfos, key, WithBuildManifestStreamName(streamName), WithBuildManifestSuggestedFileName(suggestedFileName))
	require.NoError(t, err)
	require.NotNil(t, sd)

	// Verify metadata fields
	assert.Equal(t, streamName, sd.StreamName)
	assert.Equal(t, suggestedFileName, sd.SuggestedFileName)
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
}

func TestBuildManifest_EmptyBlobInfos(t *testing.T) {
	key, err := lbrycrypto.GenerateKey()
	require.NoError(t, err)

	// Test with empty blob infos
	_, err = BuildManifest([]BlobInfo{}, key)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob infos cannot be empty")
}

func TestBuildManifest_InvalidKeySize(t *testing.T) {
	blobInfos := []BlobInfo{
		{
			BlobNum:  0,
			Length:   0,
			BlobHash: []byte{},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
	}

	// Test with invalid key size
	_, err := BuildManifest(blobInfos, []byte("short"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid key size")
}

func TestBuildManifest_DefaultStreamType(t *testing.T) {
	key, err := lbrycrypto.GenerateKey()
	require.NoError(t, err)

	blobInfos := []BlobInfo{
		{
			BlobNum:  0,
			Length:   0,
			BlobHash: []byte{},
			IV:       []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		},
	}

	// Build manifest without specifying stream type
	sd, err := BuildManifest(blobInfos, key)
	require.NoError(t, err)

	// Should default to StreamTypeLBRYFile
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
}

func TestCreateManifestFromSource(t *testing.T) {
	testData := []byte("test data for manifest creation from source")
	reader := bytes.NewReader(testData)
	size := int64(len(testData))

	sd, sdData, err := CreateManifestFromSource(reader, size)
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// Verify SD blob structure
	assert.NotEmpty(t, sd.Key)
	assert.Len(t, sd.Key, lbrycrypto.AES256KeySize)
	assert.NotEmpty(t, sd.StreamHash)
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
	assert.NotEmpty(t, sd.BlobInfos)
	assert.True(t, len(sd.BlobInfos) >= 1)

	// Verify the SD blob can be parsed back
	parsedSD, err := ParseManifest(sdData)
	require.NoError(t, err)
	assert.Equal(t, sd.StreamType, parsedSD.StreamType)
	assert.True(t, bytes.Equal(sd.StreamHash, parsedSD.StreamHash))
}

func TestCreateManifestFromPath(t *testing.T) {
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

	sd, sdData, err := CreateManifestFromPath(tmpFile.Name())
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// Verify SD blob structure
	assert.NotEmpty(t, sd.Key)
	assert.Len(t, sd.Key, lbrycrypto.AES256KeySize)
	assert.NotEmpty(t, sd.StreamHash)
	assert.Equal(t, StreamTypeLBRYFile, sd.StreamType)
	assert.NotEmpty(t, sd.BlobInfos)
}

func TestCreateManifestFromPath_NotFound(t *testing.T) {
	_, _, err := CreateManifestFromPath("/non/existent/file")
	assert.Error(t, err)
}

func TestCreateManifestFromSource_EmptyData(t *testing.T) {
	reader := bytes.NewReader([]byte{})
	size := int64(0)

	sd, sdData, err := CreateManifestFromSource(reader, size)
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// For empty data, we should still have a valid SD blob with at least the terminating blob
	assert.NotEmpty(t, sd.BlobInfos)
	assert.Equal(t, 1, len(sd.BlobInfos))
	assert.Equal(t, 0, sd.BlobInfos[0].Length)
}

func TestCreateManifestFromSource_LargeData(t *testing.T) {
	// Test with data that requires multiple blobs
	testData := make([]byte, maxBlobDataSize*3+100)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	reader := bytes.NewReader(testData)
	size := int64(len(testData))

	sd, sdData, err := CreateManifestFromSource(reader, size)
	require.NoError(t, err)
	require.NotNil(t, sd)
	require.NotEmpty(t, sdData)

	// Should have 5 blob infos (3 content blobs + 1 partial blob + 1 terminating blob)
	assert.Equal(t, 5, len(sd.BlobInfos))
	assert.Equal(t, 0, sd.BlobInfos[len(sd.BlobInfos)-1].Length)
}

func TestCreateManifestFromSource_ErrorHandling(t *testing.T) {
	// Test with invalid reader that returns an error
	invalidReader := &invalidReader{}
	_, _, err := CreateManifestFromSource(invalidReader, 100)
	assert.Error(t, err)
}

// invalidReader is a test helper that always returns an error
type invalidReader struct{}

func (r *invalidReader) Read(p []byte) (n int, err error) {
	return 0, fmt.Errorf("read error")
}
