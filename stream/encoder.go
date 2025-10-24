// Code adapted from github.com/lbryio/lbry.go/v2 - MIT License (c) 2016-2020 LBRY Inc.
//
// Adapted for liblbry integration with the following changes:
//   - Combined stream processing functionality into a single encoder
//   - Added chunking support for large file processing
//   - Integrated configuration options for flexible stream handling
//   - Updated package structure to match liblbry conventions
//   - Preserved core LBRY stream encoding algorithms and crypto behavior

package stream

import (
	"bytes"
	"crypto/rand"
	"crypto/sha512"
	"encoding/hex"
	"hash"
	"io"
	"math"

	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

// New creates a new Stream from a stream of bytes.
func New(src io.Reader) (Stream, error) {
	return NewEncoder(src).Stream()
}

// Data returns the file data that a stream encapsulates.
//
// Deprecated: use Decode() instead. It's a more accurate name. Data() will be removed in the future.
func (s Stream) Data() ([]byte, error) {
	return s.Decode()
}

// Decode returns the file data that a stream encapsulates
func (s Stream) Decode() ([]byte, error) {
	if len(s) < 2 {
		return nil, liblbryerrors.Err("stream must be at least 2 blobs long") // sd blob and content blob
	}

	sdBlob := &SDBlob{}
	err := sdBlob.FromBlob([]byte(s[0]))
	if err != nil {
		return nil, err
	}

	if !sdBlob.IsValid() {
		return nil, liblbryerrors.Err("sd blob is not valid")
	}

	if sdBlob.BlobInfos[len(sdBlob.BlobInfos)-1].Length != 0 {
		return nil, liblbryerrors.Err("sd blob is missing the terminating 0-length blob")
	}

	if len(s[1:]) != len(sdBlob.BlobInfos)-1 { // -1 for terminating 0-length blob
		return nil, liblbryerrors.Err("number of blobs in stream does not match number of blobs in sd info")
	}

	var file []byte
	for i, blobInfo := range sdBlob.BlobInfos {
		if blobInfo.Length == 0 {
			if i != len(sdBlob.BlobInfos)-1 {
				return nil, liblbryerrors.Err("got 0-length blob before end of stream")
			}
			break
		}

		if blobInfo.BlobNum != i {
			return nil, liblbryerrors.Err("blobs are out of order in sd blob")
		}

		blob := s[i+1]

		if !bytes.Equal(blob.Hash(), blobInfo.BlobHash) {
			return nil, liblbryerrors.Err("blob hash doesn't match hash in blobInfo")
		}

		data, err := blob.Plaintext(sdBlob.Key, blobInfo.IV)
		if err != nil {
			return nil, err
		}
		file = append(file, data...)
	}

	return file, nil
}

// Encoder reads bytes from a source and returns blobs of the stream
type Encoder struct {
	// source data to be encoded into a stream
	src io.Reader
	// preset IVs to use for encrypting blobs
	ivs [][]byte
	// an optionals hint about the total size of the source data
	// encoder will use this to preallocate space for blobs
	srcSizeHint int

	// buffer for reading bytes from reader
	buf []byte
	// sd blob that gets built as stream is encoded
	sd *SDBlob
	// number of bytes read from src
	srcLen int
	// running hash bytes read from src
	srcHash hash.Hash
}

// NewEncoder creates a new stream encoder
func NewEncoder(src io.Reader) *Encoder {
	return &Encoder{
		src: src,

		buf: make([]byte, maxBlobDataSize),
		sd: &SDBlob{
			StreamType: streamTypeLBRYFile,
			Key:        randIV(),
		},
		srcHash: sha512.New384(),
	}
}

// NewEncoderWithIVs creates a new encoder that uses preset cryptographic material
func NewEncoderWithIVs(src io.Reader, key []byte, ivs [][]byte) *Encoder {
	e := NewEncoder(src)
	e.sd.Key = key
	e.ivs = ivs
	return e
}

// NewEncoderFromSD creates a new encoder that reuses cryptographic material from an sd blob
// This can be used to reconstruct a stream exactly from a file
// NOTE: this will assume that all blobs except the last one are at max length. in theory this is not
// required, but in practice this is always true. if this is false, streams may not match exactly
func NewEncoderFromSD(src io.Reader, sdBlob *SDBlob) *Encoder {
	ivs := make([][]byte, len(sdBlob.BlobInfos))
	for i := range ivs {
		ivs[i] = sdBlob.BlobInfos[i].IV
	}

	e := NewEncoderWithIVs(src, sdBlob.Key, ivs)
	e.sd.StreamName = sdBlob.StreamName
	e.sd.SuggestedFileName = sdBlob.SuggestedFileName
	return e
}

// Next reads the next chunk of data, encodes it into a blob, and adds it to the stream
// When the source is fully consumed, Next() makes sure the stream is terminated (i.e. the sd blob
// ends with an empty terminating blob) and returns io.EOF
func (e *Encoder) Next() (Blob, error) {
	n, err := e.src.Read(e.buf)
	// If we read some bytes, process them regardless of err.
	if n == 0 {
		if liblbryerrors.Is(err, io.EOF) {
			e.ensureTerminated()
		}
		return nil, err
	}

	e.srcLen += n
	e.srcHash.Write(e.buf[:n])
	iv := e.nextIV()

	blob, err := NewBlob(e.buf[:n], e.sd.Key, iv)
	if err != nil {
		return nil, err
	}

	err = e.sd.addBlob(blob, iv)
	if err != nil {
		return nil, err
	}

	// If underlying read reported EOF along with data, surface EOF next call.
	if liblbryerrors.Is(err, io.EOF) {
		// Do not terminate yet; allow caller to drain the last blob first.
		err = nil
	}
	return blob, err
}

// Stream creates the whole stream in one call
func (e *Encoder) Stream() (Stream, error) {
	s := make(Stream, 1, 1+int(math.Ceil(float64(e.srcSizeHint)/maxBlobDataSize))) // len starts at 1 and cap is +1 to leave room for sd blob

	for {
		blob, err := e.Next()
		if err != nil {
			if liblbryerrors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}

		s = append(s, blob)
	}

	sdBlobData, err := e.SDBlob().ToBlob()
	if err != nil {
		return nil, err
	}
	s[0] = Blob(sdBlobData)

	if cap(s) > len(s) {
		// size hint was too big. copy stream to smaller underlying array to free memory
		// this might be premature optimization...
		s = append(Stream(nil), s[:]...)
	}

	return s, nil
}

// Encode processes the entire stream according to the provided configuration
func (e *Encoder) Encode(config *StreamConfig) (*StreamResult, error) {
	// Guard against nil config
	if config == nil {
		config = &StreamConfig{}
	}

	// If using existing SD blob, parse it and use its data
	if len(config.ExistingSDBlob) > 0 {
		sdBlob := &SDBlob{}
		err := sdBlob.FromBlob(config.ExistingSDBlob)
		if err != nil {
			return nil, liblbryerrors.Err("failed to parse existing SD blob: %w", err)
		}
		e.sd = sdBlob
	}

	// Set custom chunk size if provided
	chunkSize := maxBlobDataSize
	if config.ChunkSize > 0 {
		chunkSize = config.ChunkSize
		// Ensure we leave room for padding
		if chunkSize >= MaxBlobSize {
			chunkSize = maxBlobDataSize
		}
	}
	e.buf = make([]byte, chunkSize)

	// Track chunk sizes and hashes
	var chunkSizes []int
	var contentBlobs [][]byte
	var contentHashes []string

	// If using chunk handler, process chunks individually
	if config.ChunkHandler != nil {
		chunkNumber := 0
		for {
			blob, err := e.Next()
			if err != nil {
				if liblbryerrors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}

			chunk := Chunk{
				Number: chunkNumber,
				Hash:   blob.HashHex(),
				Data:   []byte(blob),
				Size:   len(blob),
			}

			// Call chunk handler
			err = config.ChunkHandler(chunk)
			if err != nil {
				return nil, liblbryerrors.Err("chunk handler failed for chunk %d: %w", chunkNumber, err)
			}

			// Store chunk info for result
			chunkSizes = append(chunkSizes, chunk.Size)

			chunkNumber++
		}
	} else {
		// Process all chunks in memory
		for {
			blob, err := e.Next()
			if err != nil {
				if liblbryerrors.Is(err, io.EOF) {
					break
				}
				return nil, err
			}

			contentBlobs = append(contentBlobs, []byte(blob))
			contentHashes = append(contentHashes, blob.HashHex())
			chunkSizes = append(chunkSizes, len(blob))
		}
	}

	// Generate final SD blob data
	sdBlobData, err := e.SDBlob().ToBlob()
	if err != nil {
		return nil, liblbryerrors.Err("failed to generate SD blob data: %w", err)
	}

	// Call SD handler if provided
	if config.SDHandler != nil {
		err = config.SDHandler(e.SDBlob(), sdBlobData)
		if err != nil {
			return nil, liblbryerrors.Err("SD handler failed: %w", err)
		}
	}

	blobHash, err := computeBlobHash(sdBlobData)
	if err != nil {
		return nil, liblbryerrors.Err("failed to compute SD blob hash: %w", err)
	}

	result := &StreamResult{
		SDBlob:      e.SDBlob(),
		SDBlobData:  sdBlobData,
		SDBlobHash:  hex.EncodeToString(blobHash),
		StreamHash:  hex.EncodeToString(e.SDBlob().StreamHash),
		SourceSize:  int64(e.SourceLen()),
		TotalChunks: len(e.sd.BlobInfos) - 1, // Exclude terminating blob
		ChunkSizes:  chunkSizes,
	}

	// Only populate content blobs if not using chunk handler
	if config.ChunkHandler == nil {
		result.ContentBlobs = contentBlobs
		result.ContentHashes = contentHashes
	}

	return result, nil
}

// SDBlob returns the sd blob so far
func (e *Encoder) SDBlob() *SDBlob {
	e.sd.updateStreamHash()
	return e.sd
}

// SourceLen returns the number of bytes read from source
func (e *Encoder) SourceLen() int {
	return e.srcLen
}

// SourceLen returns a hash of the bytes read from source
func (e *Encoder) SourceHash() []byte {
	return e.srcHash.Sum(nil)
}

// SourceSizeHint sets a hint about the total size of the source
// This helps allocate RAM more efficiently.
// If the hint is wrong, it still works fine but there will be a small performance penalty.
func (e *Encoder) SourceSizeHint(size int) *Encoder {
	e.srcSizeHint = size
	return e
}

func (e *Encoder) isTerminated() bool {
	return len(e.sd.BlobInfos) >= 1 && e.sd.BlobInfos[len(e.sd.BlobInfos)-1].Length == 0
}

func (e *Encoder) ensureTerminated() {
	if !e.isTerminated() {
		// Add a terminating null blob
		_ = e.sd.addBlob(Blob{}, e.nextIV())
	}
}

// nextIV returns the next preset IV if there is one
func (e *Encoder) nextIV() []byte {
	if len(e.ivs) == 0 {
		return randIV()
	}

	iv := e.ivs[0]
	e.ivs = e.ivs[1:]
	return iv
}

// randIV generates a random initialization vector
func randIV() []byte {
	iv := make([]byte, 16) // AES block size
	_, err := io.ReadFull(rand.Reader, iv)
	if err != nil {
		panic(liblbryerrors.Err("failed to generate random IV: %w", err))
	}
	return iv
}
