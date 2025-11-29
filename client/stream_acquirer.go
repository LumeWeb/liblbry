package client

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/avast/retry-go/v4"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/blob"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage"
	"go.lumeweb.com/liblbry/stream"
	"go.uber.org/zap"
)

// verifyBlobHash verifies that the blob data matches the expected hash
func verifyBlobHash(data []byte, expectedHash string) error {
	if len(data) == 0 {
		return NewStreamError(OperationValidation, "", expectedHash, ErrBlobAcquisition, 1)
	}

	// Compute hash of the actual data
	computedHash, err := blob.ComputeBlobHashBytes(data)
	if err != nil {
		return NewStreamError(OperationValidation, "", expectedHash, err, 1)
	}

	// Convert to hex string for error reporting
	computedHashHex := hex.EncodeToString(computedHash)

	// Decode expected hash for constant-time comparison
	expectedHashBytes, err := hex.DecodeString(expectedHash)
	if err != nil {
		return NewStreamError(OperationValidation, "", expectedHash,
			liblbryerrors.Err("invalid expected hash format: %w", err), 1)
	}

	// Compare computed hash with expected hash using constant-time comparison
	if subtle.ConstantTimeCompare(computedHash, expectedHashBytes) != 1 {
		return NewStreamError(OperationValidation, "", expectedHash,
			liblbryerrors.Err("blob hash verification failed: expected %s, got %s", expectedHash, computedHashHex), 1)
	}

	return nil
}

// verifyAndValidateSDBlob verifies the blob hash (if enabled) and validates SD blob structure
func verifyAndValidateSDBlob(data []byte, expectedHash string, verificationEnabled bool) error {
	if len(data) == 0 {
		return NewStreamError(OperationValidation, "", expectedHash, ErrBlobAcquisition, 1)
	}

	// Verify blob hash if verification is enabled
	if verificationEnabled {
		if err := verifyBlobHash(data, expectedHash); err != nil {
			return fmt.Errorf("SD blob hash verification failed for %s: %w", expectedHash, err)
		}
	}

	// Validate SD blob structure
	if err := stream.ValidateSDBlob(data); err != nil {
		return fmt.Errorf("invalid SD blob %s: %w", expectedHash, err)
	}

	return nil
}

// StreamReader provides reading and seeking capabilities for streams with on-demand blob acquisition
type StreamReader interface {
	io.Reader
	io.Seeker
	io.Closer

	// Size returns the total size of the stream
	Size() int64

	// DecryptedSize returns actual content size after decryption
	// Returns -1 if size cannot be determined without decrypting blobs
	DecryptedSize() int64

	// Hash returns the stream hash
	Hash() string

	// SDBlob returns the SD blob metadata
	SDBlob() *stream.SDBlob

	// Progress returns current read progress (0.0 to 1.0)
	Progress() float64
}

// StreamAcquirer provides a unified interface for stream acquisition that works
// across both client and server contexts, eliminating code duplication.
type StreamAcquirer interface {
	// GetStream provides memory-efficient streaming access to a stream
	// This is preferred method for large files as it avoids loading entire stream into memory
	GetStream(ctx context.Context, sdHash string, opts ...AcquireOption) (StreamReader, error)

	// GetStreamResult retrieves full stream data (for backward compatibility)
	// This loads all content blobs into memory, similar to existing BlobManager.AcquireSDBlob behavior
	GetStreamResult(ctx context.Context, sdHash string, opts ...AcquireOption) (*stream.StreamResult, error)

	// GetSDBlob retrieves just the SD blob metadata without content blobs
	GetSDBlob(ctx context.Context, sdHash string) (*stream.SDBlob, []byte, error)
}

// StreamConfig holds configuration for stream acquisition
type StreamConfig struct {
	// Recursive determines whether to fetch all content blobs or just SD blob.
	Recursive bool

	// Progress callback for reporting download progress (0.0 to 1.0).
	Progress func(progress float64)

	// ChunkHandler for custom chunk processing during streaming.
	// Called for each chunk as it's processed.
	ChunkHandler func(chunk StreamChunk) error

	// CacheSize determines how many blobs to keep in memory during streaming.
	// Minimum value is 1. Default is 3 (current + next + previous).
	CacheSize int

	// PrefetchEnabled enables prefetching the next blob while reading the current one.
	PrefetchEnabled bool

	// VerificationEnabled enables hash verification of SD blobs and content blobs during streaming.
	// Enabled by default for security.
	VerificationEnabled bool
}

// StreamChunk represents a chunk of stream data during processing
type StreamChunk struct {
	StreamID string
	Index    int
	Data     []byte
	Hash     string
	Size     int
}

// DefaultStreamConfig returns a default configuration for stream acquisition
func DefaultStreamConfig() *StreamConfig {
	return &StreamConfig{
		Recursive:       false,
		CacheSize:       3, // Keep current + next + previous blobs in memory
		PrefetchEnabled: true,

		VerificationEnabled: true, // Enable hash verification by default
	}
}

// StreamOption defines a function type for configuring stream acquisition
type StreamOption func(*StreamConfig)

// WithStreamRecursive sets whether to recursively fetch all content blobs
func WithStreamRecursive(recursive bool) StreamOption {
	return func(config *StreamConfig) {
		config.Recursive = recursive
	}
}

// WithStreamProgress sets a progress callback for stream acquisition
func WithStreamProgress(progress func(float64)) StreamOption {
	return func(config *StreamConfig) {
		config.Progress = progress
	}
}

// WithStreamChunkHandler sets a custom chunk handler
func WithStreamChunkHandler(handler func(chunk StreamChunk) error) StreamOption {
	return func(config *StreamConfig) {
		config.ChunkHandler = handler
	}
}

// WithStreamCacheSize sets the cache size for streaming
func WithStreamCacheSize(size int) StreamOption {
	return func(config *StreamConfig) {
		config.CacheSize = size
	}
}

// WithStreamPrefetch enables or disables prefetching
func WithStreamPrefetch(enabled bool) StreamOption {
	return func(config *StreamConfig) {
		config.PrefetchEnabled = enabled
	}
}

// WithStreamVerification enables or disables hash verification during streaming
func WithStreamVerification(enabled bool) StreamOption {
	return func(config *StreamConfig) {
		config.VerificationEnabled = enabled
	}
}

// AcquireConfig holds configuration for stream acquisition
type AcquireConfig struct {
	Recursive           bool           // Whether to fetch all content blobs
	Progress            func(float64)  // Progress callback (0.0 to 1.0)
	RetryOptions        []retry.Option // Retry configuration using retry-go
	StreamOptions       []StreamOption // Stream-specific options
	VerificationEnabled bool           // Whether to verify blob hashes during acquisition
}

// AcquireOption defines a function type for configuring stream acquisition
type AcquireOption func(*AcquireConfig)

// WithAcquireRecursive sets whether to recursively fetch all content blobs
func WithAcquireRecursive(recursive bool) AcquireOption {
	return func(config *AcquireConfig) {
		config.Recursive = recursive
	}
}

// WithAcquireProgress sets a progress callback for stream acquisition
func WithAcquireProgress(progress func(float64)) AcquireOption {
	return func(config *AcquireConfig) {
		config.Progress = progress
	}
}

// WithAcquireRetry sets the retry configuration for stream acquisition
func WithAcquireRetry(retryOptions []retry.Option) AcquireOption {
	return func(config *AcquireConfig) {
		config.RetryOptions = retryOptions
	}
}

// WithAcquireStreamOptions adds stream-specific options
func WithAcquireStreamOptions(streamOpts ...StreamOption) AcquireOption {
	return func(config *AcquireConfig) {
		config.StreamOptions = append(config.StreamOptions, streamOpts...)
	}
}

// WithAcquireVerification enables or disables hash verification during stream acquisition
func WithAcquireVerification(enabled bool) AcquireOption {
	return func(config *AcquireConfig) {
		config.VerificationEnabled = enabled
	}
}

// cachedBlob represents a cached blob with LRU tracking
type cachedBlob struct {
	data       []byte
	lastAccess time.Time
}

// streamingReader implements StreamReader with on-demand blob acquisition
type streamingReader struct {
	ctx      context.Context
	sdHash   string
	sdBlob   *stream.SDBlob
	acquirer liblbry.BlobAcquirer
	store    storage.BlobStore
	config   *StreamConfig

	// Stream state
	currentPos int64
	totalSize  int64
	streamHash string

	// Blob cache for efficient access
	blobCache  map[int]*cachedBlob
	cacheMutex sync.RWMutex
	cacheSize  int

	// Current reading state
	currentBlob int
	blobOffset  int64
	currentData []byte
	readMutex   sync.RWMutex

	// Prefetching
	prefetchChan   chan int
	prefetchWG     sync.WaitGroup
	prefetchCtx    context.Context
	prefetchCancel context.CancelFunc

	// Storage operations
	storageWG sync.WaitGroup // Track background storage operations

	// Progress tracking
	progressCallback func(float64)
	bytesRead        int64

	// Error handling and retry
	retryOptions []retry.Option

	// Logger
	logger *zap.Logger

	// Cleanup
	closed int32
}

// newStreamReader creates a new StreamReader for streaming access
func newStreamReader(
	ctx context.Context,
	sdHash string,
	sdBlob *stream.SDBlob,
	acquirer liblbry.BlobAcquirer,
	store storage.BlobStore,
	logger *zap.Logger,
	opts ...StreamOption,
) (StreamReader, error) {
	config := DefaultStreamConfig()
	for _, opt := range opts {
		opt(config)
	}

	// Calculate total stream size (excluding terminating blob)
	// Note: This is the sum of encrypted blob sizes, not decrypted content size.
	// Use DecryptedSize() to get actual content size after decryption.
	var totalSize int64
	for i, blobInfo := range sdBlob.BlobInfos {
		// Skip terminating blob (last blob with zero length)
		if i == len(sdBlob.BlobInfos)-1 && blobInfo.Length == 0 {
			continue
		}
		totalSize += int64(blobInfo.Length)
	}

	logger.Debug("Creating new stream reader",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", hex.EncodeToString(sdBlob.StreamHash)),
		zap.Int("blobCount", len(sdBlob.BlobInfos)),
		zap.Int64("totalSize", totalSize),
	)

	// Create prefetch context
	prefetchCtx, prefetchCancel := context.WithCancel(ctx)

	// Initialize error handling
	retryOptions := DefaultRetryOptions()

	reader := &streamingReader{
		ctx:              ctx,
		sdHash:           sdHash,
		sdBlob:           sdBlob,
		acquirer:         acquirer,
		store:            store,
		config:           config,
		totalSize:        totalSize,
		streamHash:       hex.EncodeToString(sdBlob.StreamHash),
		blobCache:        make(map[int]*cachedBlob),
		cacheSize:        config.CacheSize,
		prefetchChan:     make(chan int, 1),
		prefetchCtx:      prefetchCtx,
		prefetchCancel:   prefetchCancel,
		progressCallback: config.Progress,
		retryOptions:     retryOptions,
		logger:           logger,
	}

	// Start prefetching goroutine if enabled
	if config.PrefetchEnabled {
		reader.startPrefetcher()
		logger.Debug("Started prefetcher for stream reader",
			zap.String("sdHash", sdHash),
			zap.Int("cacheSize", config.CacheSize),
		)
	}

	logger.Info("Created stream reader successfully",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", reader.streamHash),
		zap.Int64("totalSize", totalSize),
		zap.Bool("prefetchEnabled", config.PrefetchEnabled),
		zap.Bool("verificationEnabled", config.VerificationEnabled),
	)

	return reader, nil
}

// Read implements io.Reader
func (r *streamingReader) Read(p []byte) (int, error) {
	if atomic.LoadInt32(&r.closed) != 0 {
		return 0, io.ErrClosedPipe
	}

	r.readMutex.RLock()
	if r.currentPos >= r.totalSize {
		r.readMutex.RUnlock()
		return 0, io.EOF
	}
	r.readMutex.RUnlock()

	r.logger.Debug("Reading from stream",
		zap.String("sdHash", r.sdHash),
		zap.Int64("currentPos", r.currentPos),
		zap.Int64("totalSize", r.totalSize),
		zap.Int("requestedBytes", len(p)),
	)

	// Ensure we have current blob data
	if err := r.ensureCurrentBlob(); err != nil {
		r.logger.Error("Failed to ensure current blob data",
			zap.String("sdHash", r.sdHash),
			zap.Int("currentBlob", r.currentBlob),
			zap.Error(err),
		)
		return 0, err
	}

	r.readMutex.Lock()
	// Calculate how much we can read from current blob
	remainingInBlob := int64(len(r.currentData)) - r.blobOffset
	remainingInStream := r.totalSize - r.currentPos
	toRead := int64(len(p))

	if toRead > remainingInBlob {
		toRead = remainingInBlob
	}
	if toRead > remainingInStream {
		toRead = remainingInStream
	}

	// Copy data to output buffer (already decrypted by ensureCurrentBlob)
	copy(p, r.currentData[r.blobOffset:r.blobOffset+toRead])

	// Update positions
	r.blobOffset += toRead
	r.currentPos += toRead
	atomic.AddInt64(&r.bytesRead, toRead)

	// Check if we need to move to next blob
	needNextBlob := r.blobOffset >= int64(len(r.currentData))
	r.readMutex.Unlock()

	// Report progress (outside lock to avoid holding it during callback)
	if r.progressCallback != nil {
		progress := float64(r.currentPos) / float64(r.totalSize)
		r.progressCallback(progress)
	}

	// Move to next blob if current one is exhausted (needs write lock)
	if needNextBlob {
		r.readMutex.Lock()
		if r.blobOffset >= int64(len(r.currentData)) {
			oldBlob := r.currentBlob
			r.currentBlob++
			r.blobOffset = 0
			r.currentData = nil

			r.logger.Debug("Moving to next blob",
				zap.String("sdHash", r.sdHash),
				zap.Int("previousBlob", oldBlob),
				zap.Int("nextBlob", r.currentBlob),
			)

			// Trigger prefetch for next blob
			if r.config.PrefetchEnabled && r.currentBlob < len(r.sdBlob.BlobInfos) {
				select {
				case r.prefetchChan <- r.currentBlob:
					r.logger.Debug("Triggered prefetch for next blob",
						zap.String("sdHash", r.sdHash),
						zap.Int("blobIndex", r.currentBlob),
					)
				default:
					// Channel full, prefetch already queued
					r.logger.Debug("Prefetch channel full, skipping",
						zap.String("sdHash", r.sdHash),
						zap.Int("blobIndex", r.currentBlob),
					)
				}
			}
		}
		r.readMutex.Unlock()
	}

	return int(toRead), nil
}

// Seek implements io.Seeker
func (r *streamingReader) Seek(offset int64, whence int) (int64, error) {
	if atomic.LoadInt32(&r.closed) != 0 {
		return 0, io.ErrClosedPipe
	}

	r.logger.Debug("Seeking in stream",
		zap.String("sdHash", r.sdHash),
		zap.Int64("offset", offset),
		zap.Int("whence", whence),
		zap.Int64("currentPos", r.currentPos),
	)

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		r.readMutex.RLock()
		newPos = r.currentPos + offset
		r.readMutex.RUnlock()
	case io.SeekEnd:
		newPos = r.totalSize + offset
	default:
		return 0, errors.New("invalid whence value")
	}

	if newPos < 0 || newPos > r.totalSize {
		r.logger.Error("Seek position out of bounds",
			zap.String("sdHash", r.sdHash),
			zap.Int64("newPos", newPos),
			zap.Int64("totalSize", r.totalSize),
		)
		return 0, fmt.Errorf("seek position %d out of bounds [0, %d]", newPos, r.totalSize)
	}

	// Find which blob contains the new position
	var cumulativeSize int64
	targetBlob := -1
	targetOffset := int64(0)

	for i, blobInfo := range r.sdBlob.BlobInfos {
		if cumulativeSize+int64(blobInfo.Length) > newPos {
			targetBlob = i
			targetOffset = newPos - cumulativeSize
			break
		}
		cumulativeSize += int64(blobInfo.Length)
	}

	r.readMutex.Lock()
	defer r.readMutex.Unlock()

	if targetBlob == -1 {
		// Seeking to end of stream
		r.currentPos = r.totalSize
		r.currentBlob = len(r.sdBlob.BlobInfos)
		r.blobOffset = 0
		r.currentData = nil
		return r.currentPos, nil
	}

	// Update reading state
	r.currentPos = newPos
	r.currentBlob = targetBlob
	r.blobOffset = targetOffset
	r.currentData = nil // Will be loaded on next read

	return r.currentPos, nil
}

// Size returns the total size of the stream (encrypted blob sizes)
func (r *streamingReader) Size() int64 {
	return r.totalSize
}

// DecryptedSize returns actual content size after decryption
// Returns -1 if size cannot be determined without decrypting blobs
// NOTE: Will return -1 after Close() is called since cache is cleared
func (r *streamingReader) DecryptedSize() int64 {
	// If we have decrypted blobs in cache, sum their actual sizes
	r.cacheMutex.RLock()
	defer r.cacheMutex.RUnlock()

	var totalDecryptedSize int64
	knownBlobs := 0

	// Calculate size from decrypted blobs we have in cache
	for i := 0; i < len(r.sdBlob.BlobInfos)-1; i++ { // Skip terminating blob
		if cachedBlob, exists := r.blobCache[i]; exists {
			totalDecryptedSize += int64(len(cachedBlob.data))
			knownBlobs++
		}
	}

	// If we have all content blobs decrypted, return the actual total size
	totalContentBlobs := len(r.sdBlob.BlobInfos) - 1 // Exclude terminating blob
	if knownBlobs == totalContentBlobs {
		return totalDecryptedSize
	}

	// If we have some blobs decrypted, we can estimate the total
	// by assuming similar compression ratio for remaining blobs
	// WARNING: This estimate may be inaccurate for streams with variable blob sizes.
	if knownBlobs > 0 {
		avgDecryptedSize := float64(totalDecryptedSize) / float64(knownBlobs)
		remainingBlobs := totalContentBlobs - knownBlobs
		estimatedRemaining := int64(avgDecryptedSize * float64(remainingBlobs))
		return totalDecryptedSize + estimatedRemaining
	}

	// If no blobs are decrypted yet, we cannot determine the size
	// without decrypting at least one blob first
	return -1
}

// Hash returns the stream hash
func (r *streamingReader) Hash() string {
	return r.streamHash
}

// SDBlob returns the SD blob metadata
func (r *streamingReader) SDBlob() *stream.SDBlob {
	return r.sdBlob
}

// Progress returns current read progress (0.0 to 1.0)
func (r *streamingReader) Progress() float64 {
	if r.sdBlob == nil || len(r.sdBlob.BlobInfos) == 0 {
		return 1.0
	}

	// Calculate progress based on completed blob indices for accuracy
	// since bytesRead tracks decrypted bytes while totalSize is encrypted bytes
	r.readMutex.RLock()
	currentBlob := r.currentBlob
	r.readMutex.RUnlock()
	return float64(currentBlob) / float64(len(r.sdBlob.BlobInfos))
}

// Close implements io.Closer
func (r *streamingReader) Close() error {
	if !atomic.CompareAndSwapInt32(&r.closed, 0, 1) {
		return nil // Already closed
	}

	r.logger.Debug("Closing stream reader",
		zap.String("sdHash", r.sdHash),
		zap.Int64("bytesRead", atomic.LoadInt64(&r.bytesRead)),
		zap.Float64("progress", r.Progress()),
	)

	// Stop prefetcher
	if r.prefetchCancel != nil {
		r.prefetchCancel()
	}

	// Wait for prefetcher to finish
	r.prefetchWG.Wait()

	// Wait for background storage operations to finish
	r.storageWG.Wait()

	// Clear cache
	// WARNING: This clears the decrypted blob cache, which means DecryptedSize() will return -1
	// after Close() is called. This is intentional to free memory but users should
	// be aware that calling DecryptedSize() after Close() will not work.
	r.cacheMutex.Lock()
	cacheSize := len(r.blobCache)
	r.blobCache = make(map[int]*cachedBlob)
	r.cacheMutex.Unlock()

	r.logger.Info("Stream reader closed successfully",
		zap.String("sdHash", r.sdHash),
		zap.Int("clearedCacheEntries", cacheSize),
	)

	return nil
}

// ensureCurrentBlob ensures the current blob data is loaded
func (r *streamingReader) ensureCurrentBlob() error {
	r.readMutex.RLock()
	if r.currentData != nil {
		r.readMutex.RUnlock()
		return nil // Already loaded
	}

	if r.currentBlob >= len(r.sdBlob.BlobInfos) {
		r.readMutex.RUnlock()
		return io.EOF
	}
	currentBlob := r.currentBlob
	r.readMutex.RUnlock()

	r.logger.Debug("Ensuring current blob data is loaded",
		zap.String("sdHash", r.sdHash),
		zap.Int("blobIndex", currentBlob),
	)

	// Check cache first
	r.cacheMutex.RLock()
	if cachedBlob, exists := r.blobCache[currentBlob]; exists {
		data := cachedBlob.data
		r.cacheMutex.RUnlock()

		// Update last access time asynchronously to avoid lock contention
		go func() {
			r.cacheMutex.Lock()
			if updatedBlob, stillExists := r.blobCache[currentBlob]; stillExists {
				updatedBlob.lastAccess = time.Now()
			}
			r.cacheMutex.Unlock()
		}()

		r.readMutex.Lock()
		if r.currentBlob == currentBlob {
			r.currentData = data
		}
		r.readMutex.Unlock()

		r.logger.Debug("Blob data found in cache",
			zap.String("sdHash", r.sdHash),
			zap.Int("blobIndex", currentBlob),
			zap.Int("dataSize", len(data)),
		)
		return nil
	}
	r.cacheMutex.RUnlock()

	r.logger.Debug("Blob data not in cache, loading from storage or network",
		zap.String("sdHash", r.sdHash),
		zap.Int("blobIndex", currentBlob),
	)

	// Load blob from storage or acquire it
	blobInfo := r.sdBlob.BlobInfos[currentBlob]
	blobHash := hex.EncodeToString(blobInfo.BlobHash)

	// Skip acquiring terminating blobs (empty hash) - they represent end of stream
	if blobHash == "" {
		r.readMutex.Lock()
		if r.currentBlob == currentBlob {
			r.currentData = []byte{}
		}
		r.readMutex.Unlock()
		return nil
	}

	// Try storage first with retry
	if r.store != nil {
		r.logger.Debug("Attempting to load blob from storage",
			zap.String("sdHash", r.sdHash),
			zap.String("blobHash", blobHash),
			zap.Int("blobIndex", currentBlob),
		)

		err := WithRetry(r.ctx, r.retryOptions, func() error {
			has, err := r.store.Has(r.ctx, blobHash)
			if err != nil {
				return NewStreamError(OperationStorageHasCheck, r.sdHash, blobHash, err, 1)
			}
			if !has {
				return ErrStreamNotFound
			}

			data, err := r.store.Get(r.ctx, blobHash)
			if err != nil {
				return NewStreamError(OperationStorageGet, r.sdHash, blobHash, err, 1)
			}

			// Verify blob hash if verification is enabled
			if r.config.VerificationEnabled {
				if err := verifyBlobHash(data, blobHash); err != nil {
					return fmt.Errorf("stored blob hash verification failed for %s: %w", blobHash, err)
				}
			}

			if err := r.setBlobData(currentBlob, data); err != nil {
				return err
			}
			return nil
		})

		if err == nil {
			r.logger.Debug("Successfully loaded blob from storage",
				zap.String("sdHash", r.sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", currentBlob),
			)
			return nil
		}

		// If storage fails, continue with network acquisition
		if !isRetryableError(err) {
			r.logger.Debug("Blob not found in storage, trying network acquisition",
				zap.String("sdHash", r.sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", currentBlob),
			)
			// Non-retryable errors (like ErrStreamNotFound) should trigger network acquisition
			// Continue to network acquisition below
		} else {
			r.logger.Error("Storage operation failed with retryable error",
				zap.String("sdHash", r.sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", currentBlob),
				zap.Error(err),
			)
			// Retryable errors should be returned as-is to trigger retry logic
			return fmt.Errorf("storage operation failed: %w", err)
		}
	}

	// Acquire from network with retry
	r.logger.Info("Acquiring blob from network",
		zap.String("sdHash", r.sdHash),
		zap.String("blobHash", blobHash),
		zap.Int("blobIndex", currentBlob),
	)

	var data []byte
	err := WithRetry(r.ctx, r.retryOptions, func() error {
		acquiredData, acquireErr := r.acquirer.Acquire(r.ctx, blobHash)
		if acquireErr != nil {
			return NewStreamError(OperationNetworkAcquire, r.sdHash, blobHash, acquireErr, 1)
		}
		data = acquiredData
		return nil
	})

	if err != nil {
		r.logger.Error("Failed to acquire blob from network",
			zap.String("sdHash", r.sdHash),
			zap.String("blobHash", blobHash),
			zap.Int("blobIndex", currentBlob),
			zap.Error(err),
		)
		return fmt.Errorf("failed to acquire blob %s: %w", blobHash, err)
	}

	// Validate blob data
	if len(data) == 0 {
		return NewStreamError(OperationValidation, r.sdHash, blobHash, ErrBlobAcquisition, 1)
	}

	// Verify blob hash if verification is enabled
	if r.config.VerificationEnabled {
		if err := verifyBlobHash(data, blobHash); err != nil {
			return fmt.Errorf("blob hash verification failed for %s: %w", blobHash, err)
		}
	}

	// Store in storage if available (best effort)
	if r.store != nil {
		r.storageWG.Add(1)
		go func() {
			defer r.storageWG.Done()
			// Use background context for best-effort storage writes that should complete
			// regardless of reader state, avoiding cancellation when reader is closed
			ctx := context.Background()
			if err := r.store.Put(ctx, blobHash, data); err != nil {
				// Log error but don't fail the operation
				r.logger.Error("failed to store blob in storage",
					zap.String("blobHash", blobHash),
					zap.String("store", r.store.Name()),
					zap.Error(err),
				)
			} else {
				r.logger.Debug("Successfully stored blob in storage",
					zap.String("blobHash", blobHash),
					zap.String("store", r.store.Name()),
					zap.Int("dataSize", len(data)),
				)
			}
		}()
	}

	r.logger.Debug("Successfully acquired and processed blob",
		zap.String("sdHash", r.sdHash),
		zap.String("blobHash", blobHash),
		zap.Int("blobIndex", currentBlob),
		zap.Int("dataSize", len(data)),
	)

	if err := r.setBlobData(currentBlob, data); err != nil {
		return err
	}
	return nil
}

// setBlobData stores blob data in cache and sets current data
func (r *streamingReader) setBlobData(blobIndex int, data []byte) error {
	// Decrypt blob data using blob package with individual IV
	blobInfo := r.sdBlob.BlobInfos[blobIndex]
	decryptedData, err := blob.Blob(data).Plaintext(r.sdBlob.Key, blobInfo.IV)
	if err != nil {
		r.logger.Error("Failed to decrypt blob data",
			zap.String("sdHash", r.sdHash),
			zap.Int("blobIndex", blobIndex),
			zap.Error(err),
		)
		// Don't cache corrupted data - return error to let caller retry or fail
		return NewStreamError(OperationDecryptBlob, r.sdHash, hex.EncodeToString(blobInfo.BlobHash), err, 1)
	}

	r.logger.Debug("Successfully decrypted blob data",
		zap.String("sdHash", r.sdHash),
		zap.Int("blobIndex", blobIndex),
		zap.Int("encryptedSize", len(data)),
		zap.Int("decryptedSize", len(decryptedData)),
	)

	// Get current blob info BEFORE acquiring any locks to avoid deadlock
	// This prevents the scenario where Close() holds cacheMutex while waiting
	// for prefetchWG, and prefetcher is stuck here waiting for readMutex
	r.readMutex.RLock()
	currentBlob := r.currentBlob
	r.readMutex.RUnlock()

	r.cacheMutex.Lock()

	// Add decrypted data to cache
	r.blobCache[blobIndex] = &cachedBlob{
		data:       decryptedData,
		lastAccess: time.Now(),
	}

	// Manage cache size - remove least recently used entries if needed
	if len(r.blobCache) > r.cacheSize {
		// LRU: remove entry with oldest lastAccess time
		var oldestIndex int
		var oldestTime time.Time
		first := true

		for idx, cached := range r.blobCache {
			if first || cached.lastAccess.Before(oldestTime) {
				oldestIndex = idx
				oldestTime = cached.lastAccess
				first = false
			}
		}
		delete(r.blobCache, oldestIndex)
	}
	r.cacheMutex.Unlock()

	// Set as current data if this is the current blob
	// This is done after releasing cacheMutex to avoid deadlock with Close()
	if blobIndex == currentBlob {
		r.readMutex.Lock()
		// Double-check that this is still the current blob (race condition protection)
		if r.currentBlob == blobIndex {
			r.currentData = decryptedData
		}
		r.readMutex.Unlock()
	}

	return nil
}

// startPrefetcher starts the prefetching goroutine
func (r *streamingReader) startPrefetcher() {
	r.logger.Debug("Starting prefetcher goroutine",
		zap.String("sdHash", r.sdHash),
	)

	r.prefetchWG.Add(1)
	go func() {
		defer r.prefetchWG.Done()

		for {
			select {
			case <-r.prefetchCtx.Done():
				r.logger.Debug("Prefetcher goroutine exiting",
					zap.String("sdHash", r.sdHash),
				)
				return

			case blobIndex := <-r.prefetchChan:
				if blobIndex >= len(r.sdBlob.BlobInfos) {
					r.logger.Debug("Prefetch request for invalid blob index",
						zap.String("sdHash", r.sdHash),
						zap.Int("blobIndex", blobIndex),
						zap.Int("totalBlobs", len(r.sdBlob.BlobInfos)),
					)
					continue
				}

				r.logger.Debug("Processing prefetch request",
					zap.String("sdHash", r.sdHash),
					zap.Int("blobIndex", blobIndex),
				)

				// Check if already cached
				r.cacheMutex.RLock()
				_, exists := r.blobCache[blobIndex]
				r.cacheMutex.RUnlock()

				if exists {
					r.logger.Debug("Blob already in cache, skipping prefetch",
						zap.String("sdHash", r.sdHash),
						zap.Int("blobIndex", blobIndex),
					)
					continue
				}

				// Prefetch the blob
				blobInfo := r.sdBlob.BlobInfos[blobIndex]
				blobHash := hex.EncodeToString(blobInfo.BlobHash)

				// Skip prefetching terminating blobs (empty hash)
				if blobHash == "" {
					continue
				}

				// Try storage first
				if r.store != nil {
					r.logger.Debug("Prefetch: trying storage",
						zap.String("sdHash", r.sdHash),
						zap.String("blobHash", blobHash),
						zap.Int("blobIndex", blobIndex),
					)

					if has, err := r.store.Has(r.ctx, blobHash); err == nil && has {
						data, err := r.store.Get(r.ctx, blobHash)
						if err == nil {
							// Verify blob hash if verification is enabled
							if r.config.VerificationEnabled {
								if err := verifyBlobHash(data, blobHash); err != nil {
									r.logger.Debug("Prefetch: blob hash verification failed",
										zap.String("sdHash", r.sdHash),
										zap.String("blobHash", blobHash),
										zap.Int("blobIndex", blobIndex),
										zap.Error(err),
									)
									continue // Skip verification errors in prefetcher
								}
							}
							r.logger.Debug("Prefetch: successfully loaded blob from storage",
								zap.String("sdHash", r.sdHash),
								zap.String("blobHash", blobHash),
								zap.Int("blobIndex", blobIndex),
							)
							if err := r.setBlobData(blobIndex, data); err != nil {
								r.logger.Debug("Prefetch: failed to decrypt blob from storage",
									zap.String("sdHash", r.sdHash),
									zap.String("blobHash", blobHash),
									zap.Int("blobIndex", blobIndex),
									zap.Error(err),
								)
								continue // Skip decryption errors in prefetcher
							}
							continue
						} else {
							r.logger.Debug("Prefetch: failed to get blob from storage",
								zap.String("sdHash", r.sdHash),
								zap.String("blobHash", blobHash),
								zap.Int("blobIndex", blobIndex),
								zap.Error(err),
							)
						}
					} else {
						r.logger.Debug("Prefetch: blob not found in storage",
							zap.String("sdHash", r.sdHash),
							zap.String("blobHash", blobHash),
							zap.Int("blobIndex", blobIndex),
							zap.Error(err),
						)
					}
				}

				// Acquire from network with retry
				r.logger.Debug("Prefetch: acquiring blob from network",
					zap.String("sdHash", r.sdHash),
					zap.String("blobHash", blobHash),
					zap.Int("blobIndex", blobIndex),
				)

				var data []byte
				err := WithRetry(r.ctx, r.retryOptions, func() error {
					acquiredData, acquireErr := r.acquirer.Acquire(r.prefetchCtx, blobHash)
					if acquireErr != nil {
						return NewStreamError(OperationPrefetchAcquire, r.sdHash, blobHash, acquireErr, 1)
					}
					data = acquiredData
					return nil
				})

				if err != nil {
					r.logger.Debug("Prefetch: failed to acquire blob from network",
						zap.String("sdHash", r.sdHash),
						zap.String("blobHash", blobHash),
						zap.Int("blobIndex", blobIndex),
						zap.Error(err),
					)
					continue // Skip on error, will be loaded on demand
				}

				// Verify blob hash if verification is enabled
				if r.config.VerificationEnabled {
					if err := verifyBlobHash(data, blobHash); err != nil {
						r.logger.Debug("Prefetch: blob hash verification failed",
							zap.String("sdHash", r.sdHash),
							zap.String("blobHash", blobHash),
							zap.Int("blobIndex", blobIndex),
							zap.Error(err),
						)
						continue // Skip verification errors in prefetcher
					}
				}

				// Store in storage
				if r.store != nil {
					r.storageWG.Add(1)
					go func(hash string, d []byte) {
						defer r.storageWG.Done()
						if err := r.store.Put(r.ctx, hash, d); err != nil {
							r.logger.Debug("Prefetch: failed to store blob in storage",
								zap.String("sdHash", r.sdHash),
								zap.String("blobHash", hash),
								zap.String("store", r.store.Name()),
								zap.Error(err),
							)
						}
					}(blobHash, data)
				}

				r.logger.Debug("Prefetch: successfully processed blob",
					zap.String("sdHash", r.sdHash),
					zap.String("blobHash", blobHash),
					zap.Int("blobIndex", blobIndex),
					zap.Int("dataSize", len(data)),
				)

				if err := r.setBlobData(blobIndex, data); err != nil {
					r.logger.Debug("Prefetch: failed to decrypt blob from network",
						zap.String("sdHash", r.sdHash),
						zap.String("blobHash", blobHash),
						zap.Int("blobIndex", blobIndex),
						zap.Error(err),
					)
					// Skip decryption errors in prefetcher - will be retried on demand
				}
			}
		}
	}()
}

// DefaultStreamAcquirer implements StreamAcquirer with unified logic
type DefaultStreamAcquirer struct {
	blobAcquirer liblbry.BlobAcquirer
	store        storage.BlobStore
	logger       *zap.Logger
}

// NewStreamAcquirer creates a new StreamAcquirer with the given dependencies
func NewStreamAcquirer(blobAcquirer liblbry.BlobAcquirer, store storage.BlobStore, logger *zap.Logger) StreamAcquirer {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DefaultStreamAcquirer{
		blobAcquirer: blobAcquirer,
		store:        store,
		logger:       logger,
	}
}

// StreamAcquirerFactory defines interface for creating StreamAcquirer instances
type StreamAcquirerFactory interface {
	// CreateStreamAcquirer creates a new StreamAcquirer with the given dependencies
	CreateStreamAcquirer(acquirer liblbry.BlobAcquirer, store storage.BlobStore) StreamAcquirer
}

// DefaultStreamAcquirerFactory implements StreamAcquirerFactory
type DefaultStreamAcquirerFactory struct {
	logger *zap.Logger
}

// NewStreamAcquirerFactory creates a new StreamAcquirerFactory
func NewStreamAcquirerFactory(logger *zap.Logger) StreamAcquirerFactory {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &DefaultStreamAcquirerFactory{
		logger: logger,
	}
}

// CreateStreamAcquirer creates a new StreamAcquirer with the given dependencies and options
func (f *DefaultStreamAcquirerFactory) CreateStreamAcquirer(acquirer liblbry.BlobAcquirer, store storage.BlobStore) StreamAcquirer {
	return NewStreamAcquirer(acquirer, store, f.logger)
}

// StreamAcquirerBuilder provides a fluent interface for building StreamAcquirer instances
type StreamAcquirerBuilder struct {
	acquirer liblbry.BlobAcquirer
	store    storage.BlobStore
	factory  StreamAcquirerFactory
}

// NewStreamAcquirerBuilder creates a new StreamAcquirerBuilder
func NewStreamAcquirerBuilder(logger *zap.Logger) *StreamAcquirerBuilder {
	return &StreamAcquirerBuilder{
		factory: NewStreamAcquirerFactory(logger),
	}
}

// WithAcquirer sets the blob acquirer for the StreamAcquirer
func (b *StreamAcquirerBuilder) WithAcquirer(acquirer liblbry.BlobAcquirer) *StreamAcquirerBuilder {
	b.acquirer = acquirer
	return b
}

// WithStore sets the blob store for the StreamAcquirer
func (b *StreamAcquirerBuilder) WithStore(store storage.BlobStore) *StreamAcquirerBuilder {
	b.store = store
	return b
}

// WithFactory sets a custom factory for creating StreamAcquirer instances
func (b *StreamAcquirerBuilder) WithFactory(factory StreamAcquirerFactory) *StreamAcquirerBuilder {
	b.factory = factory
	return b
}

// Build creates the StreamAcquirer instance with the configured options
func (b *StreamAcquirerBuilder) Build() (StreamAcquirer, error) {
	if b.acquirer == nil {
		return nil, liblbryerrors.Err("acquirer is required for StreamAcquirer")
	}

	return b.factory.CreateStreamAcquirer(b.acquirer, b.store), nil
}

// GetStream provides memory-efficient streaming access to a stream
func (sa *DefaultStreamAcquirer) GetStream(ctx context.Context, sdHash string, opts ...AcquireOption) (StreamReader, error) {
	sa.logger.Info("Getting stream",
		zap.String("sdHash", sdHash),
	)

	config := &AcquireConfig{
		Recursive:    false,
		RetryOptions: DefaultRetryOptions(),
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	sa.logger.Debug("Stream acquisition configuration",
		zap.String("sdHash", sdHash),
		zap.Bool("recursive", config.Recursive),
		zap.Bool("verificationEnabled", config.VerificationEnabled),
	)

	// Create stream options
	streamOpts := make([]StreamOption, 0, len(config.StreamOptions)+2)
	streamOpts = append(streamOpts, config.StreamOptions...)

	if config.Progress != nil {
		streamOpts = append(streamOpts, WithStreamProgress(config.Progress))
	}

	// Add verification configuration
	streamOpts = append(streamOpts, WithStreamVerification(config.VerificationEnabled))

	// Validate input
	if err := ValidateBlobHash(sdHash); err != nil {
		return nil, fmt.Errorf("invalid SD hash: %w", err)
	}

	retryOptions := config.RetryOptions

	// First acquire the SD blob with retry
	sa.logger.Info("Acquiring SD blob",
		zap.String("sdHash", sdHash),
	)

	var sdBlobData []byte
	err := WithRetry(ctx, retryOptions, func() error {
		acquiredData, acquireErr := sa.blobAcquirer.Acquire(ctx, sdHash)
		if acquireErr != nil {
			return NewStreamError(OperationAcquireSDBlob, sdHash, "", acquireErr, 1)
		}
		sdBlobData = acquiredData
		return nil
	})

	if err != nil {
		sa.logger.Error("Failed to acquire SD blob",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to acquire SD blob %s: %w", sdHash, err)
	}

	sa.logger.Info("Successfully acquired SD blob",
		zap.String("sdHash", sdHash),
		zap.Int("dataSize", len(sdBlobData)),
	)

	// Verify SD blob hash and validate structure
	if err := verifyAndValidateSDBlob(sdBlobData, sdHash, config.VerificationEnabled); err != nil {
		return nil, err
	}

	// Parse the SD blob
	var sdBlob stream.SDBlob
	if err := sdBlob.FromBlob(sdBlobData); err != nil {
		sa.logger.Error("Failed to parse SD blob",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, NewStreamError(OperationParseSDBlob, sdHash, "", err, 1)
	}

	// Validate SD blob structure
	if len(sdBlob.BlobInfos) == 0 {
		sa.logger.Error("SD blob has no blob infos",
			zap.String("sdHash", sdHash),
		)
		return nil, NewStreamError(OperationValidateSDBlob, sdHash, "", stream.ErrInvalidSDBlob, 1)
	}

	sa.logger.Info("Successfully parsed SD blob",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", hex.EncodeToString(sdBlob.StreamHash)),
		zap.Int("blobCount", len(sdBlob.BlobInfos)),
	)

	// Create streaming reader directly
	reader, err := newStreamReader(ctx, sdHash, &sdBlob, sa.blobAcquirer, sa.store, sa.logger, streamOpts...)
	if err != nil {
		sa.logger.Error("Failed to create stream reader",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to create stream reader: %w", err)
	}

	sa.logger.Info("Successfully created stream reader",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", hex.EncodeToString(sdBlob.StreamHash)),
	)

	return reader, nil
}

// acquireBlobWithFallback tries to get blob from storage first, then falls back to network acquisition
func (sa *DefaultStreamAcquirer) acquireBlobWithFallback(ctx context.Context, config *AcquireConfig, sdHash, blobHash string, blobIndex int, blobInfo *stream.BlobInfo) ([]byte, error) {
	// Try storage first if available
	if sa.store != nil {
		if has, err := sa.store.Has(ctx, blobHash); err == nil && has {
			sa.logger.Debug("Content blob found in storage",
				zap.String("sdHash", sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", blobIndex),
			)

			blobData, err := sa.store.Get(ctx, blobHash)
			if err != nil {
				sa.logger.Debug("Failed to retrieve blob from storage, acquiring from network",
					zap.String("sdHash", sdHash),
					zap.String("blobHash", blobHash),
					zap.Int("blobIndex", blobIndex),
					zap.Error(err),
				)
			} else {
				// Verify blob hash if verification is enabled
				if config.VerificationEnabled {
					if err := verifyBlobHash(blobData, blobHash); err != nil {
						sa.logger.Error("Stored blob hash verification failed",
							zap.String("sdHash", sdHash),
							zap.String("blobHash", blobHash),
							zap.Int("blobIndex", blobIndex),
							zap.Error(err),
						)
					} else {
						sa.logger.Debug("Successfully retrieved content blob from storage",
							zap.String("sdHash", sdHash),
							zap.String("blobHash", blobHash),
							zap.Int("blobIndex", blobIndex),
							zap.Int("dataSize", len(blobData)),
						)
						return blobData, nil
					}
				} else {
					sa.logger.Debug("Successfully retrieved content blob from storage",
						zap.String("sdHash", sdHash),
						zap.String("blobHash", blobHash),
						zap.Int("blobIndex", blobIndex),
						zap.Int("dataSize", len(blobData)),
					)
					return blobData, nil
				}
			}
		} else {
			sa.logger.Debug("Content blob not found in storage",
				zap.String("sdHash", sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", blobIndex),
				zap.Error(err),
			)
		}
	}

	// Acquire from network
	sa.logger.Info("Acquiring content blob from network",
		zap.String("sdHash", sdHash),
		zap.String("blobHash", blobHash),
		zap.Int("blobIndex", blobIndex),
	)

	blobData, err := sa.blobAcquirer.Acquire(ctx, blobHash)
	if err != nil {
		sa.logger.Error("Failed to acquire content blob",
			zap.String("sdHash", sdHash),
			zap.String("blobHash", blobHash),
			zap.Int("blobIndex", blobIndex),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to acquire content blob %s (index %d): %w", blobHash, blobIndex, err)
	}

	// Verify blob hash if verification is enabled
	if config.VerificationEnabled {
		if err := verifyBlobHash(blobData, blobHash); err != nil {
			sa.logger.Error("Content blob hash verification failed",
				zap.String("sdHash", sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", blobIndex),
				zap.Error(err),
			)
			return nil, fmt.Errorf("content blob hash verification failed for %s (index %d): %w", blobHash, blobIndex, err)
		}
	}

	// Store the blob if we have a store
	if sa.store != nil {
		if err := sa.store.Put(ctx, blobHash, blobData); err != nil {
			sa.logger.Debug("Failed to store acquired blob",
				zap.String("sdHash", sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", blobIndex),
				zap.String("store", sa.store.Name()),
				zap.Error(err),
			)
		} else {
			sa.logger.Debug("Successfully stored acquired blob",
				zap.String("sdHash", sdHash),
				zap.String("blobHash", blobHash),
				zap.Int("blobIndex", blobIndex),
				zap.String("store", sa.store.Name()),
			)
		}
	}

	sa.logger.Debug("Successfully acquired content blob",
		zap.String("sdHash", sdHash),
		zap.String("blobHash", blobHash),
		zap.Int("blobIndex", blobIndex),
		zap.Int("dataSize", len(blobData)),
	)

	return blobData, nil
}

// GetStreamResult retrieves full stream data (for backward compatibility)
func (sa *DefaultStreamAcquirer) GetStreamResult(ctx context.Context, sdHash string, opts ...AcquireOption) (*stream.StreamResult, error) {
	sa.logger.Info("Getting stream result",
		zap.String("sdHash", sdHash),
	)

	config := &AcquireConfig{
		Recursive:    false,
		RetryOptions: DefaultRetryOptions(),
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	sa.logger.Debug("Stream result acquisition configuration",
		zap.String("sdHash", sdHash),
		zap.Bool("recursive", config.Recursive),
		zap.Bool("verificationEnabled", config.VerificationEnabled),
	)

	// First acquire the SD blob with retry
	sa.logger.Info("Acquiring SD blob for stream result",
		zap.String("sdHash", sdHash),
	)

	var sdBlobData []byte
	var attemptCount int
	retryOptions := append(DefaultRetryOptions(),
		retry.OnRetry(func(n uint, err error) {
			attemptCount = int(n) + 1 // n is 0-based, so add 1 for human-readable attempt number
			sa.logger.Debug("Retrying SD blob acquisition",
				zap.String("sdHash", sdHash),
				zap.Int("attempt", attemptCount),
				zap.Error(err),
			)
		}),
	)
	err := WithRetry(ctx, retryOptions, func() error {
		acquiredData, acquireErr := sa.blobAcquirer.Acquire(ctx, sdHash)
		if acquireErr != nil {
			attemptCount++ // Increment for the current attempt
			return NewStreamError(OperationAcquireSDBlob, sdHash, "", acquireErr, attemptCount)
		}
		sdBlobData = acquiredData
		return nil
	})
	if err != nil {
		sa.logger.Error("Failed to acquire SD blob for stream result",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to acquire SD blob %s: %w", sdHash, err)
	}

	// Validate SD blob data
	if err := verifyAndValidateSDBlob(sdBlobData, sdHash, config.VerificationEnabled); err != nil {
		return nil, err
	}

	// Parse the SD blob
	var sdBlob stream.SDBlob
	if err := sdBlob.FromBlob(sdBlobData); err != nil {
		sa.logger.Error("Failed to parse SD blob for stream result",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to parse SD blob %s: %w", sdHash, err)
	}

	// Validate SD blob structure
	if len(sdBlob.BlobInfos) == 0 {
		sa.logger.Error("SD blob has no blob infos for stream result",
			zap.String("sdHash", sdHash),
		)
		return nil, fmt.Errorf("invalid SD blob %s: no blob infos found", sdHash)
	}

	sa.logger.Info("Successfully parsed SD blob for stream result",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", hex.EncodeToString(sdBlob.StreamHash)),
		zap.Int("blobCount", len(sdBlob.BlobInfos)),
	)

	// Verify SD blob hash if verification is enabled
	if config.VerificationEnabled {
		if err := verifyBlobHash(sdBlobData, sdHash); err != nil {
			return nil, fmt.Errorf("SD blob hash verification failed for %s: %w", sdHash, err)
		}
	}

	// Create the basic stream result
	result := &stream.StreamResult{
		SDBlob:     &sdBlob,
		SDBlobData: sdBlobData,
		SDBlobHash: sdHash,
		StreamHash: hex.EncodeToString(sdBlob.StreamHash),
	}

	// If not recursive, return just the SD blob metadata
	if !config.Recursive {
		return result, nil
	}

	// Recursive: fetch all content blobs
	contentBlobs := make([][]byte, 0, len(sdBlob.BlobInfos))
	contentHashes := make([]string, 0, len(sdBlob.BlobInfos))
	chunkSizes := make([]int, 0, len(sdBlob.BlobInfos))

	sa.logger.Info("Fetching content blobs recursively",
		zap.String("sdHash", sdHash),
		zap.Int("totalBlobs", len(sdBlob.BlobInfos)),
	)

	// Get all content blobs (excluding the terminating null blob)
	for i, blobInfo := range sdBlob.BlobInfos {
		// Skip the terminating null blob
		if blobInfo.Length == 0 {
			continue
		}

		blobHash := hex.EncodeToString(blobInfo.BlobHash)

		sa.logger.Debug("Processing content blob",
			zap.String("sdHash", sdHash),
			zap.String("blobHash", blobHash),
			zap.Int("blobIndex", i),
			zap.Int("blobLength", blobInfo.Length),
		)

		// Acquire blob with storage fallback
		blobData, err := sa.acquireBlobWithFallback(ctx, config, sdHash, blobHash, i, &blobInfo)
		if err != nil {
			return nil, err
		}

		contentBlobs = append(contentBlobs, blobData)
		contentHashes = append(contentHashes, blobHash)
		chunkSizes = append(chunkSizes, blobInfo.Length)
	}

	// Update the result with content information
	result.ContentBlobs = contentBlobs
	result.ContentHashes = contentHashes
	result.TotalChunks = len(contentBlobs)
	result.ChunkSizes = chunkSizes

	sa.logger.Info("Successfully created stream result",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", result.StreamHash),
		zap.Int("totalChunks", result.TotalChunks),
		zap.Bool("recursive", config.Recursive),
	)

	return result, nil
}

// GetSDBlob retrieves just the SD blob metadata without content blobs
func (sa *DefaultStreamAcquirer) GetSDBlob(ctx context.Context, sdHash string) (*stream.SDBlob, []byte, error) {
	sa.logger.Info("Getting SD blob metadata",
		zap.String("sdHash", sdHash),
	)

	// Validate input
	if err := ValidateBlobHash(sdHash); err != nil {
		sa.logger.Error("Invalid SD hash",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, nil, fmt.Errorf("invalid SD hash: %w", err)
	}

	// Acquire the SD blob
	sa.logger.Info("Acquiring SD blob metadata",
		zap.String("sdHash", sdHash),
	)

	var sdBlobData []byte
	err := WithRetry(ctx, DefaultRetryOptions(), func() error {
		acquiredData, acquireErr := sa.blobAcquirer.Acquire(ctx, sdHash)
		if acquireErr != nil {
			return NewStreamError(OperationAcquireSDBlob, sdHash, "", acquireErr, 1)
		}
		sdBlobData = acquiredData
		return nil
	})
	if err != nil {
		sa.logger.Error("Failed to acquire SD blob metadata",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, nil, fmt.Errorf("failed to acquire SD blob %s: %w", sdHash, err)
	}

	if err := verifyAndValidateSDBlob(sdBlobData, sdHash, true); err != nil {
		return nil, nil, err
	}

	// Parse the SD blob
	var sdBlob stream.SDBlob
	if err := sdBlob.FromBlob(sdBlobData); err != nil {
		sa.logger.Error("Failed to parse SD blob metadata",
			zap.String("sdHash", sdHash),
			zap.Error(err),
		)
		return nil, nil, fmt.Errorf("failed to parse SD blob %s: %w", sdHash, err)
	}

	// Validate SD blob structure
	if len(sdBlob.BlobInfos) == 0 {
		sa.logger.Error("SD blob metadata has no blob infos",
			zap.String("sdHash", sdHash),
		)
		return nil, nil, fmt.Errorf("invalid SD blob %s: no blob infos found", sdHash)
	}

	sa.logger.Info("Successfully retrieved SD blob metadata",
		zap.String("sdHash", sdHash),
		zap.String("streamHash", hex.EncodeToString(sdBlob.StreamHash)),
		zap.Int("blobCount", len(sdBlob.BlobInfos)),
	)

	return &sdBlob, sdBlobData, nil
}
