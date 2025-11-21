package watchdog

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	liblbrytesting "go.lumeweb.com/liblbry/internal/testing"
	"go.uber.org/zap/zaptest"
)

func TestDefaultWatchdogConfig(t *testing.T) {
	config := DefaultConfig()

	assert.Equal(t, []int{3333, 4444, 5567}, config.TestPorts)
	assert.Equal(t, 30*time.Minute, config.CacheTimeout)
	assert.Equal(t, 24*time.Hour, config.BlacklistTimeout)
	assert.Equal(t, 10*time.Minute, config.CleanupInterval)
	assert.Equal(t, 5*time.Second, config.TestTimeout)
	assert.Nil(t, config.Logger)
}

func TestDHTWatchdogInterface(t *testing.T) {
	var _ DHTWatchdog = &DefaultDHTWatchdog{}
	var _ dht.ContactValidator = &DefaultDHTWatchdog{}
}

func TestWatchdogOptions(t *testing.T) {
	logger := zaptest.NewLogger(t)

	config := DefaultConfig()
	WithTestPorts(8080, 9090)(config)
	WithCacheTimeout(time.Hour)(config)
	WithBlacklistTimeout(2 * time.Hour)(config)
	WithCleanupInterval(time.Minute)(config)
	WithTestTimeout(time.Second)(config)
	WithLogger(logger)(config)

	assert.Equal(t, []int{8080, 9090}, config.TestPorts)
	assert.Equal(t, time.Hour, config.CacheTimeout)
	assert.Equal(t, 2*time.Hour, config.BlacklistTimeout)
	assert.Equal(t, time.Minute, config.CleanupInterval)
	assert.Equal(t, time.Second, config.TestTimeout)
	assert.Equal(t, logger, config.Logger)
}

func TestNew(t *testing.T) {
	logger := zaptest.NewLogger(t)
	watchdog := New(
		WithTestPorts(8080),
		WithCacheTimeout(time.Minute),
		WithLogger(logger),
	)
	defer watchdog.Stop()

	assert.Equal(t, "dht_watchdog", watchdog.Name())

	stats := watchdog.GetStats()
	assert.Equal(t, 0, stats.CachedContacts)
	assert.Equal(t, 0, stats.BlacklistedContacts)
}

func TestValidateContactForHash(t *testing.T) {
	// Use random port to avoid conflicts with existing services
	testPort := liblbrytesting.GetFreePort(t)
	watchdog := New(
		WithTestPorts(testPort),
		WithTestTimeout(time.Second),
	)
	defer watchdog.Stop()

	contact := createTestContact(t)
	blobHash := bits.Bitmap{9, 8, 7}

	// Should fail due to no actual server
	result := watchdog.ValidateContactForHash(blobHash, *contact)
	assert.False(t, result)

	// Contact should be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))
}

func TestValidateContact_Cached(t *testing.T) {
	// Use random port to avoid conflicts with existing services
	testPort := liblbrytesting.GetFreePort(t)
	watchdog := New(
		WithTestPorts(testPort),
		WithCacheTimeout(time.Minute),
		WithTestTimeout(time.Second),
	)
	defer watchdog.Stop()

	contact := createTestContact(t)
	ctx := context.Background()

	// First validation runs against a non-listening port, so it should fail
	// and cause the contact to be blacklisted.
	result := watchdog.ValidateAndUpdateContact(ctx, contact)
	assert.False(t, result) // Should fail due to no actual server

	// Contact should now be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))
	assert.False(t, watchdog.IsCached(contact.ID))

	// Remove from blacklist and manually add to cache for testing
	watchdog.RemoveFromBlacklist(contact.ID)

	// Manually add to cache to test cached path
	watchdog.AddToCache(contact, time.Now())

	// Now validation should return true due to cache
	result = watchdog.ValidateAndUpdateContact(ctx, contact)
	assert.True(t, result)
}

func TestValidateContact_Blacklisted(t *testing.T) {
	// Use random port to avoid conflicts with existing services
	testPort := liblbrytesting.GetFreePort(t)
	watchdog := New(
		WithTestPorts(testPort),
		WithTestTimeout(time.Second),
	)
	defer watchdog.Stop()

	contact := createTestContact(t)
	ctx := context.Background()

	// First validation should fail and blacklist
	result := watchdog.ValidateAndUpdateContact(ctx, contact)
	assert.False(t, result)

	// Contact should be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// Second validation should return false due to blacklist
	result = watchdog.ValidateAndUpdateContact(ctx, contact)
	assert.False(t, result)
}

func TestIsCached(t *testing.T) {
	watchdog := New(
		WithCacheTimeout(time.Minute),
	)
	defer watchdog.Stop()

	contact := createTestContact(t)

	// Initially not cached
	assert.False(t, watchdog.IsCached(contact.ID))

	// Add to cache
	watchdog.AddToCache(contact, time.Now())

	// Should be cached
	assert.True(t, watchdog.IsCached(contact.ID))

	// Test expiration by adding an expired entry
	watchdog.AddToCache(contact, time.Now().Add(-2*time.Minute))
	assert.False(t, watchdog.IsCached(contact.ID))
}

func TestIsBlacklisted(t *testing.T) {
	watchdog := New(
		WithBlacklistTimeout(time.Minute),
	)
	defer watchdog.Stop()

	contact := createTestContact(t)

	// Initially not blacklisted
	assert.False(t, watchdog.IsBlacklisted(contact.ID))

	// Add to blacklist
	watchdog.AddToBlacklist(contact.ID, time.Now())

	// Should be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// Test expiration by adding an expired entry
	watchdog.AddToBlacklist(contact.ID, time.Now().Add(-2*time.Minute))
	assert.False(t, watchdog.IsBlacklisted(contact.ID))
}

func TestRemoveFromCache(t *testing.T) {
	watchdog := New()
	defer watchdog.Stop()

	contact := createTestContact(t)

	// Add to cache
	watchdog.AddToCache(contact, time.Now())
	assert.True(t, watchdog.IsCached(contact.ID))

	// Remove from cache
	watchdog.RemoveFromCache(contact.ID)
	assert.False(t, watchdog.IsCached(contact.ID))
}

func TestRemoveFromBlacklist(t *testing.T) {
	watchdog := New()
	defer watchdog.Stop()

	contact := createTestContact(t)

	// Add to blacklist
	watchdog.AddToBlacklist(contact.ID, time.Now())
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// Remove from blacklist
	watchdog.RemoveFromBlacklist(contact.ID)
	assert.False(t, watchdog.IsBlacklisted(contact.ID))
}

func TestCleanupExpired(t *testing.T) {
	watchdog := New(
		WithCacheTimeout(time.Minute),
		WithBlacklistTimeout(time.Minute),
	)
	defer watchdog.Stop()

	contact1 := createTestContact(t)
	contact2 := createTestContact(t)
	contact2.ID = bits.Bitmap{9, 8, 7} // Different ID

	// Add expired cache entry
	watchdog.AddToCache(contact1, time.Now().Add(-2*time.Minute))

	// Add valid cache entry
	watchdog.AddToCache(contact2, time.Now())

	// Add expired blacklist entry
	watchdog.AddToBlacklist(contact1.ID, time.Now().Add(-2*time.Minute))

	// Add valid blacklist entry
	watchdog.AddToBlacklist(contact2.ID, time.Now())

	// Before cleanup
	stats := watchdog.GetStats()
	assert.Equal(t, 2, stats.CachedContacts)
	assert.Equal(t, 2, stats.BlacklistedContacts)

	// Run cleanup
	watchdog.CleanupExpired()

	// After cleanup - only valid entries should remain
	stats = watchdog.GetStats()
	assert.Equal(t, 1, stats.CachedContacts)
	assert.Equal(t, 1, stats.BlacklistedContacts)
	assert.True(t, watchdog.IsCached(contact2.ID))
	assert.True(t, watchdog.IsBlacklisted(contact2.ID))
	assert.False(t, watchdog.IsCached(contact1.ID))
	assert.False(t, watchdog.IsBlacklisted(contact1.ID))
}

func TestGetStats(t *testing.T) {
	watchdog := New()
	defer watchdog.Stop()

	contact := createTestContact(t)

	// Add some entries
	watchdog.AddToCache(contact, time.Now())
	watchdog.AddToBlacklist(contact.ID, time.Now())

	stats := watchdog.GetStats()
	assert.Equal(t, 1, stats.CachedContacts)
	assert.Equal(t, 1, stats.BlacklistedContacts)
	assert.False(t, stats.LastCleanupTime.IsZero())
}

func TestConcurrentAccess(t *testing.T) {
	watchdog := New(
		WithTestTimeout(time.Millisecond * 100),
	)
	defer watchdog.Stop()

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 5

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				contact := createTestContact(t)
				contact.ID = bits.Bitmap{byte(i), byte(j)} // Unique ID per goroutine/operation

				ctx := context.Background()
				watchdog.ValidateAndUpdateContact(ctx, contact)
				watchdog.IsCached(contact.ID)
				watchdog.IsBlacklisted(contact.ID)
				watchdog.GetStats()
			}
		}()
	}

	wg.Wait()

	// Should not panic and stats should be reasonable
	stats := watchdog.GetStats()
	assert.True(t, stats.CachedContacts >= 0)
	assert.True(t, stats.BlacklistedContacts >= 0)
}

func TestTestPort(t *testing.T) {
	// Use random port to avoid conflicts with existing services
	testPort := liblbrytesting.GetFreePort(t)
	watchdog := New(
		WithTestTimeout(time.Second),
	)
	defer watchdog.Stop()

	ctx := context.Background()

	// Test with invalid port (should fail)
	assert.False(t, watchdog.testPort(ctx, "127.0.0.1", 99999))

	// Test with localhost on a port that's not listening (our random port)
	assert.False(t, watchdog.testPort(ctx, "127.0.0.1", testPort))
}

func TestTestContactPorts(t *testing.T) {
	// Use random ports to avoid conflicts with existing services
	testPort1 := liblbrytesting.GetFreePort(t)
	testPort2 := liblbrytesting.GetFreePort(t)
	watchdog := New(
		WithTestPorts(testPort1, testPort2),
		WithTestTimeout(time.Millisecond*100),
	)
	defer watchdog.Stop()

	contact := createTestContact(t)
	ctx := context.Background()

	// Should fail since no server is running on test ports
	port, success := watchdog.testContactPorts(ctx, contact)
	assert.Equal(t, 0, port)
	assert.False(t, success)

	// Test with contact that has PeerPort set
	contact.PeerPort = 8080
	port, success = watchdog.testContactPorts(ctx, contact)
	assert.Equal(t, 0, port) // Should be 0 since connection will fail
	assert.False(t, success)
}

// createTestContact creates a test contact for testing purposes
func createTestContact(t *testing.T) *dht.Contact {
	ip := net.ParseIP("127.0.0.1")
	require.NotNil(t, ip)

	return &dht.Contact{
		ID:   bits.Bitmap{1, 2, 3},
		IP:   ip,
		Port: 4444,
	}
}

// Test integration with a real TCP server
func TestValidateContact_WithRealServer(t *testing.T) {
	// Start a simple TCP server for testing
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Accept connections in a goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return // Listener closed
			}
			conn.Close() // Immediately close connections
		}
	}()

	watchdog := New(
		WithTestPorts(port),
		WithTestTimeout(time.Second),
	)
	defer watchdog.Stop()

	contact := &dht.Contact{
		ID:   bits.Bitmap{1, 2, 3},
		IP:   net.ParseIP("127.0.0.1"),
		Port: 4444,
	}

	ctx := context.Background()
	result := watchdog.ValidateAndUpdateContact(ctx, contact)

	// Should succeed since we have a server running
	assert.True(t, result)
	assert.True(t, watchdog.IsCached(contact.ID))
	assert.False(t, watchdog.IsBlacklisted(contact.ID))

	// Contact's PeerPort should be updated if it was 0
	if contact.PeerPort == 0 {
		assert.Equal(t, port, contact.PeerPort)
	}
}

func TestValidateAndUpdateContact_MutationBehavior(t *testing.T) {
	// Test that ValidateAndUpdateContact has the correct mutation behavior
	// even when connection fails (no server needed)
	port := liblbrytesting.GetFreePort(t)

	watchdog := New(
		WithTestPorts(port),
		WithTestTimeout(time.Millisecond*100), // Fast timeout for test
	)
	defer watchdog.Stop()

	// Create contact with PeerPort = 0 to test mutation behavior
	contact := &dht.Contact{
		ID:       bits.Bitmap{1, 2, 3},
		IP:       net.ParseIP("127.0.0.1"),
		Port:     port,
		PeerPort: 0, // Explicitly set to 0
	}

	// Verify initial state
	assert.Equal(t, 0, contact.PeerPort)

	ctx := context.Background()
	result := watchdog.ValidateAndUpdateContact(ctx, contact)

	// Should fail due to no server, but mutation behavior is tested
	assert.False(t, result)
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// The key test: PeerPort should remain 0 since no successful connection was made
	assert.Equal(t, 0, contact.PeerPort)
}

func TestValidateContactForHash_UsesValidateAndUpdateContact(t *testing.T) {
	// Test that ValidateContactForHash properly uses ValidateAndUpdateContact
	port := liblbrytesting.GetFreePort(t)

	watchdog := New(
		WithTestPorts(port),
		WithTestTimeout(time.Millisecond*100), // Fast timeout for test
	)
	defer watchdog.Stop()

	contact := &dht.Contact{
		ID:       bits.Bitmap{1, 2, 3},
		IP:       net.ParseIP("127.0.0.1"),
		Port:     port,
		PeerPort: 0, // Explicitly set to 0
	}

	blobHash := bits.Bitmap{9, 8, 7}

	// Verify initial state
	assert.Equal(t, 0, contact.PeerPort)

	// This should use ValidateAndUpdateContact internally
	result := watchdog.ValidateContactForHash(blobHash, *contact)

	// Should fail due to no server, but the method should work
	assert.False(t, result)
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// PeerPort should remain 0 since no successful connection
	assert.Equal(t, 0, contact.PeerPort)
}
