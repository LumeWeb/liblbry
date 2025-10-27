package memory

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore(t *testing.T) {
	store := NewMemoryStore()

	// Test Name method
	t.Run("Name", func(t *testing.T) {
		assert.Equal(t, "memory", store.Name())
	})

	// Test Has method
	t.Run("Has", func(t *testing.T) {
		tests := []struct {
			name     string
			hash     string
			data     []byte
			expected bool
			setup    bool
		}{
			{
				name:     "existing blob",
				hash:     "a2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6c",
				data:     []byte("test data 1"),
				expected: true,
				setup:    true,
			},
			{
				name:     "non-existing blob",
				hash:     "0c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5cb",
				expected: false,
				setup:    false,
			},
			{
				name:     "empty hash",
				hash:     "000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
				expected: false,
				setup:    false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.setup {
					err := store.Put(tt.hash, tt.data)
					require.NoError(t, err)
				}

				exists, err := store.Has(tt.hash)
				require.NoError(t, err)
				assert.Equal(t, tt.expected, exists)
			})
		}
	})

	// Test Get method
	t.Run("Get", func(t *testing.T) {
		tests := []struct {
			name        string
			hash        string
			data        []byte
			expectError bool
			setup       bool
		}{
			{
				name:        "existing blob",
				hash:        "b2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6d",
				data:        []byte("test data 2"),
				expectError: false,
				setup:       true,
			},
			{
				name:        "non-existing blob",
				hash:        "1c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5cc",
				expectError: true,
				setup:       false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if tt.setup {
					err := store.Put(tt.hash, tt.data)
					require.NoError(t, err)
				}

				data, err := store.Get(tt.hash)
				if tt.expectError {
					assert.Error(t, err)
					assert.Nil(t, data)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.data, data)
				}
			})
		}
	})

	// Test Put method
	t.Run("Put", func(t *testing.T) {
		tests := []struct {
			name        string
			hash        string
			data        []byte
			expectError bool
		}{
			{
				name:        "valid data",
				hash:        "c2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6e",
				data:        []byte("test data 3"),
				expectError: false,
			},
			{
				name:        "empty data",
				hash:        "2c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5cd",
				data:        []byte{},
				expectError: true,
			},
			{
				name:        "nil data",
				hash:        "3c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5ce",
				data:        nil,
				expectError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := store.Put(tt.hash, tt.data)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)

					// Verify the data was stored
					data, err := store.Get(tt.hash)
					require.NoError(t, err)
					assert.Equal(t, tt.data, data)
				}
			})
		}
	})

	// Test PutSD method
	t.Run("PutSD", func(t *testing.T) {
		tests := []struct {
			name        string
			hash        string
			data        []byte
			expectError bool
		}{
			{
				name:        "valid SD data",
				hash:        "d2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e6f",
				data:        []byte("sd test data 1"),
				expectError: false,
			},
			{
				name:        "empty SD data",
				hash:        "4c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5cf",
				data:        []byte{},
				expectError: true,
			},
			{
				name:        "nil SD data",
				hash:        "5c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5d0",
				data:        nil,
				expectError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := store.PutSD(tt.hash, tt.data)
				if tt.expectError {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)

					// Verify the SD data was stored
					data, err := store.Get(tt.hash)
					require.NoError(t, err)
					assert.Equal(t, tt.data, data)
				}
			})
		}
	})

	// Test integration between regular blobs and SD blobs
	t.Run("BlobSDIntegration", func(t *testing.T) {
		store := NewMemoryStore()

		regularHash := "e2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e60"
		sdHash := "6c9675ad7f40f29dcd41883ed9cf7e145bbb13976d9b83ab9354f4f61a87f0f7771a56724c2aa7a5ab43c68d7942e5d1"
		regularData := []byte("regular data")
		sdData := []byte("sd data")

		// Put regular blob
		err := store.Put(regularHash, regularData)
		require.NoError(t, err)

		// Put SD blob
		err = store.PutSD(sdHash, sdData)
		require.NoError(t, err)

		// Verify both exist
		exists, err := store.Has(regularHash)
		require.NoError(t, err)
		assert.True(t, exists)

		exists, err = store.Has(sdHash)
		require.NoError(t, err)
		assert.True(t, exists)

		// Verify data is correct
		data, err := store.Get(regularHash)
		require.NoError(t, err)
		assert.Equal(t, regularData, data)

		data, err = store.Get(sdHash)
		require.NoError(t, err)
		assert.Equal(t, sdData, data)
	})

	// Test concurrent access
	t.Run("ConcurrentAccess", func(t *testing.T) {
		store := NewMemoryStore()
		const goroutines = 100
		const testData = "concurrent test data"

		// Put initial data
		err := store.Put("f2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e67", []byte(testData))
		require.NoError(t, err)

		// Test concurrent Has calls
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				exists, err := store.Has("f2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e67")
				assert.NoError(t, err)
				assert.True(t, exists)
			}()
		}

		wg.Wait()

		// Test concurrent Get calls
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				data, err := store.Get("f2f1841bb9c5f3b583ac3b8c07ee1a5bf9cc48923721c30d5ca6318615776c284e8936d72fa4db7fdda2e4e9598b1e67")
				assert.NoError(t, err)
				assert.Equal(t, testData, string(data))
			}()
		}

		wg.Wait()

		// Test concurrent Put calls
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			go func(id int) {
				defer wg.Done()
				// Create valid 96-character hex hash
				hash := fmt.Sprintf("%096x", id)[:96]
				// Ensure hash is exactly 96 characters
				for len(hash) < 96 {
					hash += "0"
				}
				data := []byte(fmt.Sprintf("data_%d", id))
				err := store.Put(hash, data)
				assert.NoError(t, err)

				// Verify data was stored
				storedData, err := store.Get(hash)
				assert.NoError(t, err)
				assert.Equal(t, data, storedData)
			}(i)
		}

		wg.Wait()
	})

}
