package stream

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultStreamCreator_CreateStream(t *testing.T) {
	// Create a stream creator
	streamCreator := NewStreamCreator()

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
	// Create a stream creator
	streamCreator := NewStreamCreator()

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

	// Create a stream creator
	streamCreator := NewStreamCreator()

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
	// Create a stream creator
	streamCreator := NewStreamCreator()

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
	totalChunkData := 0
	for _, size := range result.ChunkSizes {
		totalChunkData += size
	}

	// The total chunk data should be >= our original test data size
	assert.GreaterOrEqual(t, totalChunkData, len(testData))
}

func TestDefaultStreamCreator_ErrorHandling(t *testing.T) {
	// Create a stream creator
	streamCreator := NewStreamCreator()

	// Test with nil reader
	result, err := streamCreator.CreateStream(nil, 100)
	assert.Error(t, err)
	assert.Nil(t, result)

	// Test with negative size
	testData := "test data"
	reader := strings.NewReader(testData)
	result, err = streamCreator.CreateStream(reader, -1)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestDefaultStreamCreator_WithExistingSDBlob(t *testing.T) {
	// Create test data
	testData := "This is test data for stream creation with existing SD blob"
	reader := strings.NewReader(testData)

	// First, create an SD blob using CreateManifestFromSource
	sdBlob, sdBlobData, err := CreateManifestFromSource(reader, int64(len(testData)))
	require.NoError(t, err)
	assert.NotNil(t, sdBlob)
	assert.NotNil(t, sdBlobData)

	// Reset reader
	reader = strings.NewReader(testData)

	// Create a stream creator
	streamCreator := NewStreamCreator()

	// Create stream with existing SD blob
	result, err := streamCreator.CreateStream(reader, int64(len(testData)), WithExistingSDBlob(sdBlobData))
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.SDBlob)
	assert.NotNil(t, result.SDBlobData)
	assert.NotEmpty(t, result.SDBlobHash)
	assert.NotEmpty(t, result.StreamHash)
	// SourceSize should be 0 because stream processing is skipped when using existing SD blob with blob infos
	assert.Equal(t, int64(0), result.SourceSize)
	// TotalChunks should match the number of blobs in the SD blob (excluding terminating blob)
	assert.Equal(t, len(sdBlob.BlobInfos)-1, result.TotalChunks)
}

func TestDefaultStreamCreator_EmptyData(t *testing.T) {
	// Create a stream creator
	streamCreator := NewStreamCreator()

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
	// Create a stream creator
	streamCreator := NewStreamCreator()

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
	// Create a stream creator
	streamCreator := NewStreamCreator()

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

// mockFS implements fs.FS and returns non-seekable files
type mockFS struct {
	files map[string][]byte
}

func (m *mockFS) Open(name string) (fs.File, error) {
	data, exists := m.files[name]
	if !exists {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	// Return a non-seekable file (like embed.FS)
	return &mockNonSeekableFile{
		reader: strings.NewReader(string(data)),
		name:   name,
		size:   int64(len(data)),
	}, nil
}

// mockNonSeekableFile simulates a non-seekable file like those from embed.FS
type mockNonSeekableFile struct {
	reader *strings.Reader
	name   string
	size   int64
}

func (f *mockNonSeekableFile) Stat() (fs.FileInfo, error) {
	return &mockFileInfo{name: f.name, size: f.size}, nil
}

func (f *mockNonSeekableFile) Read(p []byte) (int, error) {
	return f.reader.Read(p)
}

func (f *mockNonSeekableFile) Close() error {
	return nil
}

// mockFileInfo implements fs.FileInfo
type mockFileInfo struct {
	name string
	size int64
}

func (fi *mockFileInfo) Name() string {
	return fi.name
}

func (fi *mockFileInfo) Size() int64 {
	return fi.size
}

func (fi *mockFileInfo) Mode() fs.FileMode {
	return 0644
}

func (fi *mockFileInfo) ModTime() time.Time {
	return time.Time{}
}

func (fi *mockFileInfo) IsDir() bool {
	return false
}

func (fi *mockFileInfo) Sys() any {
	return nil
}

func TestDefaultStreamCreator_ChunkHandlerErrors(t *testing.T) {
	// Create a stream creator
	streamCreator := NewStreamCreator()

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

func TestDefaultStreamCreator_NonSeekableFileHandling(t *testing.T) {
	// Create a stream creator
	streamCreator := NewStreamCreator()

	// Test 1: Small non-seekable file should work
	t.Run("SmallNonSeekableFile", func(t *testing.T) {
		// Create mock FS with small file
		mockFS := &mockFS{
			files: map[string][]byte{
				"small.txt": []byte("This is a small test file"),
			},
		}

		result, err := streamCreator.CreateStreamFromFile(mockFS, "small.txt")
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, int64(len("This is a small test file")), result.SourceSize)
	})

	// Test 2: Large non-seekable file should return error
	t.Run("LargeNonSeekableFile", func(t *testing.T) {
		// Create mock FS with large file (exceeding maxFileSizeForMemory)
		largeData := make([]byte, maxFileSizeForMemory+1024)
		for i := range largeData {
			largeData[i] = byte('A' + (i % 26))
		}

		mockFS := &mockFS{
			files: map[string][]byte{
				"large.txt": largeData,
			},
		}

		result, err := streamCreator.CreateStreamFromFile(mockFS, "large.txt")
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "exceeds maximum allowed size")
		assert.Contains(t, err.Error(), "use CreateStreamFromPath instead")
	})

	// Test 3: Seekable files should continue to work
	t.Run("SeekableFile", func(t *testing.T) {
		// Create a temporary file (seekable)
		tmpFile, err := os.CreateTemp("", "test_seekable_file")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		// Write test data to the file
		testData := "This is test data for a seekable file"
		_, err = tmpFile.WriteString(testData)
		require.NoError(t, err)

		// Close the file to ensure data is written
		err = tmpFile.Close()
		require.NoError(t, err)

		// Create a stream creator
		streamCreator := NewStreamCreator()

		// Use the real filesystem which provides seekable files
		result, err := streamCreator.CreateStreamFromFile(os.DirFS(filepath.Dir(tmpFile.Name())), filepath.Base(tmpFile.Name()))
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, int64(len(testData)), result.SourceSize)
	})
}
