package disk

import (
	"testing"

	"github.com/knadh/koanf/v2"
	"go.lumeweb.com/liblbry"
	"go.uber.org/zap"
	"github.com/stretchr/testify/require"
)

func TestLoggerIntegration(t *testing.T) {
	// Test 1: Default no-op logger works when no logger provided
	logger := zap.NewNop()
	factory, err := liblbry.CreateStorageFactory[DiskStoreFactory](logger)
	if err != nil {
		t.Fatalf("CreateStorageFactory failed: %v", err)
	}
	
	tempDir := t.TempDir()
	
	// Test with no logger in config (should use no-op logger)
	k := koanf.New(".")
	require.NoError(t, k.Set("path", tempDir))
	config := k
	
	store, err := factory.CreateStore(config)
	if err != nil {
		t.Fatalf("Failed to create store with no logger: %v", err)
	}
	
	// Test putting and getting data (should not panic even with no-op logger)
	testData := []byte("test data")
	testHash := "testhash1234567890"
	
	err = store.Put(testHash, testData)
	if err != nil {
		t.Fatalf("Failed to put data: %v", err)
	}
	
	// Test 2: Logger properly configured from config
	logger = zap.NewNop()
	
	configWithLogger := koanf.New(".")
	require.NoError(t, configWithLogger.Set("path", tempDir+"_with_logger"))
	require.NoError(t, configWithLogger.Set("logger", logger))
	
	storeWithLogger, err := factory.CreateStore(configWithLogger)
	if err != nil {
		t.Fatalf("Failed to create store with logger: %v", err)
	}
	
	// Test putting and getting data with actual logger
	err = storeWithLogger.Put(testHash, testData)
	if err != nil {
		t.Fatalf("Failed to put data with logger: %v", err)
	}
	
	// Test 3: Verify both stores implement the interface
	if store.Name() != "disk" {
		t.Errorf("Expected store name 'disk', got '%s'", store.Name())
	}
	
	if storeWithLogger.Name() != "disk" {
		t.Errorf("Expected store name 'disk', got '%s'", storeWithLogger.Name())
	}
}

func TestLoggerStoreFactory_CreateStore(t *testing.T) {
	// Test with valid config including logger
	t.Run("WithLoggerConfig", func(t *testing.T) {
		tempDir := t.TempDir()
		logger := zap.NewNop()
		factory := &DiskStoreFactory{}
		k := koanf.New(".")
		require.NoError(t, k.Set("path", tempDir))
		require.NoError(t, k.Set("logger", logger))
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

	// Test with config without logger
	t.Run("WithoutLoggerConfig", func(t *testing.T) {
		tempDir := t.TempDir()
		factory := &DiskStoreFactory{}
		k := koanf.New(".")
		require.NoError(t, k.Set("path", tempDir))
		config := k

		store, err := factory.CreateStore(config)
		if err != nil {
			t.Fatalf("CreateStore failed without logger config: %v", err)
		}

		if store == nil {
			t.Fatal("CreateStore returned nil store without logger config")
		}

		if store.Name() != "disk" {
			t.Errorf("Expected store name 'disk', got '%s'", store.Name())
		}
	})

	// Test with nil config
	t.Run("NilConfig", func(t *testing.T) {
		factory := &DiskStoreFactory{}
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
		factory := &DiskStoreFactory{}
		expectedName := "disk"
		actualName := factory.Name()

		if actualName != expectedName {
			t.Errorf("Expected factory name '%s', got '%s'", expectedName, actualName)
		}
	})
}

// Test that the BlobStore interface is properly implemented
func TestLoggerStore_Interface(t *testing.T) {
	var _ liblbry.BlobStore = &DiskStore{}
}

// Test that the StoreFactory interface is properly implemented
func TestLoggerStoreFactory_Interface(t *testing.T) {
	var _ liblbry.StoreFactory = &DiskStoreFactory{}
}
