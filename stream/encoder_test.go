package stream

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry/errors"
	liblbrytesting "go.lumeweb.com/liblbry/internal/testing"
)

// computeHash computes the hash of data using SHA384
func computeHash(data []byte) string {
	hash := sha512.Sum384(data)
	return hex.EncodeToString(hash[:])
}

var testdataBlobHashes = []string{
	"1bf7d39c45d1a38ffa74bff179bf7f67d400ff57fa0b5a0308963f08d01712b3079530a8c188e8c89d9b390c6ee06f05", // sd hash
	"a2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6c",
	"0c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5cb",
	"a4d07d442b9907036c75b6c92db316a8b8428733bf5ec976627a48a7c862bf84db33075d54125a7c0b297bd2dc445f1c",
	"dcd2093f4a3eca9f6dd59d785d0bef068fee788481986aa894cf72ed4d992c0ff9d19d1743525de2f5c3c62f5ede1c58",
}

func TestStreamToFile(t *testing.T) {
	stream := make(Stream, len(testdataBlobHashes))
	for i, hash := range testdataBlobHashes {
		stream[i] = liblbrytesting.TestData(t, hash)
	}

	data, err := stream.Decode()
	if err != nil {
		t.Fatal(err)
	}

	expectedLen := 6990951
	actualLen := len(data)

	if actualLen != expectedLen {
		t.Errorf("file length mismatch. got %d, expected %d", actualLen, expectedLen)
	}

	expectedFileHash := sha512.Sum384(data)

	expectedSha256 := liblbrytesting.Unhex(t, "51e4d03bd6d69ea17d1be3ce01fdffa44ffe053f2dbce8d42a50283b2890fea2")
	actualSha256 := sha256.Sum256(data)

	if !bytes.Equal(actualSha256[:], expectedSha256) {
		t.Errorf("file hash mismatch. got %s, expected %s", hex.EncodeToString(actualSha256[:]), hex.EncodeToString(expectedSha256))
	}

	sdBlob := &SDBlob{}
	err = sdBlob.FromBlob(stream[0])
	if err != nil {
		t.Fatal(err)
	}

	enc := NewEncoderFromSD(bytes.NewBuffer(data), sdBlob)
	newStream, err := enc.Stream()
	require.NoError(t, err, "enc.Stream() failed")

	if len(newStream) != len(testdataBlobHashes) {
		t.Fatalf("stream length mismatch. got %d blobs, expected %d", len(newStream), len(testdataBlobHashes))
	}

	if enc.SourceLen() != expectedLen {
		t.Errorf("reconstructed file length mismatch. got %d, expected %d", enc.SourceLen(), expectedLen)
	}

	if !bytes.Equal(enc.SourceHash(), expectedFileHash[:]) {
		t.Errorf("reconstructed file hash mismatch. got %s, expected %s", hex.EncodeToString(enc.SourceHash()), hex.EncodeToString(expectedFileHash[:]))
	}

	for i, hash := range testdataBlobHashes {
		if newStream[i].HashHex() != hash {
			t.Errorf("blob %d hash mismatch. got %s, expected %s", i, newStream[i].HashHex(), hash)
		}
	}
}

func TestMakeStream(t *testing.T) {
	blobsToRead := 3
	totalBlobs := blobsToRead + 3

	data := make([]byte, ((totalBlobs-1)*maxBlobDataSize)+1000) // last blob is partial
	_, err := rand.Read(data)
	if err != nil {
		t.Fatal(err)
	}

	buf := bytes.NewBuffer(data)

	enc := NewEncoder(buf)

	stream := make(Stream, blobsToRead+1) // +1 for sd blob
	for i := 1; i < blobsToRead+1; i++ {  // start at 1 to skip sd blob
		stream[i], err = enc.Next()
		if err != nil {
			t.Fatal(err)
		}
	}

	sdBlob := enc.SDBlob()

	if len(sdBlob.BlobInfos) != blobsToRead {
		t.Errorf("expected %d blobs in partial sdblob, got %d", blobsToRead, len(sdBlob.BlobInfos))
	}
	if enc.SourceLen() != maxBlobDataSize*blobsToRead {
		t.Errorf("expected length of %d , got %d", maxBlobDataSize*blobsToRead, enc.SourceLen())
	}

	// now finish the stream, reusing key and IVs

	buf = bytes.NewBuffer(data) // rewind to the beginning of the data

	enc = NewEncoderFromSD(buf, sdBlob)

	reconstructedStream, err := enc.Stream()
	if err != nil {
		t.Fatal(err)
	}

	if len(reconstructedStream) != totalBlobs+1 { // +1 for the terminating blob at the end
		t.Errorf("expected %d blobs in stream, got %d", totalBlobs+1, len(reconstructedStream))
	}
	if enc.SourceLen() != len(data) {
		t.Errorf("expected length of %d , got %d", len(data), enc.SourceLen())
	}

	reconstructedSDBlob := enc.SDBlob()

	for i := 0; i < len(sdBlob.BlobInfos); i++ {
		if !bytes.Equal(sdBlob.BlobInfos[i].IV, reconstructedSDBlob.BlobInfos[i].IV) {
			t.Errorf("blob info %d of reconstructed sd blobd does not match original sd blob", i)
		}
	}
	for i := 1; i < len(stream); i++ { // start at 1 to skip sd blob
		if !bytes.Equal(stream[i], reconstructedStream[i]) {
			t.Errorf("blob %d of reconstructed stream does not match original stream", i)
		}
	}
}

func TestEmptyStream(t *testing.T) {
	enc := NewEncoder(bytes.NewBuffer(nil))
	_, err := enc.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
	sd := enc.SDBlob()
	if len(sd.BlobInfos) != 1 {
		t.Errorf("expected 1 blobinfos in sd blob, got %d", len(sd.BlobInfos))
	}
	if sd.BlobInfos[0].Length != 0 {
		t.Errorf("first and only blob to be the terminator blob")
	}
}

func TestTermination(t *testing.T) {
	b := make([]byte, 12)

	enc := NewEncoder(bytes.NewBuffer(b))

	_, err := enc.Next()
	if err != nil {
		t.Error(err)
	}
	if enc.isTerminated() {
		t.Errorf("stream should not terminate until after EOF")
	}

	_, err = enc.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
	if !enc.isTerminated() {
		t.Errorf("stream should be terminated after EOF")
	}

	_, err = enc.Next()
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF on all subsequent reads, got %v", err)
	}
	sd := enc.SDBlob()
	if len(sd.BlobInfos) != 2 {
		t.Errorf("expected 2 blobinfos in sd blob, got %d", len(sd.BlobInfos))
	}
}

func TestSizeHint(t *testing.T) {
	b := make([]byte, 12)

	newStream, err := NewEncoder(bytes.NewBuffer(b)).SourceSizeHint(5 * maxBlobDataSize).Stream()
	if err != nil {
		t.Fatal(err)
	}

	if cap(newStream) != 2 { // 1 for sd blob (terminator is in SD metadata), 1 for the 12 bytes of the actual stream
		t.Fatalf("expected 2 blobs allocated, got %d", cap(newStream))
	}
}

func TestEncoderChunkHandler_FirstChunkProcessed(t *testing.T) {
	data := make([]byte, maxBlobDataSize*3)
	_, err := rand.Read(data)
	require.NoError(t, err)

	buf := bytes.NewBuffer(data)
	enc := NewEncoder(buf)

	var receivedChunks []Chunk
	config := &StreamConfig{
		ChunkHandler: func(chunk Chunk) error {
			receivedChunks = append(receivedChunks, chunk)
			return nil
		},
	}

	_, err = enc.Encode(config)
	require.NoError(t, err)

	// Should have received 3 chunks
	require.Equal(t, 3, len(receivedChunks))

	// Verify the first chunk was processed
	firstChunk := receivedChunks[0]
	require.Equal(t, 0, firstChunk.Number)
	require.Greater(t, len(firstChunk.Data), 0)
	require.NotEmpty(t, firstChunk.Hash)

	// Verify hash consistency by recomputing
	expectedHash := computeHash(firstChunk.Data)
	require.Equal(t, expectedHash, firstChunk.Hash)
}

func TestEncoderChunkHandler_ChunkNumbering(t *testing.T) {
	data := make([]byte, maxBlobDataSize*5)
	_, err := rand.Read(data)
	require.NoError(t, err)

	buf := bytes.NewBuffer(data)
	enc := NewEncoder(buf)

	var receivedChunks []Chunk
	config := &StreamConfig{
		ChunkHandler: func(chunk Chunk) error {
			receivedChunks = append(receivedChunks, chunk)
			return nil
		},
	}

	_, err = enc.Encode(config)
	require.NoError(t, err)

	// Should have received 5 chunks
	require.Equal(t, 5, len(receivedChunks))

	// Verify chunk numbering starts at 0 and increments correctly
	for i, chunk := range receivedChunks {
		require.Equal(t, i, chunk.Number)
	}
}

func TestEncoderChunkHandler_AllChunksProcessed(t *testing.T) {
	data := make([]byte, maxBlobDataSize*4+1000) // 4 full chunks + 1 partial chunk
	_, err := rand.Read(data)
	require.NoError(t, err)

	buf := bytes.NewBuffer(data)
	enc := NewEncoder(buf)

	var receivedChunks []Chunk
	config := &StreamConfig{
		ChunkHandler: func(chunk Chunk) error {
			receivedChunks = append(receivedChunks, chunk)
			return nil
		},
	}

	_, err = enc.Encode(config)
	require.NoError(t, err)

	// Should have received 5 chunks (4 full + 1 partial)
	require.Equal(t, 5, len(receivedChunks))

	// Verify all chunks have data and hashes
	for i, chunk := range receivedChunks {
		require.Greater(t, len(chunk.Data), 0, "chunk %d has no data", i)
		require.NotEmpty(t, chunk.Hash, "chunk %d has no hash", i)
		require.Greater(t, chunk.Size, 0, "chunk %d has zero size", i)
	}
}

func TestEncoderChunkHandler_ErrorHandling(t *testing.T) {
	data := make([]byte, maxBlobDataSize*3)
	_, err := rand.Read(data)
	require.NoError(t, err)

	buf := bytes.NewBuffer(data)
	enc := NewEncoder(buf)

	testError := errors.Err("test error")
	config := &StreamConfig{
		ChunkHandler: func(chunk Chunk) error {
			if chunk.Number == 1 { // Fail on the second chunk
				return testError
			}
			return nil
		},
	}

	_, err = enc.Encode(config)
	require.Error(t, err)
	require.True(t, errors.Is(err, testError))
}

func TestEncoderChunkHandler_DifferentDataSizes(t *testing.T) {
	testCases := []struct {
		name           string
		dataSize       int
		expectedChunks int
	}{
		{
			name:           "empty_data",
			dataSize:       0,
			expectedChunks: 0, // No chunks when data is empty
		},
		{
			name:           "small_data",
			dataSize:       100,
			expectedChunks: 1,
		},
		{
			name:           "one_full_chunk",
			dataSize:       maxBlobDataSize,
			expectedChunks: 1,
		},
		{
			name:           "one_full_plus_partial",
			dataSize:       maxBlobDataSize + 1000,
			expectedChunks: 2,
		},
		{
			name:           "multiple_chunks",
			dataSize:       maxBlobDataSize*3 + 5000,
			expectedChunks: 4,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, tc.dataSize)
			if tc.dataSize > 0 {
				_, err := rand.Read(data)
				require.NoError(t, err)
			}

			buf := bytes.NewBuffer(data)
			enc := NewEncoder(buf)

			var receivedChunks []Chunk
			var chunkNumbers []int
			config := &StreamConfig{
				ChunkHandler: func(chunk Chunk) error {
					receivedChunks = append(receivedChunks, chunk)
					chunkNumbers = append(chunkNumbers, chunk.Number)
					return nil
				},
			}

			result, err := enc.Encode(config)
			require.NoError(t, err)

			require.Equal(t, tc.expectedChunks, len(receivedChunks))

			// Verify numbering is sequential starting from 0
			if len(chunkNumbers) > 0 {
				for i := 0; i < len(chunkNumbers); i++ {
					require.Equal(t, i, chunkNumbers[i])
				}
			}

			// Verify result contains correct chunk count
			require.Equal(t, tc.expectedChunks, result.TotalChunks)
		})
	}
}
