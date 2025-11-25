// Package disk implements a disk-based blob storage backend.
//
// The disk storage organizes blobs in a two-level directory structure based on
// the first two characters of the blob hash. Regular blobs are stored directly
// under the base path, while SD blobs are stored in an "sd" subdirectory.
//
// Example directory structure:
//
//	storage/
//	  ├── ab/
//	  │   ├── abc123... (regular blob)
//	  │   └── abd456... (regular blob)
//	  ├── cd/
//	  │   └── cde789... (regular blob)
//	  └── sd/
//	      ├── ef/
//	      │   └── efg123... (SD blob)
//	      └── gh/
//	          └── ghi456... (SD blob)
package disk

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/knadh/koanf/v2"
	"github.com/samber/lo"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
	"go.lumeweb.com/liblbry/storage"
	"go.uber.org/zap"
)

// SDDirectory is the name of the subdirectory used for storing SD blobs.
const SDDirectory = "sd"

var safeHashRe = regexp.MustCompile(`^[a-fA-F0-9]{96}$`)

// DiskStore implements the BlobStore interface using the file system.
//
// It provides methods to store, retrieve, and check for the existence of blobs
// on disk. Blobs are stored in a hierarchical directory structure to prevent
// having too many files in a single directory.
type DiskStore struct {
	path   string
	logger *zap.Logger
}

// DiskStoreFactory implements the StoreFactory interface for creating DiskStore instances.
//
// It creates DiskStore objects configured with a base storage path.
type DiskStoreFactory struct {
	logger *zap.Logger
}

// validateHash checks if the hash format is valid.
//
// A valid hash is a 96-character hexadecimal string.
func validateHash(hash string) bool {
	return safeHashRe.MatchString(hash)
}

// validateHashWithError combines hash validation with error creation.
//
// It checks if the hash format is valid and returns an error if not.
// The operation parameter is used for logging purposes.
func validateHashWithError(hash string, operation string) error {
	if !validateHash(hash) {
		return liblbryerrors.ErrInvalidHash
	}
	return nil
}

// writeWithContext writes data to a file while respecting context cancellation.
//
// For small data, it writes directly. For larger data, it writes in chunks
// to allow context cancellation during the write operation.
func writeWithContext(ctx context.Context, file *os.File, data []byte) error {
	// For small data, write directly with retry for partial writes
	if len(data) <= 4096 {
		for written := 0; written < len(data); {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := file.Write(data[written:])
			if err != nil {
				return err
			}
			written += n
		}
		return nil
	}

	// For larger data, write in chunks to allow context cancellation
	const chunkSize = 4096
	for i := 0; i < len(data); i += chunkSize {
		// Check context before each chunk
		if err := ctx.Err(); err != nil {
			return err
		}

		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}

		// Write the chunk with retry for partial writes
		for written := 0; written < end-i; {
			if err := ctx.Err(); err != nil {
				return err
			}
			n, err := file.Write(data[i+written : end])
			if err != nil {
				return err
			}
			written += n
		}
	}

	return nil
}

// atomicWrite handles the atomic write pattern used in Put/PutSD.
//
// It writes data to a temporary file first and then renames it to the final path,
// ensuring that the file is either completely written or not written at all.
// The operation parameter is used for logging purposes.
// Respects context cancellation during file operations.
func atomicWrite(ctx context.Context, path string, data []byte, logger *zap.Logger, operation string) error {
	// Check context before starting operation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create directory if it doesn't exist
	dir := filepath.Dir(path)

	if err := os.MkdirAll(dir, 0755); err != nil {
		logErrorIfLogger(logger, "failed to create directory for "+operation+" operation", err, zap.String("dir", dir))
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Check context before creating temp file
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create temporary file in the same directory as the final file
	tmp, err := os.CreateTemp(dir, ".tmp-")
	if err != nil {
		logErrorIfLogger(logger, "failed to create temporary file for "+operation+" operation", err, zap.String("dir", dir))
		return fmt.Errorf("failed to create temporary file: %w", err)
	}
	tmpPath := tmp.Name()

	// Use cleanup flag to control whether we remove the temp file
	cleanup := true
	defer func() {
		if cleanup {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	// Write data to temporary file with context cancellation support
	if err := writeWithContext(ctx, tmp, data); err != nil {
		logErrorIfLogger(logger, "failed to write temporary file for "+operation+" operation", err, zap.String("tmpPath", tmpPath))
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Check context before sync
	if err := ctx.Err(); err != nil {
		return err
	}

	// Sync file to disk
	if err := tmp.Sync(); err != nil {
		logErrorIfLogger(logger, "failed to sync temporary file for "+operation+" operation", err, zap.String("tmpPath", tmpPath))
		return fmt.Errorf("failed to sync temporary file: %w", err)
	}

	// Check context before close
	if err := ctx.Err(); err != nil {
		return err
	}

	// Close file before rename (required on Windows)
	if err := tmp.Close(); err != nil {
		logErrorIfLogger(logger, "failed to close temporary file for "+operation+" operation", err, zap.String("tmpPath", tmpPath))
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	// Set restrictive permissions (owner read/write only)
	if err := os.Chmod(tmpPath, 0600); err != nil {
		logErrorIfLogger(logger, "failed to set permissions on temporary file for "+operation+" operation", err, zap.String("tmpPath", tmpPath))
		return fmt.Errorf("failed to set file permissions: %w", err)
	}

	// Check context before rename
	if err := ctx.Err(); err != nil {
		return err
	}

	// Rename temporary file to final path
	if err := os.Rename(tmpPath, path); err != nil {
		logErrorIfLogger(logger, "failed to rename temporary file for "+operation+" operation", err, zap.String("tmpPath", tmpPath), zap.String("path", path))
		return fmt.Errorf("failed to rename temporary file: %w", err)
	}

	// Only if rename succeeds do we skip cleanup
	cleanup = false

	// Best-effort sync of parent directory to ensure rename is durable
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}

	return nil
}

// readWithContext reads a file while respecting context cancellation.
//
// For small files, it reads directly. For larger files, it reads in chunks
// to allow context cancellation during the read operation.
func readWithContext(ctx context.Context, path string) ([]byte, error) {
	// Get file info to determine size
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	// For small files, read directly
	if info.Size() <= 4096 {
		return os.ReadFile(path)
	}

	// For larger files, open and read in chunks
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Check context after opening file
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Read in chunks to allow context cancellation
	const chunkSize = 4096
	var data []byte
	buffer := make([]byte, chunkSize)

	for {
		// Check context before each read
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		n, err := file.Read(buffer)
		if err != nil && err != io.EOF {
			return nil, err
		}

		if n == 0 {
			break
		}

		data = append(data, buffer[:n]...)
	}

	return data, nil
}

// checkBlobPaths checks both regular and SD blob paths, returns data if found.
//
// It first attempts to find a regular blob, and if not found, tries to find an SD blob.
// Returns the data, any error encountered, and a boolean indicating if the blob was found.
// The operation parameter is used for logging purposes.
// Respects context cancellation during file operations.
func checkBlobPaths(ctx context.Context, basePath, hash string, logger *zap.Logger, operation string) ([]byte, error, bool) {
	// Check context before starting operation
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}

	// Try to get regular blob with safe path construction
	blobPath, err := safeJoin(basePath, hash)
	if err != nil {
		logDebugIfLogger(logger, "unsafe path detected for "+operation+" operation", zap.String("hash", hash), zap.Error(err))
		return nil, err, false
	}

	// Check context before reading file
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}

	data, err := readWithContext(ctx, blobPath)
	if err == nil {
		logDebugIfLogger(logger, "retrieved regular blob", zap.String("hash", hash), zap.String("path", blobPath))
		return data, nil, true
	}
	if !os.IsNotExist(err) {
		logErrorIfLogger(logger, "failed to read regular blob", err, zap.String("hash", hash), zap.String("path", blobPath))
		return nil, err, false
	}

	// Check context before trying SD blob
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}

	// If regular blob doesn't exist, try SD blob
	sdBlobPath, err := safeJoinSD(basePath, hash)
	if err != nil {
		logDebugIfLogger(logger, "unsafe path detected for "+operation+" operation (SD blob)", zap.String("hash", hash), zap.Error(err))
		return nil, err, false
	}

	// Check context before reading SD file
	if err := ctx.Err(); err != nil {
		return nil, err, false
	}

	data, err = readWithContext(ctx, sdBlobPath)
	if err == nil {
		logDebugIfLogger(logger, "retrieved SD blob", zap.String("hash", hash), zap.String("path", sdBlobPath))
		return data, nil, true
	}
	if !os.IsNotExist(err) {
		logErrorIfLogger(logger, "failed to read SD blob", err, zap.String("hash", hash), zap.String("path", sdBlobPath))
		return nil, err, false
	}

	// If neither exists, return not found error
	logDebugIfLogger(logger, "blob not found for "+operation+" operation", zap.String("hash", hash))
	return nil, fmt.Errorf("blob not found: %s", hash), false
}

// logDebugIfLogger eliminates repeated logger nil checks for debug logs.
//
// It logs a debug message only if the logger is not nil.
func logDebugIfLogger(logger *zap.Logger, msg string, fields ...zap.Field) {
	if logger != nil {
		logger.Debug(msg, fields...)
	}
}

// logErrorIfLogger eliminates repeated logger nil checks for error logs.
//
// It logs an error message only if the logger is not nil.
func logErrorIfLogger(logger *zap.Logger, msg string, err error, fields ...zap.Field) {
	if logger != nil {
		logger.Error(msg, append(fields, zap.Error(err))...)
	}
}

// safeJoin safely joins paths and ensures they don't escape the base directory.
//
// It validates the hash format, constructs a safe path, and checks for path traversal attempts.
// It also verifies that directories in the path are not symlinks.
func safeJoin(base, hash string) (string, error) {
	// Validate hash format
	if !validateHash(hash) {
		return "", liblbryerrors.ErrInvalidHash
	}

	// Create the expected path
	subDir := hash[:2]
	expectedPath := filepath.Join(base, subDir, hash)

	// Clean the path to resolve any ".." or "." components
	cleanPath := filepath.Clean(expectedPath)

	// Ensure the cleaned path is still within the base directory using Rel to handle all cases
	rel, err := filepath.Rel(filepath.Clean(base), cleanPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("path traversal attempt detected: %s", hash)
	}

	// Check for symlinks in the base directory and subdirectory
	subDirPath := filepath.Join(base, subDir)
	if err := rejectSymlink(subDirPath); err != nil {
		return "", err
	}

	return cleanPath, nil
}

// safeJoinSD safely joins paths for SD blobs and ensures they don't escape the base directory.
//
// It validates the hash format, constructs a safe path for SD blobs, and checks for path traversal attempts.
// It also verifies that directories in the path (base, sd, and subdirectory) are not symlinks.
func safeJoinSD(base, hash string) (string, error) {
	// Validate hash format
	if !validateHash(hash) {
		return "", liblbryerrors.ErrInvalidHash
	}

	// Create the expected path for SD blob
	sdDir := SDDirectory
	subDir := hash[:2]
	expectedPath := filepath.Join(base, sdDir, subDir, hash)

	// Clean the path to resolve any ".." or "." components
	cleanPath := filepath.Clean(expectedPath)

	// Ensure the cleaned path is still within the base directory using Rel to handle all cases
	rel, err := filepath.Rel(filepath.Clean(base), cleanPath)
	if err != nil || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("path traversal attempt detected: %s", hash)
	}

	// Check for symlinks in the base directory, sd directory, and subdirectory
	sdDirPath := filepath.Join(base, sdDir)
	subDirPath := filepath.Join(base, sdDir, subDir)

	if err := rejectSymlink(sdDirPath); err != nil {
		return "", err
	}

	if err := rejectSymlink(subDirPath); err != nil {
		return "", err
	}

	return cleanPath, nil
}

// rejectSymlink checks if the given path is a symlink.
//
// It returns an error if the path is a symlink, which helps prevent symlink-based attacks.
func rejectSymlink(path string) error {
	// Check if this path is a symlink
	if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("symlink detected in path: %s", path)
	}
	return nil
}

// CreateStore creates a new DiskStore instance from the provided configuration.
//
// It validates the configuration, creates the storage directory if it doesn't exist,
// and initializes a new DiskStore with the specified path.
//
// Parameters:
//   - config: A koanf configuration object containing the storage path
//
// Returns:
//   - storage.BlobStore: A new DiskStore instance
//   - error: Any error encountered during store creation, or an error if the configuration is invalid
func (f DiskStoreFactory) CreateStore(config *koanf.Koanf) (storage.BlobStore, error) {
	if config == nil {
		err := fmt.Errorf("configuration cannot be nil")
		if f.logger != nil {
			f.logger.Error("failed to create disk store", zap.Error(err))
		}
		return nil, err
	}

	path := config.String("path")
	if path == "" {
		err := fmt.Errorf("missing or invalid 'path' configuration parameter")
		if f.logger != nil {
			f.logger.Error("failed to create disk store", zap.Error(err))
		}
		return nil, err
	}

	// Clean and validate the base path first
	cleanPath := filepath.Clean(path)
	if err := rejectSymlink(cleanPath); err != nil {
		if f.logger != nil {
			f.logger.Error("failed to create disk store - path contains symlink", zap.String("path", cleanPath), zap.Error(err))
		}
		return nil, err
	}

	// Create the directory if it doesn't exist (using cleaned path)
	if err := os.MkdirAll(cleanPath, 0755); err != nil {
		err = fmt.Errorf("failed to create storage directory: %w", err)
		if f.logger != nil {
			f.logger.Error("failed to create disk store", zap.Error(err), zap.String("path", cleanPath))
		}
		return nil, err
	}

	if f.logger != nil {
		f.logger.Debug("created disk store", zap.String("path", cleanPath))
	}

	return &DiskStore{path: cleanPath, logger: f.logger}, nil
}

// Name returns the name of the factory.
//
// This method is part of the StoreFactory interface.
func (f DiskStoreFactory) Name() string {
	if f.logger != nil {
		f.logger.Debug("returning disk store factory name")
	}
	return "disk"
}

// SetLogger configures the factory with a logger.
//
// This method allows setting a logger for the factory after creation.
func (f *DiskStoreFactory) SetLogger(logger *zap.Logger) {
	f.logger = logger
}

// GetLogger returns the factory's logger.
//
// This method returns the logger currently configured for the factory.
func (f *DiskStoreFactory) GetLogger() *zap.Logger {
	return f.logger
}

// Has checks if a blob exists in the disk store.
//
// It first attempts to check for a regular blob, and if not found, tries to check for an SD blob.
// Invalid hash formats are handled gracefully by returning false with no error.
//
// Parameters:
//   - hash: The 96-character hexadecimal hash of the blob to check
//
// Returns:
//   - bool: True if the blob exists, false otherwise
//   - error: Any error encountered during the check operation
func (d *DiskStore) Has(ctx context.Context, hash string) (bool, error) {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Validate hash format
	if !validateHash(hash) {
		logDebugIfLogger(d.logger, "invalid hash format for Has operation", zap.String("hash", hash))
		return false, nil
	}

	// Check context before checking regular blob
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Check regular blob with safe path construction
	blobPath, err := safeJoin(d.path, hash)
	if err != nil {
		logErrorIfLogger(d.logger, "unsafe path detected for Has operation", err, zap.String("hash", hash))
		return false, err
	}

	// Check context before stat operation
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if _, err := os.Stat(blobPath); err == nil {
		logDebugIfLogger(d.logger, "found regular blob", zap.String("hash", hash), zap.String("path", blobPath))
		return true, nil
	} else if !os.IsNotExist(err) {
		logErrorIfLogger(d.logger, "failed to stat regular blob", err, zap.String("hash", hash), zap.String("path", blobPath))
		return false, err
	}

	// Check context before checking SD blob
	if err := ctx.Err(); err != nil {
		return false, err
	}

	// Check SD blob with safe path construction
	sdBlobPath, err := safeJoinSD(d.path, hash)
	if err != nil {
		logErrorIfLogger(d.logger, "unsafe path detected for Has operation (SD blob)", err, zap.String("hash", hash))
		return false, err
	}

	// Check context before SD stat operation
	if err := ctx.Err(); err != nil {
		return false, err
	}

	if _, err := os.Stat(sdBlobPath); err == nil {
		logDebugIfLogger(d.logger, "found SD blob", zap.String("hash", hash), zap.String("path", sdBlobPath))
		return true, nil
	} else if !os.IsNotExist(err) {
		logErrorIfLogger(d.logger, "failed to stat SD blob", err, zap.String("hash", hash), zap.String("path", sdBlobPath))
		return false, err
	}

	logDebugIfLogger(d.logger, "blob not found", zap.String("hash", hash))
	return false, nil
}

// Get retrieves a blob from the disk store.
//
// It first attempts to retrieve a regular blob, and if not found, tries to retrieve an SD blob.
// Returns the blob data and any error encountered during retrieval.
//
// Parameters:
//   - hash: The 96-character hexadecimal hash of the blob to retrieve
//
// Returns:
//   - []byte: The blob data
//   - error: Any error encountered during retrieval, or an error if the hash format is invalid
func (d *DiskStore) Get(ctx context.Context, hash string) ([]byte, error) {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate hash format
	if err := validateHashWithError(hash, "Get"); err != nil {
		logDebugIfLogger(d.logger, "invalid hash format for Get operation", zap.String("hash", hash))
		return nil, err
	}

	data, err, found := checkBlobPaths(ctx, d.path, hash, d.logger, "Get")
	if found {
		return data, err
	}

	return nil, err
}

// Put stores a blob in the disk store using atomic write.
//
// The blob is stored in a hierarchical directory structure based on the first
// two characters of the hash. The write operation is atomic, using a temporary
// file that is renamed to the final path.
//
// Parameters:
//   - hash: The 96-character hexadecimal hash of the blob
//   - data: The blob data to store
//
// Returns:
//   - error: Any error encountered during storage, or an error if the hash format is invalid
func (d *DiskStore) Put(ctx context.Context, hash string, data []byte) error {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate hash format
	if err := validateHashWithError(hash, "Put"); err != nil {
		logDebugIfLogger(d.logger, "invalid hash format for Put operation", zap.String("hash", hash))
		return err
	}

	// Check context before path creation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create safe path for the blob
	blobPath, err := safeJoin(d.path, hash)
	if err != nil {
		logDebugIfLogger(d.logger, "unsafe path detected for Put operation", zap.String("hash", hash), zap.Error(err))
		return err
	}

	if err := atomicWrite(ctx, blobPath, data, d.logger, "Put"); err != nil {
		return err
	}

	logDebugIfLogger(d.logger, "stored blob", zap.String("hash", hash), zap.String("path", blobPath))

	return nil
}

// PutSD stores an SD blob in the disk store using atomic write.
//
// SD blobs are stored in a separate "sd" subdirectory with the same hierarchical
// structure as regular blobs. The write operation is atomic, using a temporary
// file that is renamed to the final path.
//
// Parameters:
//   - hash: The 96-character hexadecimal hash of the SD blob
//   - data: The SD blob data to store
//
// Returns:
//   - error: Any error encountered during storage, or an error if the hash format is invalid
func (d *DiskStore) PutSD(ctx context.Context, hash string, data []byte) error {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate hash format
	if err := validateHashWithError(hash, "PutSD"); err != nil {
		logDebugIfLogger(d.logger, "invalid hash format for PutSD operation", zap.String("hash", hash))
		return err
	}

	// Check context before path creation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create safe path for the SD blob
	sdBlobPath, err := safeJoinSD(d.path, hash)
	if err != nil {
		logDebugIfLogger(d.logger, "unsafe path detected for PutSD operation", zap.String("hash", hash), zap.Error(err))
		return err
	}

	if err := atomicWrite(ctx, sdBlobPath, data, d.logger, "PutSD"); err != nil {
		return err
	}

	logDebugIfLogger(d.logger, "stored SD blob", zap.String("hash", hash), zap.String("path", sdBlobPath))

	return nil
}

// Name returns the name of the store.
//
// This method is part of the BlobStore interface.
func (d *DiskStore) Name() string {
	if d.logger != nil {
		d.logger.Debug("returning disk store name")
	}
	return "disk"
}

// List returns a list of blob hashes with pagination support
func (d *DiskStore) List(ctx context.Context, offset, limit int) ([]string, error) {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if offset < 0 {
		return nil, liblbryerrors.ErrInvalidOffset
	}
	if limit <= 0 {
		return nil, liblbryerrors.ErrInvalidLimit
	}

	// Collect all blob hashes from the disk store
	allHashes, err := d.collectBlobHashes(ctx)
	if err != nil {
		return nil, liblbryerrors.Err("failed to collect blob hashes: %w", err)
	}

	// Apply pagination
	start := offset
	if start > len(allHashes) {
		return nil, liblbryerrors.ErrEndOfList
	}

	end := start + limit
	if end > len(allHashes) {
		end = len(allHashes)
	}

	return allHashes[start:end], nil
}

// walkWithContext is a context-aware version of filepath.Walk.
//
// It checks for context cancellation before processing each path entry
// and returns context.Canceled if the context is cancelled during traversal.
func walkWithContext(ctx context.Context, root string, walkFn filepath.WalkFunc) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return err
	}

	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		// Check context before processing each path
		if err := ctx.Err(); err != nil {
			return err
		}

		// Call the original walk function
		return walkFn(path, info, err)
	})
}

// collectBlobHashes walks the disk store directory structure and collects all blob hashes
// Respects context cancellation during directory traversal.
func (d *DiskStore) collectBlobHashes(ctx context.Context) ([]string, error) {
	// Use a map to track unique hashes for deduplication
	hashSet := make(map[string]struct{})

	// Walk the directory structure to find all blobs with context cancellation support
	err := walkWithContext(ctx, d.path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			sdDirPath := filepath.Join(d.path, SDDirectory)
			if path == d.path || path == sdDirPath {
				return nil
			}
			return nil
		}

		// Extract the hash from the file path (it's the filename)
		filename := filepath.Base(path)
		// Validate that it's a valid hash format
		if validateHash(filename) {
			hashSet[filename] = struct{}{}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Convert to sorted slice for deterministic pagination
	allHashes := lo.Keys(hashSet)
	sort.Strings(allHashes)

	return allHashes, nil
}

// Delete removes a blob from storage.
// If the blob exists in both regular and SD blob stores, it removes both.
// If the blob is not found, it returns nil (no-op).
// Only returns an error for invalid hash format or other unknown errors.
func (d *DiskStore) Delete(ctx context.Context, hash string) error {
	// Check context before validation
	if err := ctx.Err(); err != nil {
		return err
	}

	// Validate hash format
	if err := validateHashWithError(hash, "Delete"); err != nil {
		logDebugIfLogger(d.logger, "invalid hash format for Delete operation", zap.String("hash", hash))
		return err
	}

	// Check context before deleting regular blob
	if err := ctx.Err(); err != nil {
		return err
	}

	// Try to delete regular blob
	blobPath, err := safeJoin(d.path, hash)
	if err != nil {
		logErrorIfLogger(d.logger, "unsafe path detected for Delete operation", err, zap.String("hash", hash))
		return err
	}

	// Check context before remove operation
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Remove(blobPath); err != nil && !os.IsNotExist(err) {
		logErrorIfLogger(d.logger, "failed to delete regular blob", err, zap.String("hash", hash), zap.String("path", blobPath))
		return fmt.Errorf("failed to delete regular blob: %w", err)
	}

	// Check context before deleting SD blob
	if err := ctx.Err(); err != nil {
		return err
	}

	// Try to delete SD blob
	sdBlobPath, err := safeJoinSD(d.path, hash)
	if err != nil {
		logErrorIfLogger(d.logger, "unsafe path detected for Delete operation (SD blob)", err, zap.String("hash", hash))
		return err
	}

	// Check context before SD remove operation
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := os.Remove(sdBlobPath); err != nil && !os.IsNotExist(err) {
		logErrorIfLogger(d.logger, "failed to delete SD blob", err, zap.String("hash", hash), zap.String("path", sdBlobPath))
		return fmt.Errorf("failed to delete SD blob: %w", err)
	}

	logDebugIfLogger(d.logger, "deleted blob(s)", zap.String("hash", hash))
	return nil
}
