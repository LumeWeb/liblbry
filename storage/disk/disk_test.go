package disk

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/storage"
	"go.uber.org/zap"
)

// Common test data for blob operations
var testBlobs = []struct {
	hash string
	data []byte
}{
	{"adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59", []byte("test data 1")},
	{"89a232a9036e84b6b1608d39466563ae2b51b25d04e67a9a7abc00cf0474d48d454ac204e4109f2b198a8debb5fb8fe8", []byte("test data 2")},
	{"212798e26cf95ae045fbdc11ebcc6fd5efdba65aa274758d10fe3f68021514fe6c3b2b28a320bf162eb06b4cf0fddbe1", []byte("test data 3")},
	{"09d3cceaaebe1c051c2d80b9e9a7391af3ee95dcbb861ed811ef4d4e914bdf7b8bfd310ee0065e0c2f6fc4f31abf1a57", []byte("test data 4")},
}

// setupTestStore creates a temporary directory, logger, and disk store for testing
func setupTestStore(t *testing.T) (*DiskStore, string) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}
	return store, tempDir
}

// setupTestFactory creates a disk store factory for testing
func setupTestFactory(t *testing.T) *DiskStoreFactory {
	logger := zap.NewNop()
	factory := &DiskStoreFactory{logger: logger}
	return factory
}

// testInvalidHashes tests all invalid hash scenarios
func testInvalidHashes(t *testing.T, store *DiskStore) {
	t.Helper()

	invalidHashes := []struct {
		hash string
		desc string
	}{
		{"", "empty hash"},
		{"a", "too short (1 character)"},
		{"abc@", "invalid character @"},
		{"abc#", "invalid character #"},
		{"abc$", "invalid character $"},
		{"abc%", "invalid character %"},
		{"abc def", "space character"},
		{"../abc123", "path traversal"},
		{"..\\abc123", "windows path traversal"},
		{"/abc123", "absolute path"},
		{"\\abc123", "windows absolute path"},
		{"abc123/def456", "path separator"},
		{"abc123\\def456", "windows path separator"},
		{"abc123\000def456", "null byte"},
		{"abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd12345", "too long (97 characters)"},
	}

	for i, tc := range invalidHashes {
		t.Run(fmt.Sprintf("InvalidHash_%d_%s", i, tc.desc), func(t *testing.T) {
			hash := tc.hash
			// Test Put with invalid hash
			err := store.Put(hash, []byte("test"))
			require.Error(t, err, "Expected error for invalid hash: %s", hash)

			// Test PutSD with invalid hash
			err = store.PutSD(hash, []byte("test"))
			require.Error(t, err, "Expected error for invalid hash: %s", hash)

			// Test Get with invalid hash
			_, err = store.Get(hash)
			require.Error(t, err, "Expected error for invalid hash: %s", hash)

			// Test Has with invalid hash - should return false, nil (not an error)
			exists, err := store.Has(hash)
			require.NoError(t, err, "Has should not return an error for invalid hash: %s", hash)
			require.False(t, exists, "Has should return false for invalid hash: %s", hash)
		})
	}
}

// testPathTraversal tests path traversal attempts
func testPathTraversal(t *testing.T, store *DiskStore) {
	t.Helper()

	traversalAttempts := []struct {
		name string
		hash string
	}{
		{"DotDotSlash", "../abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"},
		{"DotDotBackslash", "..\\abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"},
		{"MultipleDotDot", "../../../abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"},
		{"DotDotInSubdir", "ab/../abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"},
		{"DotDotBackslashInSubdir", "ab\\..\\abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234"},
	}

	for _, attempt := range traversalAttempts {
		t.Run(attempt.name, func(t *testing.T) {
			// Test Put with path traversal attempt
			err := store.Put(attempt.hash, []byte("test"))
			require.Error(t, err, "Expected error for path traversal attempt: %s", attempt.name)

			// Test PutSD with path traversal attempt
			err = store.PutSD(attempt.hash, []byte("test"))
			require.Error(t, err, "Expected error for path traversal attempt: %s", attempt.name)
		})
	}
}

// setupSymlinkAttack sets up a symlink attack scenario and returns the symlink target path
func setupSymlinkAttack(t *testing.T, baseDir, hash string) string {
	t.Helper()

	// Skip on Windows
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on Windows")
	}

	// Test if symlinks are supported
	testLink := filepath.Join(baseDir, "testlink")
	testTarget := filepath.Join(baseDir, "testtarget")
	defer os.Remove(testLink)
	defer os.Remove(testTarget)

	err := os.MkdirAll(testTarget, 0755)
	if err != nil {
		t.Skipf("symlink test setup failed: %v", err)
	}

	err = os.Symlink(testTarget, testLink)
	if err != nil {
		if os.IsPermission(err) || err.Error() == "operation not supported" {
			t.Skipf("symlinks not supported: %v", err)
		}
		t.Fatalf("symlink test failed: %v", err)
	}

	// Create a directory that will be replaced with a symlink
	legitimateDir := filepath.Join(baseDir, hash[:2])
	err = os.MkdirAll(legitimateDir, 0755)
	require.NoError(t, err)

	// Remove the directory and replace it with a symlink pointing outside
	err = os.RemoveAll(legitimateDir)
	require.NoError(t, err)

	symlinkTarget := filepath.Join(baseDir, "..", "symlink_target")
	err = os.MkdirAll(symlinkTarget, 0755)
	require.NoError(t, err)

	err = os.Symlink(symlinkTarget, legitimateDir)
	require.NoError(t, err)

	return symlinkTarget
}

func TestDiskStoreFactory_CreateStore(t *testing.T) {
	// Test with valid config
	t.Run("ValidConfig", func(t *testing.T) {
		store, _ := setupTestStore(t)
		factory, err := liblbry.CreateStorageFactory[DiskStoreFactory](store.logger)
		require.NoError(t, err, "CreateStorageFactory failed")

		k := koanf.New(".")
		require.NoError(t, k.Set("path", store.path))
		config := k

		createdStore, err := factory.CreateStore(config)
		require.NoError(t, err, "CreateStore failed with valid config")

		if createdStore == nil {
			t.Fatal("CreateStore returned nil store")
		}

		if createdStore.Name() != "disk" {
			t.Errorf("Expected store name 'disk', got '%s'", createdStore.Name())
		}
	})

	// Test with missing path config
	t.Run("MissingPathConfig", func(t *testing.T) {
		factory := setupTestFactory(t)
		config := koanf.New(".")

		store, err := factory.CreateStore(config)
		require.Error(t, err, "Expected error for missing path config, but got none")

		if store != nil {
			t.Fatal("Expected nil store for missing path config")
		}
	})

	// Test with nil config
	t.Run("NilConfig", func(t *testing.T) {
		factory := setupTestFactory(t)
		var config *koanf.Koanf

		store, err := factory.CreateStore(config)
		require.Error(t, err, "Expected error for nil config, but got none")

		if store != nil {
			t.Fatal("Expected nil store for nil config")
		}
	})

	// Test Name method
	t.Run("NameMethod", func(t *testing.T) {
		factory := setupTestFactory(t)
		expectedName := "disk"
		actualName := factory.Name()

		if actualName != expectedName {
			t.Errorf("Expected factory name '%s', got '%s'", expectedName, actualName)
		}
	})
}

func TestDiskStore_PutAndGet(t *testing.T) {
	store, _ := setupTestStore(t)

	testCases := []struct {
		name      string
		hash      string
		data      []byte
		expectErr bool
	}{
		{
			name:      "ValidRegularBlob",
			hash:      "76fd253c8fd922886c60e2dfc1c7f47a213ad035b9622b9a3be5377e66eccf7c026919fccd771ca1d2b0a87bf4b4ce7b",
			data:      []byte("test data"),
			expectErr: false,
		},
		{
			name:      "ValidSDBlob",
			hash:      "47ebe801fbdae21f43239f0ec7c1b1d0e8072c3c72963078c22c447bac59e45cfa065581410e5a67390df90e615a27e2",
			data:      []byte("sd blob data"),
			expectErr: false,
		},
		{
			name:      "InvalidHash",
			hash:      "a",
			data:      []byte("test data"),
			expectErr: true,
		},
		{
			name:      "EmptyData",
			hash:      "7624adee9da4bdbeb48f63ce440eb4619cd8d128fc8d79b0d38013bad4d24f05b70660aebbc390e9f86f3e34b6eb8f01",
			data:      []byte{},
			expectErr: false,
		},
		{
			name:      "LargeData",
			hash:      "42bdb10fa69c9082304dd6af0881fcfe6f4c3cb5eebc75fdda49da043fb06b7d5701bea596f6f9b09bc420465eba1e25",
			data:      generateLargeData(1024 * 1024), // 1MB
			expectErr: false,
		},
		{
			name:      "SpecialCharacters",
			hash:      "86611c066b95318cd2ed08482cdb91b785cea7479564430d25cec003078fb412732f686380fdbae918c16490f5074fb3",
			data:      []byte("test data with special chars: \x00\x01\x02\xFF"),
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test Put
			err := store.Put(tc.hash, tc.data)
			if tc.expectErr {
				require.Error(t, err, "Expected error but got none")
				return
			}
			require.NoError(t, err, "Unexpected error")

			// Test Get
			retrievedData, err := store.Get(tc.hash)
			require.NoError(t, err, "Get failed")

			if string(retrievedData) != string(tc.data) {
				t.Error("Retrieved data doesn't match original data")
			}
		})
	}
}

func TestDiskStore_PutSDAndGet(t *testing.T) {
	store, _ := setupTestStore(t)

	testCases := []struct {
		name      string
		hash      string
		data      []byte
		expectErr bool
	}{
		{
			name:      "ValidSDBlob",
			hash:      "573c7c58aeecae156f23f760f280c509001b4586b00cc32b37f6edfe5c0d034c748bbf423ab71347ff3f5cb073a7230e",
			data:      []byte("sd blob test data"),
			expectErr: false,
		},
		{
			name:      "InvalidHash",
			hash:      "s",
			data:      []byte("sd blob test data"),
			expectErr: true,
		},
		{
			name:      "EmptySDBlob",
			hash:      "1c1e2c3ff5d5740054f39f10291a9eae6131851221312d3a1c550323f2631166737ee6a68e4c3ddc793527a84f25bdd8",
			data:      []byte{},
			expectErr: false,
		},
		{
			name:      "LargeSDBlob",
			hash:      "8c61dd29ff14d73b37ea12502ae708e5c76d614ad774998b4ba41904774ee212a60a434c12d5027ec83466c949d7a034",
			data:      generateLargeData(512 * 1024), // 512KB
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test PutSD
			err := store.PutSD(tc.hash, tc.data)
			if tc.expectErr {
				require.Error(t, err, "Expected error but got none")
				return
			}
			require.NoError(t, err, "Unexpected error")

			// Test Get for SD blob
			retrievedData, err := store.Get(tc.hash)
			require.NoError(t, err, "Get failed for SD blob")

			if string(retrievedData) != string(tc.data) {
				t.Error("Retrieved SD data doesn't match original data")
			}
		})
	}
}

func TestDiskStore_Has(t *testing.T) {
	store, _ := setupTestStore(t)

	// Put a test blob first
	testHash := "beb16f4ce7f9a4da1fb83a45c057ea4c2929c82603b9e20dd74487bd7bbb130d2d681ffe6fe1458a6cba2066ef9ef07e"
	testData := []byte("test data for Has method")
	err := store.Put(testHash, testData)
	require.NoError(t, err, "Failed to put test blob")

	// Put a test SD blob
	testSDHash := "dfe481fab5bead9258379952fcc0cee2d729a7e4e8e3e2e03a955d3453dbc7bf3d028ceb6ea94517522ca7f8561b01c3"
	testSDData := []byte("test sd data for Has method")
	err = store.PutSD(testSDHash, testSDData)
	require.NoError(t, err, "Failed to put test SD blob")

	testCases := []struct {
		name     string
		hash     string
		expected bool
	}{
		{
			name:     "ExistingRegularBlob",
			hash:     testHash,
			expected: true,
		},
		{
			name:     "ExistingSDBlob",
			hash:     testSDHash,
			expected: true,
		},
		{
			name:     "NonExistingBlob",
			hash:     "84eaddedccac7c406994a857e6c821f5ab3f0e700a81d716afd2570d11d5ef0e963152e95459ddc3637b4011ba3b38c2",
			expected: false,
		},
		{
			name:     "InvalidHash",
			hash:     "a",
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			exists, err := store.Has(tc.hash)
			require.NoError(t, err, "Has method failed")

			if exists != tc.expected {
				t.Errorf("Expected Has to return %v for hash '%s', got %v", tc.expected, tc.hash, exists)
			}
		})
	}
}

func TestDiskStore_Name(t *testing.T) {
	store, _ := setupTestStore(t)

	expectedName := "disk"
	actualName := store.Name()

	if actualName != expectedName {
		t.Errorf("Expected store name '%s', got '%s'", expectedName, actualName)
	}
}

func TestDiskStore_DirectoryStructure(t *testing.T) {
	store, _ := setupTestStore(t)

	regularHash := "ad515821f92ded8d5f0c0930adee45200427c4c553e9870236cbda73c2d883a5772ff7e4cacdb31d3f9b2b53809075f9"
	sdHash := "b9edcde802c93d2b1c61b45519e18556fff1a5cc8d7b83f289e6e0a6a00e0c903dfa0abd80989393d6e8e4e5b61b08b3"
	data := []byte("test data")

	// Test regular blob directory structure
	err := store.Put(regularHash, data)
	require.NoError(t, err, "Put failed")

	expectedRegularPath := filepath.Join(store.path, regularHash[:2], regularHash)
	if _, err := os.Stat(expectedRegularPath); os.IsNotExist(err) {
		t.Errorf("Regular blob was not stored in expected directory structure: %s", expectedRegularPath)
	}

	// Test SD blob directory structure
	err = store.PutSD(sdHash, data)
	require.NoError(t, err, "PutSD failed")

	expectedSDPath := filepath.Join(store.path, "sd", sdHash[:2], sdHash)
	if _, err := os.Stat(expectedSDPath); os.IsNotExist(err) {
		t.Errorf("SD blob was not stored in expected directory structure: %s", expectedSDPath)
	}
}

func TestDiskStore_GetNonExistentBlob(t *testing.T) {
	store, _ := setupTestStore(t)

	_, err := store.Get("6e58057919edc5f4830ae6520d965979d25126f6dd0508c3c08742d936131813fe2876b589aafda8a5bf38130e5665e5")
	require.Error(t, err, "Expected error when getting non-existent blob, but got none")
}

func TestDiskStore_HasNonExistentBlob(t *testing.T) {
	store, _ := setupTestStore(t)

	exists, err := store.Has("dae0fe98c8c3a773b6e68f1081350cbbc0fb6de799d13520148e1c6fe8dbc461cc6b6bbc1be51600cd71db5a50a91f1a")
	require.NoError(t, err, "Has failed")

	if exists {
		t.Error("Has returned true for non-existent blob")
	}
}

// Helper function to generate large data for testing
func generateLargeData(size int) []byte {
	data := make([]byte, size)
	_, err := rand.Read(data)
	if err != nil {
		panic(fmt.Sprintf("Failed to generate random data: %v", err))
	}
	return data
}

// Test that the BlobStore interface is properly implemented
func TestDiskStore_Interface(t *testing.T) {
	store, _ := setupTestStore(t)
	var _ storage.BlobStore = store
}

// Test that the StoreFactory interface is properly implemented
func TestDiskStoreFactory_Interface(t *testing.T) {
	factory := setupTestFactory(t)
	var _ storage.StoreFactory = factory
}

// Test hash validation with various invalid formats
func TestDiskStore_InvalidHashFormats(t *testing.T) {
	store, _ := setupTestStore(t)
	testInvalidHashes(t, store)
}

// Test path traversal attempts
func TestDiskStore_PathTraversalAttempts(t *testing.T) {
	store, _ := setupTestStore(t)
	testPathTraversal(t, store)
}

// Test symlink attack prevention
func TestDiskStore_SymlinkAttack(t *testing.T) {
	store, _ := setupTestStore(t)

	// Test symlink in regular blob path
	validHash := "1f39f474898e1ea8d75937452396321e518822252f7de28f568db3c967bb81380b357594c0ca231efb0a9bb5e665a2df"
	symlinkTarget := setupSymlinkAttack(t, store.path, validHash)
	defer os.RemoveAll(symlinkTarget)

	// Test that Put properly rejects symlinks in directory path
	err := store.Put(validHash, []byte("test data"))
	require.Error(t, err, "Expected error when trying to write to path with symlink directory")

	// Test symlink in SD blob path
	sdValidHash := "46d988f897d39bf56f9a158fd03ca524dc57ee9da5fb426f9e11bf61d737a895dd14710e42899dac96daf3863cbf8e15"

	// Create SD directory structure with symlink
	sdSubDir := filepath.Join(store.path, "sd", sdValidHash[:2])
	err = os.MkdirAll(filepath.Join(store.path, "sd"), 0755)
	require.NoError(t, err)

	// Remove the subdirectory and replace it with a symlink
	err = os.RemoveAll(filepath.Join(store.path, "sd", sdValidHash[:2]))
	require.NoError(t, err)

	sdSymlinkTarget := filepath.Join(store.path, "..", "sd_symlink_target")
	err = os.MkdirAll(sdSymlinkTarget, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(sdSymlinkTarget)

	err = os.Symlink(sdSymlinkTarget, sdSubDir)
	require.NoError(t, err)

	// Test that PutSD properly rejects symlinks in directory path
	err = store.PutSD(sdValidHash, []byte("test data"))
	require.Error(t, err, "Expected error when trying to write to SD path with symlink directory")
}

// Test edge cases for safeJoin function
func TestDiskStore_SafeJoinEdgeCases(t *testing.T) {
	_, tempDir := setupTestStore(t)

	// Test with valid hash
	expectedPath, err := safeJoin(tempDir, "3445b6abdb888a9b4bd4c0d91029a0d4ef6e0c2c2675fa07c0b7140d03b0bb3e48bd85b1d03a3da40dbf49e244a5eb06")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "34", "3445b6abdb888a9b4bd4c0d91029a0d4ef6e0c2c2675fa07c0b7140d03b0bb3e48bd85b1d03a3da40dbf49e244a5eb06"), expectedPath)

	// Test with invalid hash format
	_, err = safeJoin(tempDir, "invalid/hash")
	require.Error(t, err)

	_, err = safeJoin(tempDir, "../malicious")
	require.Error(t, err)

	_, err = safeJoin(tempDir, "..\\malicious")
	require.Error(t, err)
}

// Test edge cases for safeJoinSD function
func TestDiskStore_SafeJoinSDEdgeCases(t *testing.T) {
	_, tempDir := setupTestStore(t)

	// Test with valid hash
	expectedPath, err := safeJoinSD(tempDir, "6e899d1ccbfc8fc3c3b5387bd052d26fbdacb1df98cd6b108644b2d3a3126609a3ea2891a254c46aebeb97a3f8c090e3")
	require.NoError(t, err)
	require.Equal(t, filepath.Join(tempDir, "sd", "6e", "6e899d1ccbfc8fc3c3b5387bd052d26fbdacb1df98cd6b108644b2d3a3126609a3ea2891a254c46aebeb97a3f8c090e3"), expectedPath)

	// Test with invalid hash format
	_, err = safeJoinSD(tempDir, "invalid/hash")
	require.Error(t, err)

	_, err = safeJoinSD(tempDir, "../malicious")
	require.Error(t, err)

	_, err = safeJoinSD(tempDir, "..\\malicious")
	require.Error(t, err)
}

// Test List method
func TestDiskStore_List(t *testing.T) {
	store, _ := setupTestStore(t)

	// Test with empty store
	hashes, err := store.List(0, 10)
	require.NoError(t, err)
	assert.Empty(t, hashes)

	for _, blob := range testBlobs {
		err := store.Put(blob.hash, blob.data)
		require.NoError(t, err)
	}

	// Test listing all blobs
	hashes, err = store.List(0, 10)
	require.NoError(t, err)
	require.Len(t, hashes, 4)

	// Check that all hashes are present (order might vary)
	for _, blob := range testBlobs {
		found := false
		for _, hash := range hashes {
			if hash == blob.hash {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected hash %s in list", blob.hash)
	}

	// Test pagination - first page
	hashes, err = store.List(0, 2)
	require.NoError(t, err)
	require.Len(t, hashes, 2)

	// Test pagination - second page
	hashes, err = store.List(2, 2)
	require.NoError(t, err)
	require.Len(t, hashes, 2)

	// Test with offset beyond available data
	hashes, err = store.List(10, 5)
	require.NoError(t, err)
	assert.Empty(t, hashes)

	// Test error conditions
	_, err = store.List(-1, 5)
	require.Error(t, err)

	_, err = store.List(0, 0)
	require.Error(t, err)

	_, err = store.List(0, -5)
	require.Error(t, err)

	// Test with SD blobs
	sdBlobs := []struct {
		hash string
		data []byte
	}{
		{"56886c6339e484ae68ee3987b88423bf1441587397d4938e15c7f77d8ab18e7aaaa3f8d535692ed073392636cc957512", []byte("sd test data 1")},
		{"67183bc35c5c45fd6eec6ce37e9db1ed872676b964b2adb735ca374b57a125b39b109a607ccc087483c9ba3bca6adbae", []byte("sd test data 2")},
	}

	for _, blob := range sdBlobs {
		err := store.PutSD(blob.hash, blob.data)
		require.NoError(t, err)
	}

	// Test listing all blobs (regular + SD)
	hashes, err = store.List(0, 20)
	require.NoError(t, err)
	require.Len(t, hashes, 6) // 4 regular + 2 SD

	// Test that we can retrieve all the blobs we listed
	for _, hash := range hashes {
		_, err := store.Get(hash)
		require.NoError(t, err, "Should be able to retrieve blob with hash %s", hash)
	}

	// Test with duplicate hash in both regular and SD stores
	t.Run("List_DuplicateHashInBothStores", func(t *testing.T) {
		store, _ := setupTestStore(t)

		hash := "adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59"
		err := store.Put(hash, []byte("regular data"))
		require.NoError(t, err)

		err = store.PutSD(hash, []byte("sd data"))
		require.NoError(t, err)

		hashes, err := store.List(0, 10)
		require.NoError(t, err)

		// Count occurrences of the hash
		count := 0
		for _, h := range hashes {
			if h == hash {
				count++
			}
		}

		// Document expected behavior: should it appear once or twice?
		assert.Equal(t, 1, count, "Duplicate hash should appear only once in the list")
	})
}

// Test rejectSymlink function directly
func TestDiskStore_RejectSymlink(t *testing.T) {
	_, tempDir := setupTestStore(t)

	// Skip on Windows
	if runtime.GOOS == "windows" {
		t.Skip("symlink tests skipped on Windows")
	}

	// Test that safeJoin properly rejects symlinks in directory path
	validHash := "1f39f474898e1ea8d75937452396321e518822252f7de28f568db3c967bb81380b357594c0ca231efb0a9bb5e665a2df"

	symlinkTarget := setupSymlinkAttack(t, tempDir, validHash)
	defer os.RemoveAll(symlinkTarget)

	_, err := safeJoin(tempDir, validHash)
	require.Error(t, err, "Expected error when trying to join path with symlink directory")

	// Test that safeJoinSD properly rejects symlinks in directory path
	sdValidHash := "10c5d5b7e3dfec03a7f123a4a34d3fc4a06cae9f5fe6f288fb2baadfa4eb979b6e1653336f39cd6135e62172e29426ec"

	sdLegitimateDir := filepath.Join(tempDir, "sd", sdValidHash[:2])
	err = os.MkdirAll(filepath.Join(tempDir, "sd"), 0755)
	require.NoError(t, err)

	// Remove the subdirectory and replace it with a symlink
	err = os.RemoveAll(filepath.Join(tempDir, "sd", sdValidHash[:2]))
	require.NoError(t, err)

	sdSymlinkTarget := filepath.Join(tempDir, "..", "sd_symlink_target")
	err = os.MkdirAll(sdSymlinkTarget, 0755)
	require.NoError(t, err)
	defer os.RemoveAll(sdSymlinkTarget)

	err = os.Symlink(sdSymlinkTarget, sdLegitimateDir)
	require.NoError(t, err)

	// Test that safeJoinSD properly rejects symlinks in directory path
	_, err = safeJoinSD(tempDir, sdValidHash)
	require.Error(t, err, "Expected error when trying to join SD path with symlink directory")
}

// Test Delete method
func TestDiskStore_Delete(t *testing.T) {
	// Delete existing regular blob
	t.Run("DeleteExistingRegularBlob", func(t *testing.T) {
		store, _ := setupTestStore(t)
		hash := "adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59"
		data := []byte("test data 1")
		
		err := store.Put(hash, data)
		require.NoError(t, err)
		
		// Verify blob exists
		exists, err := store.Has(hash)
		require.NoError(t, err)
		assert.True(t, exists)
		
		// Delete the blob
		err = store.Delete(hash)
		require.NoError(t, err)
		
		// Verify blob is deleted
		exists, err = store.Has(hash)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Delete existing SD blob
	t.Run("DeleteExistingSDBlob", func(t *testing.T) {
		store, _ := setupTestStore(t)
		hash := "89a232a9036e84b6b1608d39466563ae2b51b25d04e67a9a7abc00cf0474d48d454ac204e4109f2b198a8debb5fb8fe8"
		data := []byte("test data 2")
		
		err := store.PutSD(hash, data)
		require.NoError(t, err)
		
		// Verify blob exists
		exists, err := store.Has(hash)
		require.NoError(t, err)
		assert.True(t, exists)
		
		// Delete the blob
		err = store.Delete(hash)
		require.NoError(t, err)
		
		// Verify blob is deleted
		exists, err = store.Has(hash)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Delete blob that exists in both regular and SD stores
	t.Run("DeleteBlobInBothStores", func(t *testing.T) {
		store, _ := setupTestStore(t)
		hash := "212798e26cf95ae045fbdc11ebcc6fd5efdba65aa274758d10fe3f68021514fe6c3b2b28a320bf162eb06b4cf0fddbe1"
		regularData := []byte("regular data")
		sdData := []byte("sd data")
		
		err := store.Put(hash, regularData)
		require.NoError(t, err)
		
		err = store.PutSD(hash, sdData)
		require.NoError(t, err)
		
		// Verify blob exists
		exists, err := store.Has(hash)
		require.NoError(t, err)
		assert.True(t, exists)
		
		// Delete the blob
		err = store.Delete(hash)
		require.NoError(t, err)
		
		// Verify blob is deleted
		exists, err = store.Has(hash)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Delete non-existent blob (should be no-op)
	t.Run("DeleteNonExistentBlob", func(t *testing.T) {
		store, _ := setupTestStore(t)
		hash := "09d3cceaaebe1c051c2d80b9e9a7391af3ee95dcbb861ed811ef4d4e914bdf7b8bfd310ee0065e0c2f6fc4f31abf1a57"
		
		// Delete non-existent blob (should be no-op)
		err := store.Delete(hash)
		require.NoError(t, err)
		
		// Verify blob still doesn't exist
		exists, err := store.Has(hash)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Delete with invalid hash format (should return ErrInvalidHash)
	t.Run("DeleteWithInvalidHash", func(t *testing.T) {
		store, _ := setupTestStore(t)
		
		invalidHashes := []struct {
			hash string
			desc string
		}{
			{"", "empty hash"},
			{"a", "too short"},
			{"abc@", "invalid character"},
			{"../abc123", "path traversal"},
			{"abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd1234abcd12345", "too long"},
		}
		
		for _, tc := range invalidHashes {
			t.Run(tc.desc, func(t *testing.T) {
				err := store.Delete(tc.hash)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid hash")
			})
		}
	})

	// Delete from empty store
	t.Run("DeleteFromEmptyStore", func(t *testing.T) {
		store, _ := setupTestStore(t)
		
		// Delete from empty store (should be no-op)
		err := store.Delete("adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59")
		require.NoError(t, err)
	})

	// Delete multiple blobs in sequence
	t.Run("DeleteMultipleBlobs", func(t *testing.T) {
		store, _ := setupTestStore(t)
		
		// Add multiple blobs
		blob1 := "adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59"
		blob2 := "89a232a9036e84b6b1608d39466563ae2b51b25d04e67a9a7abc00cf0474d48d454ac204e4109f2b198a8debb5fb8fe8"
		blob3 := "212798e26cf95ae045fbdc11ebcc6fd5efdba65aa274758d10fe3f68021514fe6c3b2b28a320bf162eb06b4cf0fddbe1"
		
		err := store.Put(blob1, []byte("data1"))
		require.NoError(t, err)
		
		err = store.PutSD(blob2, []byte("data2"))
		require.NoError(t, err)
		
		err = store.Put(blob3, []byte("data3"))
		require.NoError(t, err)
		
		// Verify all blobs exist
		exists, err := store.Has(blob1)
		require.NoError(t, err)
		assert.True(t, exists)
		
		exists, err = store.Has(blob2)
		require.NoError(t, err)
		assert.True(t, exists)
		
		exists, err = store.Has(blob3)
		require.NoError(t, err)
		assert.True(t, exists)
		
		// Delete blobs sequentially
		err = store.Delete(blob1)
		require.NoError(t, err)
		
		err = store.Delete(blob2)
		require.NoError(t, err)
		
		err = store.Delete(blob3)
		require.NoError(t, err)
		
		// Verify all blobs are deleted
		exists, err = store.Has(blob1)
		require.NoError(t, err)
		assert.False(t, exists)
		
		exists, err = store.Has(blob2)
		require.NoError(t, err)
		assert.False(t, exists)
		
		exists, err = store.Has(blob3)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Verify blob is actually deleted (file doesn't exist after Delete)
	t.Run("VerifyBlobIsActuallyDeleted", func(t *testing.T) {
		store, tempDir := setupTestStore(t)
		hash := "09d3cceaaebe1c051c2d80b9e9a7391af3ee95dcbb861ed811ef4d4e914bdf7b8bfd310ee0065e0c2f6fc4f31abf1a57"
		data := []byte("test data")
		
		err := store.Put(hash, data)
		require.NoError(t, err)
		
		// Verify file exists before deletion
		blobPath := filepath.Join(tempDir, hash[:2], hash)
		_, err = os.Stat(blobPath)
		require.NoError(t, err, "File should exist before deletion")
		
		// Delete the blob
		err = store.Delete(hash)
		require.NoError(t, err)
		
		// Verify file is actually deleted
		_, err = os.Stat(blobPath)
		assert.True(t, os.IsNotExist(err), "File should not exist after deletion")
	})

	// Verify Delete doesn't affect other blobs
	t.Run("DeleteDoesntAffectOtherBlobs", func(t *testing.T) {
		store, _ := setupTestStore(t)
		
		// Add multiple blobs
		blob1 := "adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59"
		blob2 := "89a232a9036e84b6b1608d39466563ae2b51b25d04e67a9a7abc00cf0474d48d454ac204e4109f2b198a8debb5fb8fe8"
		blob3 := "212798e26cf95ae045fbdc11ebcc6fd5efdba65aa274758d10fe3f68021514fe6c3b2b28a320bf162eb06b4cf0fddbe1"
		
		err := store.Put(blob1, []byte("data1"))
		require.NoError(t, err)
		
		err = store.PutSD(blob2, []byte("data2"))
		require.NoError(t, err)
		
		err = store.Put(blob3, []byte("data3"))
		require.NoError(t, err)
		
		// Verify all blobs exist
		exists, err := store.Has(blob1)
		require.NoError(t, err)
		assert.True(t, exists)
		
		exists, err = store.Has(blob2)
		require.NoError(t, err)
		assert.True(t, exists)
		
		exists, err = store.Has(blob3)
		require.NoError(t, err)
		assert.True(t, exists)
		
		// Delete one blob
		err = store.Delete(blob2)
		require.NoError(t, err)
		
		// Verify other blobs still exist
		exists, err = store.Has(blob1)
		require.NoError(t, err)
		assert.True(t, exists)
		
		exists, err = store.Has(blob3)
		require.NoError(t, err)
		assert.True(t, exists)
		
		// Verify deleted blob doesn't exist
		exists, err = store.Has(blob2)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	// Test with both regular and SD blob directories
	t.Run("TestBothRegularAndSDDirectories", func(t *testing.T) {
		store, tempDir := setupTestStore(t)
		
		regularHash := "adabf83a9b41323d9ea4a3e91debe037d20733d88af579076a37e024123f24f869d9a2e27f958bb293b179d141a27f59"
		sdHash := "89a232a9036e84b6b1608d39466563ae2b51b25d04e67a9a7abc00cf0474d48d454ac204e4109f2b198a8debb5fb8fe8"
		
		regularData := []byte("regular data")
		sdData := []byte("sd data")
		
		// Store blobs
		err := store.Put(regularHash, regularData)
		require.NoError(t, err)
		
		err = store.PutSD(sdHash, sdData)
		require.NoError(t, err)
		
		// Verify directories exist
		regularDir := filepath.Join(tempDir, regularHash[:2])
		sdDir := filepath.Join(tempDir, "sd", sdHash[:2])
		
		_, err = os.Stat(regularDir)
		require.NoError(t, err, "Regular blob directory should exist")
		
		_, err = os.Stat(sdDir)
		require.NoError(t, err, "SD blob directory should exist")
		
		// Delete blobs
		err = store.Delete(regularHash)
		require.NoError(t, err)
		
		err = store.Delete(sdHash)
		require.NoError(t, err)
		
		// Verify blobs are deleted
		exists, err := store.Has(regularHash)
		require.NoError(t, err)
		assert.False(t, exists)
		
		exists, err = store.Has(sdHash)
		require.NoError(t, err)
		assert.False(t, exists)
		
		// Verify files are deleted (directories may remain)
		regularPath := filepath.Join(regularDir, regularHash)
		sdPath := filepath.Join(sdDir, sdHash)
		
		_, err = os.Stat(regularPath)
		assert.True(t, os.IsNotExist(err), "Regular blob file should be deleted")
		
		_, err = os.Stat(sdPath)
		assert.True(t, os.IsNotExist(err), "SD blob file should be deleted")
	})
}
