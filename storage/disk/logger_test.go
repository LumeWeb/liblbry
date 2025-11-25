package disk

import (
	"testing"

	"github.com/knadh/koanf/v2"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/liblbry"
	"go.lumeweb.com/liblbry/storage"
	"go.uber.org/zap"
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
	testHash := "84eaddedccac7c406994a857e6c821f5ab3f0e700a81d716afd2570d11d5ef0e963152e95459ddc3637b4011ba3b38c2"

	err = store.Put(t.Context(), testHash, testData)
	require.NoError(t, err, "Failed to put data")

	// Test 2: Logger properly configured from config
	logger = zap.NewNop()

	configWithLogger := koanf.New(".")
	require.NoError(t, configWithLogger.Set("path", t.TempDir()))
	require.NoError(t, configWithLogger.Set("logger", logger))

	storeWithLogger, err := factory.CreateStore(configWithLogger)
	require.NoError(t, err, "Failed to create store with logger")

	// Test putting and getting data with actual logger
	err = storeWithLogger.Put(t.Context(), testHash, testData)
	require.NoError(t, err, "Failed to put data with logger")

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
		factory := setupTestFactory(t)
		k := koanf.New(".")
		require.NoError(t, k.Set("path", t.TempDir()))
		require.NoError(t, k.Set("logger", factory.logger))
		config := k

		store, err := factory.CreateStore(config)
		require.NoError(t, err, "CreateStore failed with valid config")

		if store == nil {
			t.Fatal("CreateStore returned nil store")
		}

		if store.Name() != "disk" {
			t.Errorf("Expected store name 'disk', got '%s'", store.Name())
		}
	})

	// Test with config without logger
	t.Run("WithoutLoggerConfig", func(t *testing.T) {
		factory := setupTestFactory(t)
		k := koanf.New(".")
		require.NoError(t, k.Set("path", t.TempDir()))
		config := k

		store, err := factory.CreateStore(config)
		require.NoError(t, err, "CreateStore failed without logger config")

		if store == nil {
			t.Fatal("CreateStore returned nil store without logger config")
		}

		if store.Name() != "disk" {
			t.Errorf("Expected store name 'disk', got '%s'", store.Name())
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

// Test that the BlobStore interface is properly implemented
func TestLoggerStore_Interface(t *testing.T) {
	store, _ := setupTestStore(t)
	var _ storage.BlobStore = store
}

// Test that the StoreFactory interface is properly implemented
func TestLoggerStoreFactory_Interface(t *testing.T) {
	factory := setupTestFactory(t)
	var _ storage.StoreFactory = factory
}
