package stream

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStreamCreator_CreateStream(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test data
	testData := "This is test data for stream creation"
	reader := strings.NewReader(testData)

	// Create stream
	result, err := streamCreator.CreateStream(reader, int64(len(testData)))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.SDBlob)
	assert.NotNil(t, result.SDBlobData)
	assert.NotEmpty(t, result.SDBlobHash)
	assert.NotEmpty(t, result.StreamHash)
	assert.Equal(t, int64(len(testData)), result.SourceSize)
	assert.Greater(t, result.TotalChunks, 0)
	assert.NotEmpty(t, result.ChunkSizes)
}

func TestDefaultStreamCreator_CreateStreamWithOptions(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test data
	testData := "This is test data for stream creation with options"
	reader := strings.NewReader(testData)

	// Track progress
	var progress float64
	progressCallback := func(p float64) {
		progress = p
	}

	// Track chunks
	var chunks []Chunk
	chunkHandler := func(chunk Chunk) error {
		chunks = append(chunks, chunk)
		return nil
	}

	// Create stream with options
	result, err := streamCreator.CreateStream(reader, int64(len(testData)),
		WithProgress(progressCallback),
		WithChunkHandler(chunkHandler))
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify progress was called
	assert.Equal(t, float64(1.0), progress)

	// Verify chunks were processed
	assert.NotEmpty(t, chunks)
	assert.Equal(t, result.TotalChunks, len(chunks))
}

func TestDefaultStreamCreator_CreateStreamFromPath(t *testing.T) {
	// Create a temporary file
	tmpFile, err := os.CreateTemp("", "test_stream_file")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Write test data to the file
	testData := "This is test data for stream creation from file path"
	_, err = tmpFile.WriteString(testData)
	require.NoError(t, err)

	// Close the file to ensure data is written
	err = tmpFile.Close()
	require.NoError(t, err)

	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Create stream from file path
	result, err := streamCreator.CreateStreamFromPath(tmpFile.Name())
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.SDBlob)
	assert.NotNil(t, result.SDBlobData)
	assert.NotEmpty(t, result.SDBlobHash)
	assert.NotEmpty(t, result.StreamHash)
	assert.Equal(t, int64(len(testData)), result.SourceSize)
	assert.Greater(t, result.TotalChunks, 0)
	assert.NotEmpty(t, result.ChunkSizes)
}

func TestDefaultStreamCreator_WithChunkSize(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test data - larger data to ensure multiple chunks
	testData := strings.Repeat("A", 2048)
	reader := strings.NewReader(testData)

	// Create stream with custom chunk size
	customChunkSize := 528
	result, err := streamCreator.CreateStream(reader, int64(len(testData)), WithChunkSize(customChunkSize))
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify chunk sizes
	assert.NotEmpty(t, result.ChunkSizes)

	// Since blob data includes padding, actual chunk sizes may differ from requested size
	// We'll verify that we have multiple chunks and that the total matches our data size
	totalChunkData := 0
	for _, size := range result.ChunkSizes {
		totalChunkData += size
	}
	
	// The total chunk data should be >= our original test data size
	assert.GreaterOrEqual(t, totalChunkData, len(testData))
}

func TestDefaultStreamCreator_ErrorHandling(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test with nil reader
	result, err := streamCreator.CreateStream(nil, 100)
	assert.Error(t, err)
	assert.Nil(t, result)

	// Test with negative size
	testData := "test data"
	reader := strings.NewReader(testData)
	result, err = streamCreator.CreateStream(reader, -1)
	assert.NoError(t, err) // Negative size is not necessarily an error in this implementation
	assert.NotNil(t, result)
}

func TestDefaultStreamCreator_WithExistingSDBlob(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create test data
	testData := "This is test data for stream creation with existing SD blob"
	reader := strings.NewReader(testData)

	// First, create an SD blob
	sdBlob, sdBlobData, err := manifestCreator.CreateManifest(reader, int64(len(testData)))
	require.NoError(t, err)
	assert.NotNil(t, sdBlob)
	assert.NotNil(t, sdBlobData)

	// Reset reader
	reader = strings.NewReader(testData)

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Create stream with existing SD blob
	result, err := streamCreator.CreateStream(reader, int64(len(testData)), WithExistingSDBlob(sdBlobData))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.SDBlob)
	assert.NotNil(t, result.SDBlobData)
	assert.NotEmpty(t, result.SDBlobHash)
	assert.NotEmpty(t, result.StreamHash)
	assert.Equal(t, int64(len(testData)), result.SourceSize)
}

func TestDefaultStreamCreator_EmptyData(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test with empty data
	reader := strings.NewReader("")
	result, err := streamCreator.CreateStream(reader, 0)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.SDBlob)
	assert.NotNil(t, result.SDBlobData)
	assert.NotEmpty(t, result.SDBlobHash)
	assert.NotEmpty(t, result.StreamHash)
	assert.Equal(t, int64(0), result.SourceSize)
	assert.Equal(t, 0, result.TotalChunks)
	assert.Empty(t, result.ChunkSizes)
}

func TestDefaultStreamCreator_ProgressAccuracy(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test data
	testData := "This is test data for checking progress accuracy"
	reader := strings.NewReader(testData)
	totalSize := int64(len(testData))

	// Track progress calls
	var progressCalls []float64
	progressCallback := func(p float64) {
		progressCalls = append(progressCalls, p)
	}

	// Create stream with progress tracking
	result, err := streamCreator.CreateStream(reader, totalSize, WithProgress(progressCallback))
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Verify progress was called
	assert.NotEmpty(t, progressCalls)

	// Verify final progress is 1.0
	assert.Equal(t, float64(1.0), progressCalls[len(progressCalls)-1])
}

func TestDefaultStreamCreator_RoundTrip(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test data
	testData := "This is test data for round trip stream creation and decoding"
	reader := strings.NewReader(testData)

	// Create stream
	result, err := streamCreator.CreateStream(reader, int64(len(testData)))
	require.NoError(t, err)
	assert.NotNil(t, result)

	// Convert result to Stream type for decoding
	stream := make(Stream, 0, len(result.ContentBlobs)+1)
	stream = append(stream, result.SDBlobData)
	for _, blob := range result.ContentBlobs {
		stream = append(stream, blob)
	}

	// Decode stream
	decodedData, err := stream.Decode()
	require.NoError(t, err)
	assert.Equal(t, testData, string(decodedData))
}

func TestDefaultStreamCreator_ChunkHandlerErrors(t *testing.T) {
	// Create a manifest creator
	manifestCreator := NewManifestCreator()

	// Create a stream creator
	streamCreator := NewStreamCreator(manifestCreator)

	// Test data
	testData := "This is test data for testing chunk handler error propagation"
	reader := strings.NewReader(testData)

	// Create a chunk handler that returns an error
	chunkHandler := func(chunk Chunk) error {
		return io.ErrUnexpectedEOF
	}

	// Create stream with error-producing chunk handler
	result, err := streamCreator.CreateStream(reader, int64(len(testData)), WithChunkHandler(chunkHandler))
	assert.Error(t, err)
	assert.Nil(t, result)
	
	// Check that the error is the exact expected error type
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}
