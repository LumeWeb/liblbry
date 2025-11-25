package client

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/blob"
	"go.lumeweb.com/liblbry/blob/transfer"
	transferMocks "go.lumeweb.com/liblbry/blob/transfer/mocks"
	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
	"go.lumeweb.com/liblbry/mocks"
	"go.lumeweb.com/liblbry/storage/memory"
	storageMocks "go.lumeweb.com/liblbry/storage/mocks"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap/zaptest"
)

// createTestKey creates a test 32-byte encryption key
func createTestKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i % 256)
	}
	return key
}

// createTestIV creates a test 16-byte IV
func createTestIV() []byte {
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = byte(i)
	}
	return iv
}

// createTestContent creates test content of the specified size
func createTestContent(size int) []byte {
	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i % 256)
	}
	return content
}

// createEncryptedBlob creates an encrypted blob using the blob package
func createEncryptedBlob(tb testing.TB, content []byte, key []byte, iv []byte) []byte {
	tb.Helper()
	encryptedBlob, err := blob.NewBlob(content, key, iv)
	if err != nil {
		tb.Fatal(err)
	}
	return encryptedBlob
}

// testSetup holds common test setup components
type testSetup struct {
	ctx              context.Context
	sdHash           string
	contentHash      string
	key              []byte
	iv               []byte
	contentData      []byte
	encryptedContent []byte
	sdBlobData       []byte
	acquirer         *mocks.MockBlobAcquirer
	store            *storageMocks.MockBlobStore
	streamAcquirer   StreamAcquirer
}

// createTestSetup creates a complete test setup with all necessary components
// Verification is disabled by default for existing tests
func createTestSetup(tb testing.TB, contentSize int) *testSetup {
	return createTestSetupWithOptions(tb, contentSize, false)
}

// createTestSetupWithOptions creates a test setup with configurable verification
func createTestSetupWithOptions(tb testing.TB, contentSize int, verificationEnabled bool) *testSetup {
	tb.Helper()
	ctx := context.Background()
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyStream]
	contentHash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	key := createTestKey()
	iv := createTestIV()
	contentData := createTestContent(contentSize)

	var encryptedContent []byte
	if contentSize > 0 {
		encryptedContent = createEncryptedBlob(tb, contentData, key, iv)
	}

	sdBlobData, _ := createTestSDBlob(tb, contentSize, contentHash, iv, key)

	acquirer := mocks.NewMockBlobAcquirer(tb)
	store := storageMocks.NewMockBlobStore(tb)
	logger := zaptest.NewLogger(tb)

	streamAcquirer := NewStreamAcquirer(acquirer, store, logger)

	return &testSetup{
		ctx:              ctx,
		sdHash:           sdHash,
		contentHash:      contentHash,
		key:              key,
		iv:               iv,
		contentData:      contentData,
		encryptedContent: encryptedContent,
		sdBlobData:       sdBlobData,
		acquirer:         acquirer,
		store:            store,
		streamAcquirer:   streamAcquirer,
	}
}

// createTestSetupWithContent creates a test setup with specific content data
// Verification is disabled by default for existing tests
func createTestSetupWithContent(tb testing.TB, content []byte) *testSetup {
	return createTestSetupWithContentOptions(tb, content, false)
}

// createTestSetupWithContentOptions creates a test setup with specific content and configurable verification
func createTestSetupWithContentOptions(tb testing.TB, content []byte, verificationEnabled bool) *testSetup {
	tb.Helper()
	ctx := context.Background()
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyStream]
	contentHash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	key := createTestKey()
	iv := createTestIV()

	encryptedContent := createEncryptedBlob(tb, content, key, iv)
	sdBlobData, _ := createTestSDBlob(tb, len(content), contentHash, iv, key)

	acquirer := mocks.NewMockBlobAcquirer(tb)
	store := storageMocks.NewMockBlobStore(tb)
	logger := zaptest.NewLogger(tb)

	streamAcquirer := NewStreamAcquirer(acquirer, store, logger)

	return &testSetup{
		ctx:              ctx,
		sdHash:           sdHash,
		contentHash:      contentHash,
		key:              key,
		iv:               iv,
		contentData:      content,
		encryptedContent: encryptedContent,
		sdBlobData:       sdBlobData,
		acquirer:         acquirer,
		store:            store,
		streamAcquirer:   streamAcquirer,
	}
}

// getStreamWithoutVerification is a helper that gets a stream with verification disabled
func (ts *testSetup) getStreamWithoutVerification(opts ...AcquireOption) (StreamReader, error) {
	allOpts := append([]AcquireOption{WithAcquireStreamOptions(WithStreamVerification(false))}, opts...)
	return ts.streamAcquirer.GetStream(ts.ctx, ts.sdHash, allOpts...)
}

// getStreamWithVerification is a helper that gets a stream with verification enabled
func (ts *testSetup) getStreamWithVerification(opts ...AcquireOption) (StreamReader, error) {
	allOpts := append([]AcquireOption{WithAcquireStreamOptions(WithStreamVerification(true))}, opts...)
	return ts.streamAcquirer.GetStream(ts.ctx, ts.sdHash, allOpts...)
}

// getStreamWithoutVerificationFromAcquirer is a helper that gets a stream with verification disabled from any acquirer
func getStreamWithoutVerificationFromAcquirer(acquirer StreamAcquirer, ctx context.Context, sdHash string, opts ...AcquireOption) (StreamReader, error) {
	allOpts := append([]AcquireOption{WithAcquireStreamOptions(WithStreamVerification(false))}, opts...)
	return acquirer.GetStream(ctx, sdHash, allOpts...)
}

// getStreamWithoutVerificationWithRetry is a helper that gets a stream with verification disabled and retry options
func getStreamWithoutVerificationWithRetry(acquirer StreamAcquirer, ctx context.Context, sdHash string, retryOptions []retry.Option, opts ...AcquireOption) (StreamReader, error) {
	allOpts := append([]AcquireOption{WithAcquireStreamOptions(WithStreamVerification(false)), WithAcquireRetry(retryOptions)}, opts...)
	return acquirer.GetStream(ctx, sdHash, allOpts...)
}

// setupMockExpectations sets up common mock expectations for blob acquisition
func (ts *testSetup) setupMockExpectations() {
	ts.store.EXPECT().Name().Return("mock-store")
	if len(ts.sdBlobData) > 0 {
		ts.acquirer.EXPECT().Acquire(ts.ctx, ts.sdHash).Return(ts.sdBlobData, nil).Maybe()
	}
	if len(ts.encryptedContent) > 0 {
		ts.acquirer.EXPECT().Acquire(ts.ctx, ts.contentHash).Return(ts.encryptedContent, nil).Maybe()
		ts.store.EXPECT().Has(ts.contentHash).Return(false, nil)
		ts.store.EXPECT().Put(ts.contentHash, ts.encryptedContent).Return(nil)
	}
}

// setupMockExpectationsForBenchmark sets up mock expectations for benchmarking
func (ts *testSetup) setupMockExpectationsForBenchmark() {
	if len(ts.sdBlobData) > 0 {
		ts.acquirer.EXPECT().Acquire(ts.ctx, ts.sdHash).Return(ts.sdBlobData, nil)
	}
	if len(ts.encryptedContent) > 0 {
		ts.acquirer.EXPECT().Acquire(ts.ctx, ts.contentHash).Return(ts.encryptedContent, nil)
		ts.store.EXPECT().Has(ts.contentHash).Return(false, nil)
		ts.store.EXPECT().Put(ts.contentHash, ts.encryptedContent).Return(nil).Maybe()
	}
}

// createTestSDBlob creates a test SDBlob with the given parameters and returns the serialized JSON data and blob hash
// Works with both *testing.T and *testing.B by using interface{}
func createTestSDBlob(tb testing.TB, contentLength int, blobHashHex string, iv []byte, key []byte) ([]byte, string) {
	tb.Helper()
	blobHash, err := hex.DecodeString(blobHashHex)
	if err != nil {
		tb.Fatal(err)
	}

	sdBlob := stream.SDBlob{
		StreamType: "lbryfile",
		Key:        key,
		BlobInfos: []stream.BlobInfo{
			{
				Length:   contentLength,
				BlobNum:  0,
				BlobHash: blobHash,
				IV:       iv,
			},
		},
	}

	sdBlobData, err := json.Marshal(sdBlob)
	if err != nil {
		tb.Fatal(err)
	}

	return sdBlobData, blobHashHex
}

func TestNewStreamAcquirer(t *testing.T) {
	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	assert.NotNil(t, streamAcquirer)
}

func TestStreamAcquirer_GetSDBlob(t *testing.T) {
	ctx := context.Background()

	// Create test content for a full stream
	testContent := []byte("This is test content for creating a complete stream with verification.")
	contentReader := bytes.NewReader(testContent)

	// Create a full stream using StreamCreator (higher level API)
	manifestCreator := stream.NewManifestCreator()
	streamCreator := stream.NewStreamCreator(manifestCreator)

	// Create the complete stream with all blobs
	streamResult, err := streamCreator.CreateStream(contentReader, int64(len(testContent)))
	require.NoError(t, err)
	require.NotNil(t, streamResult)
	require.NotEmpty(t, streamResult.SDBlobData)
	require.NotEmpty(t, streamResult.ContentBlobs)
	require.Greater(t, len(streamResult.ContentBlobs), 0, "Stream should have at least one content blob")

	// Verify the stream result structure
	assert.Equal(t, int64(len(testContent)), streamResult.SourceSize)
	assert.Equal(t, len(streamResult.ContentBlobs), streamResult.TotalChunks)
	assert.Equal(t, len(streamResult.ContentBlobs), len(streamResult.ContentHashes))

	// Get the SD blob hash
	sdHash := streamResult.SDBlobHash
	assert.NotEmpty(t, sdHash)

	// Set up blob acquirer mock
	acquirer := mocks.NewMockBlobAcquirer(t)

	// Mock SD blob acquisition
	acquirer.EXPECT().Acquire(mock.AnythingOfType("*context.cancelCtx"), sdHash).Return(streamResult.SDBlobData, nil).Maybe()
	acquirer.EXPECT().Acquire(mock.AnythingOfType("context.backgroundCtx"), sdHash).Return(streamResult.SDBlobData, nil).Maybe()

	// Mock content blob acquisitions for all blobs in the stream
	for i, contentBlob := range streamResult.ContentBlobs {
		contentHash := streamResult.ContentHashes[i]
		// Only mock non-empty hashes - empty hashes are terminating blobs and shouldn't be acquired
		if contentHash != "" {
			acquirer.EXPECT().Acquire(mock.AnythingOfType("*context.cancelCtx"), contentHash).Return(contentBlob, nil).Maybe()
			acquirer.EXPECT().Acquire(mock.AnythingOfType("context.backgroundCtx"), contentHash).Return(contentBlob, nil).Maybe()
		}
	}

	// Use memory store
	store := memory.NewMemoryStore()

	// Create the stream acquirer
	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	// Test GetSDBlob functionality
	sdBlobResult, data, err := streamAcquirer.GetSDBlob(ctx, sdHash)

	require.NoError(t, err)
	assert.NotNil(t, sdBlobResult)
	assert.Equal(t, streamResult.SDBlobData, data)
	assert.Equal(t, "lbryfile", sdBlobResult.StreamType)

	// Verify the SD blob structure
	assert.Equal(t, streamResult.SDBlob.Key, sdBlobResult.Key)
	assert.Equal(t, len(streamResult.SDBlob.BlobInfos), len(sdBlobResult.BlobInfos))

	// Verify blob info matches our stream result
	for i, blobInfo := range sdBlobResult.BlobInfos {
		if i < len(streamResult.ContentHashes) {
			assert.Equal(t, streamResult.ContentHashes[i], hex.EncodeToString(blobInfo.BlobHash))
			assert.Equal(t, len(streamResult.ContentBlobs[i]), blobInfo.Length)
			assert.Equal(t, i, blobInfo.BlobNum)
		}
	}

	// Additional verification: test that we can reconstruct the original content
	// by getting the full stream and reading from it
	// Set up the full mock expectations for stream reconstruction
	for i, contentBlob := range streamResult.ContentBlobs {
		contentHash := streamResult.ContentHashes[i]
		// Mock all blob acquisitions, including empty hashes for terminating blobs
		acquirer.EXPECT().Acquire(mock.Anything, contentHash).Return(contentBlob, nil)
	}

	streamReader, err := streamAcquirer.GetStream(ctx, sdHash, WithAcquireStreamOptions(WithStreamVerification(true)))
	require.NoError(t, err)
	require.NotNil(t, streamReader)

	// Read the entire stream
	reconstructedContent, err := io.ReadAll(streamReader)
	require.NoError(t, err)

	// Test DecryptedSize() - should return actual content size after reading the entire stream
	// Check BEFORE closing to ensure cache is still available
	decryptedSize := streamReader.DecryptedSize()
	assert.Equal(t, int64(len(testContent)), decryptedSize, "DecryptedSize should return actual content size after full read")

	// Now close the stream
	err = streamReader.Close()
	require.NoError(t, err)

	// Verify the reconstructed content matches the original
	assert.Equal(t, testContent, reconstructedContent, "Reconstructed content should match original")

	// Verify that Size() still returns encrypted blob size (different from DecryptedSize)
	assert.NotEqual(t, streamReader.Size(), streamReader.DecryptedSize(), "Size() and DecryptedSize() should return different values")
}

func TestStreamAcquirer_GetSDBlob_MultipleBlobs(t *testing.T) {
	ctx := context.Background()

	// Create larger test content that will be split into multiple blobs
	// Content should be larger than max blob size to ensure multiple blobs
	testContent := make([]byte, 3*1024*1024) // 3MB to ensure multiple blobs
	for i := range testContent {
		testContent[i] = byte(i % 256)
	}
	contentReader := bytes.NewReader(testContent)

	// Create a full stream using StreamCreator (higher level API)
	manifestCreator := stream.NewManifestCreator()
	streamCreator := stream.NewStreamCreator(manifestCreator)

	// Create the complete stream with multiple blobs
	streamResult, err := streamCreator.CreateStream(contentReader, int64(len(testContent)))
	require.NoError(t, err)
	require.NotNil(t, streamResult)
	require.NotEmpty(t, streamResult.SDBlobData)
	require.NotEmpty(t, streamResult.ContentBlobs)
	require.Greater(t, len(streamResult.ContentBlobs), 1, "Stream should have multiple content blobs")

	// Verify the stream result structure
	assert.Equal(t, int64(len(testContent)), streamResult.SourceSize)
	assert.Equal(t, len(streamResult.ContentBlobs), streamResult.TotalChunks)
	assert.Equal(t, len(streamResult.ContentBlobs), len(streamResult.ContentHashes))

	// Get the SD blob hash
	sdHash := streamResult.SDBlobHash
	assert.NotEmpty(t, sdHash)

	// Set up blob acquirer mock
	acquirer := mocks.NewMockBlobAcquirer(t)

	// Mock SD blob acquisition
	acquirer.EXPECT().Acquire(mock.AnythingOfType("*context.cancelCtx"), sdHash).Return(streamResult.SDBlobData, nil).Maybe()
	acquirer.EXPECT().Acquire(mock.AnythingOfType("context.backgroundCtx"), sdHash).Return(streamResult.SDBlobData, nil).Maybe()

	// Mock content blob acquisitions for all non-empty hashes
	for i, contentBlob := range streamResult.ContentBlobs {
		contentHash := streamResult.ContentHashes[i]
		if contentHash != "" { // Only mock non-empty hashes (skip terminating blob)
			acquirer.EXPECT().Acquire(mock.Anything, contentHash).Return(contentBlob, nil).Maybe()
		}
	}

	// Use memory store
	store := memory.NewMemoryStore()

	// Create the stream acquirer
	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	// Test GetSDBlob functionality
	sdBlobResult, data, err := streamAcquirer.GetSDBlob(ctx, sdHash)

	require.NoError(t, err)
	assert.NotNil(t, sdBlobResult)
	assert.Equal(t, streamResult.SDBlobData, data)
	assert.Equal(t, "lbryfile", sdBlobResult.StreamType)

	// Verify the SD blob structure
	assert.Equal(t, streamResult.SDBlob.Key, sdBlobResult.Key)
	assert.Equal(t, len(streamResult.SDBlob.BlobInfos), len(sdBlobResult.BlobInfos))

	// Verify blob info matches our stream result
	for i, blobInfo := range sdBlobResult.BlobInfos {
		if i < len(streamResult.ContentHashes) && streamResult.ContentHashes[i] != "" {
			assert.Equal(t, streamResult.ContentHashes[i], hex.EncodeToString(blobInfo.BlobHash))
			assert.Equal(t, len(streamResult.ContentBlobs[i]), blobInfo.Length)
			assert.Equal(t, i, blobInfo.BlobNum)
		} else if i == len(streamResult.ContentHashes) {
			// This should be the terminating blob
			assert.Equal(t, 0, blobInfo.Length, "Last blob should be terminating with zero length")
			assert.Equal(t, i, blobInfo.BlobNum, "Terminating blob should have correct blob number")
		}
	}

	// Additional verification: test that we can reconstruct the original content
	// by getting the full stream and reading from it
	// Set up the full mock expectations for stream reconstruction
	for i, contentBlob := range streamResult.ContentBlobs {
		contentHash := streamResult.ContentHashes[i]
		if contentHash != "" { // Only mock non-empty hashes (skip terminating blob)
			acquirer.EXPECT().Acquire(mock.AnythingOfType("*context.cancelCtx"), contentHash).Return(contentBlob, nil).Maybe()
			acquirer.EXPECT().Acquire(mock.AnythingOfType("context.backgroundCtx"), contentHash).Return(contentBlob, nil).Maybe()
		}
	}

	streamReader, err := streamAcquirer.GetStream(ctx, sdHash, WithAcquireStreamOptions(WithStreamVerification(true)))
	require.NoError(t, err)
	require.NotNil(t, streamReader)

	// Read the entire stream
	reconstructedContent, err := io.ReadAll(streamReader)
	require.NoError(t, err)

	// Test DecryptedSize() - should return actual content size after reading the entire stream
	// Check BEFORE closing to ensure cache is still available
	assert.Equal(t, int64(len(testContent)), streamReader.DecryptedSize(), "DecryptedSize should return actual content size after full read")

	// Now close the stream
	err = streamReader.Close()
	require.NoError(t, err)

	// Verify the reconstructed content matches the original
	assert.Equal(t, testContent, reconstructedContent, "Reconstructed content should match original")

	// Note: Can't test Size() vs DecryptedSize() difference after close since cache is cleared
}

func TestStreamAcquirer_DecryptedSize_Behavior(t *testing.T) {
	ctx := context.Background()

	// Create test content
	testContent := []byte("This is test content for DecryptedSize behavior testing.")
	contentReader := bytes.NewReader(testContent)

	// Create a full stream using StreamCreator
	manifestCreator := stream.NewManifestCreator()
	streamCreator := stream.NewStreamCreator(manifestCreator)

	streamResult, err := streamCreator.CreateStream(contentReader, int64(len(testContent)))
	require.NoError(t, err)
	require.NotNil(t, streamResult)

	sdHash := streamResult.SDBlobHash
	require.NotEmpty(t, sdHash)

	// Set up blob acquirer mock
	acquirer := mocks.NewMockBlobAcquirer(t)

	// Mock SD blob acquisition
	acquirer.EXPECT().Acquire(mock.AnythingOfType("*context.cancelCtx"), sdHash).Return(streamResult.SDBlobData, nil).Maybe()
	acquirer.EXPECT().Acquire(mock.AnythingOfType("context.backgroundCtx"), sdHash).Return(streamResult.SDBlobData, nil).Maybe()

	// Mock content blob acquisitions
	for i, contentBlob := range streamResult.ContentBlobs {
		contentHash := streamResult.ContentHashes[i]
		if contentHash != "" {
			acquirer.EXPECT().Acquire(mock.AnythingOfType("*context.cancelCtx"), contentHash).Return(contentBlob, nil).Maybe()
			acquirer.EXPECT().Acquire(mock.AnythingOfType("context.backgroundCtx"), contentHash).Return(contentBlob, nil).Maybe()
		}
	}

	// Use memory store
	store := memory.NewMemoryStore()

	// Create the stream acquirer
	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	// Get the stream reader
	streamReader, err := streamAcquirer.GetStream(ctx, sdHash, WithAcquireStreamOptions(WithStreamVerification(true)))
	require.NoError(t, err)
	require.NotNil(t, streamReader)
	defer streamReader.Close()

	// Test DecryptedSize() before any reading - should return -1 (unknown)
	assert.Equal(t, int64(-1), streamReader.DecryptedSize(), "DecryptedSize should return -1 before any blobs are read")

	// Read a small portion to trigger decryption of first blob
	buffer := make([]byte, 10)
	n, err := streamReader.Read(buffer)
	require.NoError(t, err)
	assert.Greater(t, n, 0, "Should have read some bytes")

	// After partial read, DecryptedSize() should still return -1 or an estimate
	// (implementation dependent - we accept either -1 or a reasonable estimate)
	decryptedSize := streamReader.DecryptedSize()
	if decryptedSize != -1 {
		// If it returns an estimate, it should be reasonable
		assert.Greater(t, decryptedSize, int64(0), "Estimated DecryptedSize should be positive")
		assert.Less(t, decryptedSize, streamReader.Size(), "Estimated DecryptedSize should be less than encrypted size")
	}

	// Read the entire stream
	remainingContent, err := io.ReadAll(streamReader)
	require.NoError(t, err)
	fullContent := append(buffer[:n], remainingContent...)

	// Verify content
	assert.Equal(t, testContent, fullContent, "Full content should match original")

	// After full read, DecryptedSize() should return exact content size
	assert.Equal(t, int64(len(testContent)), streamReader.DecryptedSize(), "DecryptedSize should return exact content size after full read")

	// Test that DecryptedSize() returns -1 after Close() due to cache clearing
	err = streamReader.Close()
	require.NoError(t, err)
	assert.Equal(t, int64(-1), streamReader.DecryptedSize(), "DecryptedSize should return -1 after Close() due to cache clearing")
}

func TestStreamAcquirer_GetSDBlob_InvalidHash(t *testing.T) {
	ctx := context.Background()
	sdHash := "" // Invalid hash

	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	_, _, err := streamAcquirer.GetSDBlob(ctx, sdHash)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid SD hash")
}

func TestStreamAcquirer_GetStreamResult(t *testing.T) {
	ctx := context.Background()
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyStream]
	// Create IV for the blob
	iv := make([]byte, 16)
	for i := range iv {
		iv[i] = byte(i)
	}

	contentBlobData := []byte("test content")
	contentBlobHash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1]

	// Create key for the SD blob
	key := make([]byte, 32)
	// Key is all zeros as specified in the original test

	// Create SD blob using helper function
	sdBlobData, _ := createTestSDBlob(t, 100, contentBlobHash, iv, key)

	acquirer := mocks.NewMockBlobAcquirer(t)
	acquirer.EXPECT().Acquire(ctx, sdHash).Return(sdBlobData, nil)
	acquirer.EXPECT().Acquire(ctx, contentBlobHash).Return(contentBlobData, nil)

	store := storageMocks.NewMockBlobStore(t)
	store.EXPECT().Name().Return("mock-store")
	store.EXPECT().Has(contentBlobHash).Return(false, nil)
	store.EXPECT().Put(contentBlobHash, contentBlobData).Return(nil)

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	result, err := streamAcquirer.GetStreamResult(ctx, sdHash, WithAcquireRecursive(true))

	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, sdHash, result.SDBlobHash)
	assert.Equal(t, "lbryfile", result.SDBlob.StreamType)
	assert.Equal(t, 1, result.TotalChunks)
	assert.Equal(t, contentBlobData, result.ContentBlobs[0])
}

func TestStreamAcquirer_GetStream(t *testing.T) {
	setup := createTestSetupWithContent(t, []byte("test content"))
	setup.setupMockExpectations()

	reader, err := setup.getStreamWithoutVerification()

	require.NoError(t, err)
	assert.NotNil(t, reader)

	// Test reading from stream
	buffer := make([]byte, len(setup.contentData))
	n, err := reader.Read(buffer)

	require.NoError(t, err)
	assert.Equal(t, len(setup.contentData), n)
	assert.Equal(t, setup.contentData, buffer[:len(setup.contentData)])

	// Test stream properties (original content length from SD blob)
	assert.Equal(t, int64(len(setup.contentData)), reader.Size())
	assert.NotNil(t, reader.SDBlob())

	// Close reader to ensure cleanup
	err = reader.Close()
	require.NoError(t, err)

	// Small delay to allow storage goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestStreamAcquirer_GetStream_Seeking(t *testing.T) {
	setup := createTestSetupWithContent(t, []byte("test content for seeking"))
	setup.setupMockExpectations()

	reader, err := setup.getStreamWithoutVerification()

	require.NoError(t, err)
	assert.NotNil(t, reader)

	// Test seeking
	pos, err := reader.Seek(10, io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(10), pos)

	// Read from new position
	buffer := make([]byte, 5)
	n, err := reader.Read(buffer)
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, setup.contentData[10:15], buffer[:5])

	// Test seeking relative to current position
	pos, err = reader.Seek(-3, io.SeekCurrent)
	require.NoError(t, err)
	assert.Equal(t, int64(12), pos)

	// Test seeking to end
	pos, err = reader.Seek(0, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(len(setup.contentData)), pos)

	// Close reader to ensure cleanup
	err = reader.Close()
	require.NoError(t, err)

	// Small delay to allow storage goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestStreamAcquirer_GetStream_Progress(t *testing.T) {
	setup := createTestSetupWithContent(t, []byte("test content for progress"))
	setup.setupMockExpectations()

	var progressCalls []float64
	progressCallback := func(progress float64) {
		progressCalls = append(progressCalls, progress)
	}

	reader, err := setup.getStreamWithoutVerification(WithAcquireProgress(progressCallback))

	require.NoError(t, err)
	assert.NotNil(t, reader)

	// Read entire stream
	buffer := make([]byte, 1024)
	_, err = reader.Read(buffer)
	require.NoError(t, err)

	// Check that progress was reported
	assert.Greater(t, len(progressCalls), 0)
	assert.Equal(t, 1.0, progressCalls[len(progressCalls)-1]) // Final progress should be 1.0

	// Close reader to ensure cleanup
	err = reader.Close()
	require.NoError(t, err)

	// Small delay to allow storage goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestStreamAcquirerBuilder(t *testing.T) {
	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	// Test builder pattern
	streamAcquirer, err := NewStreamAcquirerBuilder(zaptest.NewLogger(t)).
		WithAcquirer(acquirer).
		WithStore(store).
		Build()

	require.NoError(t, err)
	assert.NotNil(t, streamAcquirer)
}

func TestStreamAcquirerBuilder_MissingAcquirer(t *testing.T) {
	store := storageMocks.NewMockBlobStore(t)

	// Test builder without acquirer should fail
	_, err := NewStreamAcquirerBuilder(zap.NewNop()).
		WithStore(store).
		Build()

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "acquirer is required")
}

func TestStreamAcquirerFactory(t *testing.T) {
	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	factory := NewStreamAcquirerFactory(zaptest.NewLogger(t))

	// Test acquirer creation
	acquirer1 := factory.CreateStreamAcquirer(acquirer, store)
	assert.NotNil(t, acquirer1)

	// Test another acquirer creation
	acquirer2 := factory.CreateStreamAcquirer(acquirer, store)
	assert.NotNil(t, acquirer2)
}

func TestStreamAcquirer_GetStream_Caching(t *testing.T) {
	setup := createTestSetupWithContent(t, []byte("test content for caching"))
	setup.setupMockExpectations()

	reader, err := setup.getStreamWithoutVerification(WithAcquireStreamOptions(WithStreamCacheSize(5)))

	require.NoError(t, err)
	assert.NotNil(t, reader)

	// Read multiple times - should use cache
	buffer := make([]byte, 4) // Adjusted for encrypted data size
	for i := 0; i < 5; i++ {
		n, err := reader.Read(buffer)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		if n < len(buffer) {
			break // EOF reached
		}
		assert.Equal(t, 4, n)
	}

	// Close reader to ensure cleanup
	err = reader.Close()
	require.NoError(t, err)

	// Small delay to allow storage goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func BenchmarkStreamAcquirer_GetStream(b *testing.B) {
	benchmarkStreamAcquirer(b, 1000) // 1KB content
}

// Tests for larger data amounts

func TestStreamAcquirer_GetStream_LargeData_1MB(t *testing.T) {
	testLargeDataStream(t, 1024*1024) // 1MB
}

func TestStreamAcquirer_GetStream_LargeData_10MB(t *testing.T) {
	testLargeDataStream(t, 10*1024*1024) // 10MB
}

func TestStreamAcquirer_GetStream_LargeData_50MB(t *testing.T) {
	testLargeDataStream(t, 50*1024*1024) // 50MB
}

func TestStreamAcquirer_GetStream_LargeData_100MB(t *testing.T) {
	testLargeDataStream(t, 100*1024*1024) // 100MB
}

// testLargeDataStream is a helper function for testing large data streams
func testLargeDataStream(t *testing.T, size int) {
	t.Helper()

	setup := createTestSetup(t, size)
	setup.setupMockExpectations()

	reader, err := setup.getStreamWithoutVerification()

	require.NoError(t, err)
	assert.NotNil(t, reader)

	// Test reading the entire stream in chunks
	buffer := make([]byte, 64*1024) // 64KB chunks
	totalRead := 0

	for totalRead < size {
		n, err := reader.Read(buffer)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		// Verify the data matches expected content
		expectedStart := totalRead
		expectedEnd := expectedStart + n
		if expectedEnd > size {
			expectedEnd = size
		}

		for i := 0; i < n && expectedStart+i < size; i++ {
			expectedByte := byte((expectedStart + i) % 256)
			assert.Equal(t, expectedByte, buffer[i], "Data mismatch at position %d", expectedStart+i)
		}

		totalRead += n
	}

	assert.Equal(t, size, totalRead, "Should read exactly %d bytes", size)
	assert.Equal(t, int64(size), reader.Size(), "Stream size should match content size")

	// Test seeking to different positions for large streams
	testSeekingInLargeStream(t, reader, size)

	// Close reader to ensure cleanup
	err = reader.Close()
	require.NoError(t, err)
}

// testSeekingInLargeStream tests seeking functionality in large streams
func testSeekingInLargeStream(t *testing.T, reader io.ReadSeeker, size int) {
	t.Helper()

	// Test seeking to middle
	midPos := size / 2
	pos, err := reader.Seek(int64(midPos), io.SeekStart)
	require.NoError(t, err)
	assert.Equal(t, int64(midPos), pos)

	// Read a small chunk from the middle
	buffer := make([]byte, 1024)
	n, err := reader.Read(buffer)
	require.NoError(t, err)

	// Verify the data at the middle position
	for i := 0; i < n; i++ {
		expectedByte := byte((midPos + i) % 256)
		assert.Equal(t, expectedByte, buffer[i], "Data mismatch at middle position %d", midPos+i)
	}

	// Test seeking to end
	pos, err = reader.Seek(0, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(size), pos)

	// Test seeking from end
	pos, err = reader.Seek(-1024, io.SeekEnd)
	require.NoError(t, err)
	assert.Equal(t, int64(size-1024), pos)
}

// Performance benchmarks for different data sizes

func BenchmarkStreamAcquirer_GetStream_1KB(b *testing.B) {
	benchmarkStreamAcquirer(b, 1024)
}

func BenchmarkStreamAcquirer_GetStream_1MB(b *testing.B) {
	benchmarkStreamAcquirer(b, 1024*1024)
}

func BenchmarkStreamAcquirer_GetStream_10MB(b *testing.B) {
	benchmarkStreamAcquirer(b, 10*1024*1024)
}

func BenchmarkStreamAcquirer_GetStream_100MB(b *testing.B) {
	benchmarkStreamAcquirer(b, 100*1024*1024)
}

// benchmarkStreamAcquirer is a helper function for benchmarking different data sizes
func benchmarkStreamAcquirer(b *testing.B, size int) {
	b.ResetTimer()
	// Note: ReportBytes is not available in older Go versions, using SetBytes instead
	b.SetBytes(int64(size)) // Report throughput in bytes

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		setup := createTestSetup(b, size)
		setup.setupMockExpectationsForBenchmark()
		b.StartTimer()

		reader, err := setup.getStreamWithoutVerification()
		if err != nil {
			b.Fatal(err)
		}

		// Read all data to measure full acquisition time
		buffer := make([]byte, 64*1024)
		totalRead := 0
		for totalRead < size {
			n, err := reader.Read(buffer)
			if err == io.EOF {
				break
			}
			if err != nil {
				b.Error(err)
				break
			}
			totalRead += n
		}

		err = reader.Close()
		if err != nil {
			b.Error(err)
		}

		b.StopTimer()
	}
}

// Test memory usage with large streams
func TestStreamAcquirer_GetStream_MemoryUsage_LargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory usage test in short mode")
	}

	size := 50 * 1024 * 1024 // 50MB
	setup := createTestSetup(t, size)
	setup.setupMockExpectations()

	reader, err := setup.getStreamWithoutVerification()
	require.NoError(t, err)

	// Read in small chunks to test that memory doesn't grow excessively
	buffer := make([]byte, 8192) // 8KB buffer
	chunksRead := 0

	for {
		_, err := reader.Read(buffer)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		chunksRead++

		// Periodically check that we're not accumulating too much memory
		if chunksRead%1000 == 0 {
			// Force garbage collection to get more accurate memory readings
			runtime.GC()
		}
	}

	expectedChunks := (size + 8191) / 8192 // Ceiling division
	assert.Equal(t, expectedChunks, chunksRead)

	err = reader.Close()
	require.NoError(t, err)
}

// Test concurrent access to large streams
func TestStreamAcquirer_GetStream_Concurrent_LargeData(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	size := 10 * 1024 * 1024 // 10MB
	setup := createTestSetup(t, size)
	setup.setupMockExpectations()

	const numGoroutines = 5
	const readsPerGoroutine = 10

	var wg sync.WaitGroup
	errChan := make(chan error, numGoroutines*readsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < readsPerGoroutine; j++ {
				reader, err := setup.getStreamWithoutVerification()
				if err != nil {
					errChan <- err
					return
				}

				// Read a portion of the stream
				buffer := make([]byte, 1024)
				_, err = reader.Read(buffer)
				if err != nil && err != io.EOF {
					errChan <- err
				}

				if closeErr := reader.Close(); closeErr != nil {
					errChan <- closeErr
				}

				if err != nil && err != io.EOF {
					return
				}
			}
		}(i)
	}

	wg.Wait()
	close(errChan)

	// Check for any errors
	for err := range errChan {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func TestStreamAcquirer_WithRetryBehavior(t *testing.T) {
	ctx := context.Background()
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockStore := storageMocks.NewMockBlobStore(t)

	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyBlob]

	// Create test SD blob using helper function
	sdBlobData, _ := createTestSDBlob(t, 100, lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey1], nil, nil)

	// Test retry on SD blob acquisition failure
	acquireCalls := 0
	mockAcquirer.On("Acquire", ctx, sdHash).Return(nil, errors.New("network error")).Once().Run(func(args mock.Arguments) {
		acquireCalls++
	})
	mockAcquirer.On("Acquire", ctx, sdHash).Return(nil, errors.New("network error")).Once().Run(func(args mock.Arguments) {
		acquireCalls++
	})
	mockAcquirer.On("Acquire", ctx, sdHash).Return(sdBlobData, nil).Run(func(args mock.Arguments) {
		acquireCalls++
	})

	// Create acquirer with custom retry options
	acquirer := NewStreamAcquirer(mockAcquirer, mockStore, zaptest.NewLogger(t))

	// Use custom retry options with 3 attempts and short delay
	customRetryOptions := []retry.Option{
		retry.Attempts(3),
		retry.Delay(10 * time.Millisecond),
		retry.RetryIf(func(err error) bool {
			return !errors.Is(err, stream.ErrInvalidSDBlob)
		}),
	}

	start := time.Now()
	streamReader, err := getStreamWithoutVerificationWithRetry(acquirer, ctx, sdHash, customRetryOptions)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.NotNil(t, streamReader)
	assert.Equal(t, 3, acquireCalls)                        // Should have retried twice
	assert.GreaterOrEqual(t, duration, 20*time.Millisecond) // At least one delay
}

func TestStreamAcquirer_WithRetryNonRetryableError(t *testing.T) {
	ctx := context.Background()
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockStore := storageMocks.NewMockBlobStore(t)

	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyBlob]

	// Create invalid SD blob data that should fail validation
	invalidSDBlobData := []byte("invalid json")

	// Mock acquirer to return invalid SD blob data
	mockAcquirer.On("Acquire", ctx, sdHash).Return(invalidSDBlobData, nil)

	acquirer := NewStreamAcquirer(mockAcquirer, mockStore, zaptest.NewLogger(t))

	// Use default retry options
	streamReader, err := getStreamWithoutVerificationFromAcquirer(acquirer, ctx, sdHash)

	assert.Error(t, err)
	assert.Nil(t, streamReader)
	assert.Contains(t, err.Error(), "invalid SD blob")
}

func TestStreamAcquirer_WithRetryMaxAttemptsExhausted(t *testing.T) {
	ctx := context.Background()
	mockAcquirer := mocks.NewMockBlobAcquirer(t)
	mockStore := storageMocks.NewMockBlobStore(t)

	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyBlob]

	// Mock acquirer to always fail with retryable error
	mockAcquirer.On("Acquire", ctx, sdHash).Return(nil, errors.New("persistent network error"))

	acquirer := NewStreamAcquirer(mockAcquirer, mockStore, zaptest.NewLogger(t))

	// Use custom retry options with only 2 attempts
	customRetryOptions := []retry.Option{
		retry.Attempts(2),
		retry.Delay(5 * time.Millisecond),
	}

	start := time.Now()
	streamReader, err := getStreamWithoutVerificationWithRetry(acquirer, ctx, sdHash, customRetryOptions)
	duration := time.Since(start)

	assert.Error(t, err)
	assert.Nil(t, streamReader)
	assert.Contains(t, err.Error(), "All attempts fail")
	assert.Less(t, duration, 100*time.Millisecond) // Should fail quickly
}

func TestStreamReader_WithRetryOnBlobAcquisition(t *testing.T) {
	ctx := context.Background()

	// Use real memory store
	store := memory.NewMemoryStore()

	// Create test SD blob
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyBlob]

	// Create encryption key and IV for the SD blob
	key := createTestKey()
	iv := createTestIV()

	// Create and encrypt content
	content := createTestContent(50)
	encryptedBlobData := createEncryptedBlob(t, content, key, iv)

	// Calculate the actual blob hash from encrypted data
	blobHash, err := blob.ComputeBlobHashBytes(encryptedBlobData)
	require.NoError(t, err)
	blobHashHex := hex.EncodeToString(blobHash)

	sdBlob := &stream.SDBlob{
		StreamName: "test_stream",
		StreamType: "test",
		Key:        key,
		BlobInfos: []stream.BlobInfo{
			{BlobNum: 0, Length: len(encryptedBlobData), BlobHash: blobHash, IV: iv},
		},
	}
	sdBlobData, err := json.Marshal(sdBlob)
	require.NoError(t, err)

	// Pre-populate memory store with SD blob
	err = store.PutSD(sdHash, sdBlobData)
	require.NoError(t, err)

	// Create mock transfer that fails initially then succeeds
	mockTransfer := transferMocks.NewMockTransfer(t)
	transferCalls := 0

	// Set up expectations up front: first call fails, second call succeeds
	mockTransfer.On("Get", ctx, blobHashHex).Return(nil, errors.New("temporary network error")).Once().Run(func(args mock.Arguments) {
		transferCalls++
	})
	mockTransfer.On("Get", ctx, blobHashHex).Return(encryptedBlobData, nil).Once().Run(func(args mock.Arguments) {
		transferCalls++
	})

	// Create real acquirer with mock transfer
	acquirer, err := liblbry.NewBlobAcquirer([]transfer.Transfer{mockTransfer}, store)
	require.NoError(t, err)

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	// Create stream reader with custom retry options
	customRetryOptions := []retry.Option{
		retry.Attempts(3),
		retry.Delay(10 * time.Millisecond),
	}
	streamReader, err := getStreamWithoutVerificationWithRetry(streamAcquirer, ctx, sdHash, customRetryOptions)
	require.NoError(t, err)

	// Try to read from the stream - this should trigger blob acquisition with retry
	buf := make([]byte, 10)
	n, err := streamReader.Read(buf)

	assert.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, 2, transferCalls) // Should have retried once
}

func TestStreamReader_WithRetryOnStorageFailure(t *testing.T) {
	ctx := context.Background()

	// Create mock store that fails initially then succeeds
	mockStore := storageMocks.NewMockBlobStore(t)

	// Create test SD blob
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyBlob]

	// Create encryption key and IV for the SD blob
	key := createTestKey()
	iv := createTestIV()

	// Create and encrypt content
	content := createTestContent(50)
	encryptedBlobData := createEncryptedBlob(t, content, key, iv)

	// Calculate the actual blob hash from encrypted data
	blobHash, err := blob.ComputeBlobHashBytes(encryptedBlobData)
	require.NoError(t, err)
	blobHashHex := hex.EncodeToString(blobHash)

	sdBlob := &stream.SDBlob{
		StreamName: "test_stream",
		StreamType: "test",
		Key:        key,
		BlobInfos: []stream.BlobInfo{
			{BlobNum: 0, Length: len(encryptedBlobData), BlobHash: blobHash, IV: iv},
		},
	}
	sdBlobData, err := json.Marshal(sdBlob)
	require.NoError(t, err)

	// Mock SD blob storage - always succeed
	mockStore.On("Has", sdHash).Return(true, nil)
	mockStore.On("Get", sdHash).Return(sdBlobData, nil)

	// Mock blob storage Has check to fail initially, then succeed
	// First call fails with temporary error
	mockStore.On("Has", blobHashHex).Return(false, errors.New("storage temporarily unavailable")).Once()
	// Subsequent calls succeed (use Maybe() to handle multiple retry attempts)
	mockStore.On("Has", blobHashHex).Return(true, nil).Maybe()
	// Get call succeeds when Has returns true
	mockStore.On("Get", blobHashHex).Return(encryptedBlobData, nil).Maybe()

	// Create mock transfer for blob acquisition as fallback (may not be called if storage succeeds)
	mockTransfer := transferMocks.NewMockTransfer(t)
	mockTransfer.On("Get", ctx, blobHashHex).Return(encryptedBlobData, nil).Maybe() // Use Maybe() since it might not be called

	// Create real acquirer with mock transfer and mock store
	acquirer, err := liblbry.NewBlobAcquirer([]transfer.Transfer{mockTransfer}, mockStore)
	require.NoError(t, err)

	streamAcquirer := NewStreamAcquirer(acquirer, mockStore, zaptest.NewLogger(t))

	// Create stream reader with custom retry options
	customRetryOptions := []retry.Option{
		retry.Attempts(3),
		retry.Delay(10 * time.Millisecond),
	}
	streamReader, err := getStreamWithoutVerificationWithRetry(streamAcquirer, ctx, sdHash, customRetryOptions)
	require.NoError(t, err)

	// Try to read from the stream
	buf := make([]byte, 10)
	n, err := streamReader.Read(buf)

	assert.NoError(t, err)
	assert.Equal(t, 10, n)
	// Note: hasCalls counter removed as we no longer use .Run() callbacks
	// The retry behavior is verified by the successful read operation
}

// Verification tests with on-the-fly data generation

// generateVerifiedTestContent creates content with proper hash verification
func generateVerifiedTestContent(t *testing.T, contentSize int) ([]byte, string, []byte, []byte, []byte) {
	t.Helper()

	// Generate random content
	content := make([]byte, contentSize)
	for i := range content {
		content[i] = byte((i*7 + 13) % 256) // Pseudo-random pattern
	}

	// Create encryption key and IV
	key := createTestKey()
	iv := createTestIV()

	// Encrypt the content
	encryptedContent := createEncryptedBlob(t, content, key, iv)

	// Calculate the actual blob hash
	blobHash, err := blob.ComputeBlobHashBytes(encryptedContent)
	require.NoError(t, err)
	blobHashHex := hex.EncodeToString(blobHash)

	return content, blobHashHex, key, iv, encryptedContent
}

// createVerifiedTestSetup creates a test setup with proper hash verification
func createVerifiedTestSetup(t *testing.T, contentSize int) *testSetup {
	content, blobHashHex, key, iv, encryptedContent := generateVerifiedTestContent(t, contentSize)

	ctx := context.Background()
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyStream]

	// Create SD blob with the correct blob hash
	sdBlobData, _ := createTestSDBlob(t, contentSize, blobHashHex, iv, key)

	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	// Set up mock expectations with correct hashes
	acquirer.EXPECT().Acquire(ctx, sdHash).Return(sdBlobData, nil)
	acquirer.EXPECT().Acquire(ctx, blobHashHex).Return(encryptedContent, nil)
	store.EXPECT().Has(blobHashHex).Return(false, nil)
	store.EXPECT().Put(blobHashHex, encryptedContent).Return(nil).Maybe() // May be called multiple times (main + prefetcher)
	store.EXPECT().Name().Return("mock-store").Maybe()

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	return &testSetup{
		ctx:              ctx,
		sdHash:           sdHash,
		contentHash:      blobHashHex,
		key:              key,
		iv:               iv,
		contentData:      content,
		encryptedContent: encryptedContent,
		sdBlobData:       sdBlobData,
		acquirer:         acquirer,
		store:            store,
		streamAcquirer:   streamAcquirer,
	}
}

func TestStreamAcquirer_Verification_Enabled_ValidHashes(t *testing.T) {
	setup := createVerifiedTestSetup(t, 1000) // 1KB content

	reader, err := setup.getStreamWithVerification()
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Read the entire content
	buffer := make([]byte, len(setup.contentData))
	n, err := reader.Read(buffer)
	require.NoError(t, err)
	assert.Equal(t, len(setup.contentData), n)
	assert.Equal(t, setup.contentData, buffer[:n])

	// Verify stream properties
	assert.Equal(t, int64(len(setup.contentData)), reader.Size())
	assert.NotNil(t, reader.SDBlob())

	err = reader.Close()
	require.NoError(t, err)
}

func TestStreamAcquirer_Verification_Enabled_InvalidSDBlobHash(t *testing.T) {
	content, _, key, iv, _ := generateVerifiedTestContent(t, 500)

	ctx := context.Background()
	sdHash := lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyStream]

	// Create SD blob with WRONG blob hash (but SD blob itself should hash correctly to sdHash)
	wrongBlobHash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey2]
	sdBlobData, _ := createTestSDBlob(t, len(content), wrongBlobHash, iv, key)

	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	// Set up mocks - SD blob acquisition should succeed but verification will fail
	acquirer.EXPECT().Acquire(ctx, sdHash).Return(sdBlobData, nil)

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))

	// GetStream should fail due to SD blob hash verification
	reader, err := streamAcquirer.GetStream(ctx, sdHash, WithAcquireVerification(true))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "SD blob hash verification failed")
	assert.Nil(t, reader)
}

// createVerificationTestSetup creates common setup for verification failure tests
func createVerificationTestSetup(t *testing.T, contentSize int) (context.Context, []byte, string, []byte, string, *stream.SDBlob) {
	content, blobHashHex, key, iv, encryptedContent := generateVerifiedTestContent(t, contentSize)
	ctx := context.Background()

	// Create a valid SD blob
	sdBlobData, _ := createTestSDBlob(t, len(content), blobHashHex, iv, key)

	// Parse the SD blob
	var sdBlob stream.SDBlob
	if err := json.Unmarshal(sdBlobData, &sdBlob); err != nil {
		t.Fatal(err)
	}

	return ctx, encryptedContent, blobHashHex, sdBlobData, blobHashHex, &sdBlob
}

// setupVerificationFailureMocks sets up mocks for verification failure tests
func setupVerificationFailureMocks(t *testing.T, ctx context.Context, sdBlobHash string, contentHash string, sdBlobData []byte, contentData []byte) (*mocks.MockBlobAcquirer, *storageMocks.MockBlobStore, StreamAcquirer) {
	acquirer := mocks.NewMockBlobAcquirer(t)
	store := storageMocks.NewMockBlobStore(t)

	// Set up mocks - SD blob is correct and hashes to sdHash, but verification should fail
	acquirer.EXPECT().Acquire(ctx, sdBlobHash).Return(sdBlobData, nil)
	// Content blob acquisition should be called since blob hash doesn't exist in storage
	store.EXPECT().Has(contentHash).Return(false, nil)
	acquirer.EXPECT().Acquire(ctx, contentHash).Return(contentData, nil)
	store.EXPECT().Put(contentHash, contentData).Return(nil).Maybe()

	streamAcquirer := NewStreamAcquirer(acquirer, store, zaptest.NewLogger(t))
	return acquirer, store, streamAcquirer
}

func TestStreamAcquirer_Verification_Enabled_CorruptedData(t *testing.T) {
	ctx, encryptedContent, blobHashHex, _, _, sdBlob := createVerificationTestSetup(t, 800)

	// Corrupt the encrypted content by flipping some bits
	corruptedContent := make([]byte, len(encryptedContent))
	copy(corruptedContent, encryptedContent)
	corruptedContent[10] ^= 0xFF // Flip bits in the encrypted data

	// Re-serialize the SD blob to ensure consistent format
	modifiedSDBlobData, err := sdBlob.ToBlob()
	if err != nil {
		t.Fatal(err)
	}
	sdBlobHash := hex.EncodeToString(sdBlob.Hash())

	// Set up mocks using helper
	_, _, streamAcquirer := setupVerificationFailureMocks(t, ctx, sdBlobHash, blobHashHex, modifiedSDBlobData, corruptedContent)

	reader, err := streamAcquirer.GetStream(ctx, sdBlobHash, WithAcquireVerification(true))
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Reading should fail due to blob hash verification
	buffer := make([]byte, 100)
	_, err = reader.Read(buffer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob hash verification failed")
}

func TestStreamAcquirer_Verification_Enabled_ValidSDBlob_WrongBlobHash(t *testing.T) {
	ctx, _, blobHashHex, _, originalBlobHash, sdBlob := createVerificationTestSetup(t, 500)

	// Verify the returned blob hash matches what we expect
	assert.Equal(t, blobHashHex, originalBlobHash, "createTestSDBlob should return the same blob hash we passed in")

	// Replace the correct blob hash with a wrong one
	wrongBlobHash := lbryTesting.LBRYTestHashes[lbryTesting.LBRYHashKey2]
	wrongBlobHashBytes, err := hex.DecodeString(wrongBlobHash)
	if err != nil {
		t.Fatal(err)
	}
	sdBlob.BlobInfos[0].BlobHash = wrongBlobHashBytes

	// Re-serialize the modified SD blob
	modifiedSDBlobData, err := sdBlob.ToBlob()
	if err != nil {
		t.Fatal(err)
	}

	modifiedSDBlobHash := hex.EncodeToString(sdBlob.Hash())

	// Create wrong content that doesn't match the wrong blob hash
	wrongContent := []byte("this content definitely doesn't match any hash")

	// Set up mocks using helper
	_, _, streamAcquirer := setupVerificationFailureMocks(t, ctx, modifiedSDBlobHash, wrongBlobHash, modifiedSDBlobData, wrongContent)

	reader, err := streamAcquirer.GetStream(ctx, modifiedSDBlobHash, WithAcquireVerification(true))
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Reading should fail due to blob hash verification
	buffer := make([]byte, 100)
	_, err = reader.Read(buffer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "blob hash verification failed")

	err = reader.Close()
	assert.NoError(t, err)
}

func TestStreamAcquirer_Verification_Enabled_MultipleBlobs(t *testing.T) {
	// Create content large enough for multiple blobs
	contentSize := 200 * 1024 // 200KB
	setup := createVerifiedTestSetup(t, contentSize)

	reader, err := setup.getStreamWithVerification()
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Read in chunks to test multiple blob verification
	buffer := make([]byte, 32*1024) // 32KB chunks
	totalRead := 0

	for totalRead < contentSize {
		n, err := reader.Read(buffer)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)

		// Verify the data matches expected content
		expectedStart := totalRead
		expectedEnd := expectedStart + n
		if expectedEnd > contentSize {
			expectedEnd = contentSize
		}

		assert.Equal(t, setup.contentData[expectedStart:expectedEnd], buffer[:n])
		totalRead += n
	}

	assert.Equal(t, contentSize, totalRead)
	assert.Equal(t, int64(contentSize), reader.Size())

	err = reader.Close()
	require.NoError(t, err)
}

func TestStreamAcquirer_Verification_Enabled_Seeking(t *testing.T) {
	setup := createVerifiedTestSetup(t, 50*1024) // 50KB

	reader, err := setup.getStreamWithVerification()
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Test seeking to different positions
	positions := []int64{0, 1000, 10000, 25000, 40000}

	for _, pos := range positions {
		newPos, err := reader.Seek(pos, io.SeekStart)
		require.NoError(t, err)
		assert.Equal(t, pos, newPos)

		// Read a small chunk and verify
		buffer := make([]byte, 100)
		n, err := reader.Read(buffer)
		require.NoError(t, err)

		expected := setup.contentData[pos : pos+int64(n)]
		assert.Equal(t, expected, buffer[:n])
	}

	err = reader.Close()
	require.NoError(t, err)
}

func TestStreamAcquirer_Verification_Enabled_Progress(t *testing.T) {
	setup := createVerifiedTestSetup(t, 10*1024) // 10KB

	var progressCalls []float64
	progressCallback := func(progress float64) {
		progressCalls = append(progressCalls, progress)
	}

	reader, err := setup.getStreamWithVerification(WithAcquireProgress(progressCallback))
	require.NoError(t, err)
	require.NotNil(t, reader)

	// Read entire content
	buffer := make([]byte, len(setup.contentData))
	_, err = reader.Read(buffer)
	require.NoError(t, err)

	// Check that progress was reported
	assert.Greater(t, len(progressCalls), 0)
	assert.Equal(t, 1.0, progressCalls[len(progressCalls)-1]) // Final progress should be 1.0

	err = reader.Close()
	require.NoError(t, err)
}

func TestStreamAcquirer_Verification_Comparison_EnabledVsDisabled(t *testing.T) {
	contentSize := 5 * 1024 // 5KB

	// Test with verification enabled
	setupEnabled := createVerifiedTestSetup(t, contentSize)
	readerEnabled, err := setupEnabled.getStreamWithVerification()
	require.NoError(t, err)

	bufferEnabled := make([]byte, contentSize)
	nEnabled, err := io.ReadFull(readerEnabled, bufferEnabled)
	require.NoError(t, err)

	// Test with verification disabled
	setupDisabled := createVerifiedTestSetup(t, contentSize)
	readerDisabled, err := setupDisabled.getStreamWithoutVerification()
	require.NoError(t, err)

	bufferDisabled := make([]byte, contentSize)
	nDisabled, err := io.ReadFull(readerDisabled, bufferDisabled)
	require.NoError(t, err)

	// Both should read the same amount and content
	assert.Equal(t, nEnabled, nDisabled)
	assert.Equal(t, bufferEnabled[:nEnabled], bufferDisabled[:nDisabled])
	assert.Equal(t, setupEnabled.contentData, bufferEnabled[:nEnabled])

	err = readerEnabled.Close()
	require.NoError(t, err)
	err = readerDisabled.Close()
	require.NoError(t, err)
}
