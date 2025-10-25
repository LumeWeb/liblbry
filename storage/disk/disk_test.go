package disk

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/knadh/koanf/v2"
	"go.lumeweb.com/liblbry"
	"go.uber.org/zap"
	"github.com/stretchr/testify/require"
)

func TestDiskStoreFactory_CreateStore(t *testing.T) {
	// Test with valid config
	t.Run("ValidConfig", func(t *testing.T) {
		tempDir := t.TempDir()
		logger := zap.NewNop()
		factory, err := liblbry.CreateStorageFactory[DiskStoreFactory](logger)
		if err != nil {
			t.Fatalf("CreateStorageFactory failed: %v", err)
		}
		k := koanf.New(".")
		require.NoError(t, k.Set("path", tempDir))
		config := k

		store, err := factory.CreateStore(config)
		if err != nil {
			t.Fatalf("CreateStore failed with valid config: %v", err)
		}

		if store == nil {
			t.Fatal("CreateStore returned nil store")
		}

		if store.Name() != "disk" {
			t.Errorf("Expected store name 'disk', got '%s'", store.Name())
		}
	})

	// Test with missing path config
	t.Run("MissingPathConfig", func(t *testing.T) {
		logger := zap.NewNop()
		factory := &DiskStoreFactory{logger: logger}
		config := koanf.New(".")

		store, err := factory.CreateStore(config)
		if err == nil {
			t.Fatal("Expected error for missing path config, but got none")
		}

		if store != nil {
			t.Fatal("Expected nil store for missing path config")
		}
	})

	// Test with nil config
	t.Run("NilConfig", func(t *testing.T) {
		logger := zap.NewNop()
		factory := &DiskStoreFactory{logger: logger}
		var config *koanf.Koanf

		store, err := factory.CreateStore(config)
		if err == nil {
			t.Fatal("Expected error for nil config, but got none")
		}

		if store != nil {
			t.Fatal("Expected nil store for nil config")
		}
	})

	// Test Name method
	t.Run("NameMethod", func(t *testing.T) {
		logger := zap.NewNop()
		factory := &DiskStoreFactory{logger: logger}
		expectedName := "disk"
		actualName := factory.Name()

		if actualName != expectedName {
			t.Errorf("Expected factory name '%s', got '%s'", expectedName, actualName)
		}
	})
}

func TestDiskStore_PutAndGet(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	testCases := []struct {
		name      string
		hash      string
		data      []byte
		expectErr bool
	}{
		{
			name:      "ValidRegularBlob",
			hash:      "abcd1234",
			data:      []byte("test data"),
			expectErr: false,
		},
		{
			name:      "ValidSDBlob",
			hash:      "1234abcd",
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
			hash:      "emptydata",
			data:      []byte{},
			expectErr: false,
		},
		{
			name:      "LargeData",
			hash:      "largedata",
			data:      generateLargeData(1024 * 1024), // 1MB
			expectErr: false,
		},
		{
			name:      "SpecialCharacters",
			hash:      "specialchars",
			data:      []byte("test data with special chars: \x00\x01\x02\xFF"),
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test Put
			err := store.Put(tc.hash, tc.data)
			if tc.expectErr && err == nil {
				t.Fatal("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Skip Get test if Put failed as expected
			if tc.expectErr {
				return
			}

			// Test Get
			retrievedData, err := store.Get(tc.hash)
			if err != nil {
				t.Fatalf("Get failed: %v", err)
			}

			if string(retrievedData) != string(tc.data) {
				t.Error("Retrieved data doesn't match original data")
			}
		})
	}
}

func TestDiskStore_PutSDAndGet(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	testCases := []struct {
		name      string
		hash      string
		data      []byte
		expectErr bool
	}{
		{
			name:      "ValidSDBlob",
			hash:      "sd1234abcd",
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
			hash:      "sdempty",
			data:      []byte{},
			expectErr: false,
		},
		{
			name:      "LargeSDBlob",
			hash:      "sdlarge",
			data:      generateLargeData(512 * 1024), // 512KB
			expectErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test PutSD
			err := store.PutSD(tc.hash, tc.data)
			if tc.expectErr && err == nil {
				t.Fatal("Expected error but got none")
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			// Skip Get test if PutSD failed as expected
			if tc.expectErr {
				return
			}

			// Test Get for SD blob
			retrievedData, err := store.Get(tc.hash)
			if err != nil {
				t.Fatalf("Get failed for SD blob: %v", err)
			}

			if string(retrievedData) != string(tc.data) {
				t.Error("Retrieved SD data doesn't match original data")
			}
		})
	}
}

func TestDiskStore_Has(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	// Put a test blob first
	testHash := "has123456"
	testData := []byte("test data for Has method")
	err := store.Put(testHash, testData)
	if err != nil {
		t.Fatalf("Failed to put test blob: %v", err)
	}

	// Put a test SD blob
	testSDHash := "sdhas123456"
	testSDData := []byte("test sd data for Has method")
	err = store.PutSD(testSDHash, testSDData)
	if err != nil {
		t.Fatalf("Failed to put test SD blob: %v", err)
	}

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
			hash:     "nonexistent",
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
			if err != nil {
				t.Fatalf("Has method failed: %v", err)
			}

			if exists != tc.expected {
				t.Errorf("Expected Has to return %v for hash '%s', got %v", tc.expected, tc.hash, exists)
			}
		})
	}
}

func TestDiskStore_Name(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	expectedName := "disk"
	actualName := store.Name()

	if actualName != expectedName {
		t.Errorf("Expected store name '%s', got '%s'", expectedName, actualName)
	}
}

func TestDiskStore_DirectoryStructure(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	regularHash := "abcdef1234567890"
	sdHash := "123456abcdef7890"
	data := []byte("test data")

	// Test regular blob directory structure
	err := store.Put(regularHash, data)
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	expectedRegularPath := filepath.Join(tempDir, regularHash[:2], regularHash)
	if _, err := os.Stat(expectedRegularPath); os.IsNotExist(err) {
		t.Errorf("Regular blob was not stored in expected directory structure: %s", expectedRegularPath)
	}

	// Test SD blob directory structure
	err = store.PutSD(sdHash, data)
	if err != nil {
		t.Fatalf("PutSD failed: %v", err)
	}

	expectedSDPath := filepath.Join(tempDir, "sd", sdHash[:2], sdHash)
	if _, err := os.Stat(expectedSDPath); os.IsNotExist(err) {
		t.Errorf("SD blob was not stored in expected directory structure: %s", expectedSDPath)
	}
}

func TestDiskStore_GetNonExistentBlob(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	_, err := store.Get("nonexistentblob")
	if err == nil {
		t.Fatal("Expected error when getting non-existent blob, but got none")
	}
}

func TestDiskStore_HasNonExistentBlob(t *testing.T) {
	tempDir := t.TempDir()
	logger := zap.NewNop()
	store := &DiskStore{path: tempDir, logger: logger}

	exists, err := store.Has("nonexistentblob")
	if err != nil {
		t.Fatalf("Has failed: %v", err)
	}

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
	tempDir := t.TempDir()
	logger := zap.NewNop()
	var _ liblbry.BlobStore = &DiskStore{path: tempDir, logger: logger}
}

// Test that the StoreFactory interface is properly implemented
func TestDiskStoreFactory_Interface(t *testing.T) {
	logger := zap.NewNop()
	var _ liblbry.StoreFactory = &DiskStoreFactory{logger: logger}
}

