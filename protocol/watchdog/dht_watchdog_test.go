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

	contact := createTestContact(t)
	ctx := context.Background()

	// First validation should succeed (we'll mock the port test)
	// Since we can't actually connect, this will fail and blacklist
	result := watchdog.ValidateContact(ctx, contact)
	assert.False(t, result) // Should fail due to no actual server

	// Contact should now be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))
	assert.False(t, watchdog.IsCached(contact.ID))

	// Remove from blacklist and manually add to cache for testing
	watchdog.RemoveFromBlacklist(contact.ID)

	// Manually add to cache to test cached path
	watchdog.cache[contact.ID] = &cachedContact{
		contact:   contact,
		timestamp: time.Now(),
	}

	// Now validation should return true due to cache
	result = watchdog.ValidateContact(ctx, contact)
	assert.True(t, result)
}

func TestValidateContact_Blacklisted(t *testing.T) {
	// Use random port to avoid conflicts with existing services
	testPort := liblbrytesting.GetFreePort(t)
	watchdog := New(
		WithTestPorts(testPort),
		WithTestTimeout(time.Second),
	)

	contact := createTestContact(t)
	ctx := context.Background()

	// First validation should fail and blacklist
	result := watchdog.ValidateContact(ctx, contact)
	assert.False(t, result)

	// Contact should be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// Second validation should return false due to blacklist
	result = watchdog.ValidateContact(ctx, contact)
	assert.False(t, result)
}

func TestIsCached(t *testing.T) {
	watchdog := New(
		WithCacheTimeout(time.Minute),
	)

	contact := createTestContact(t)

	// Initially not cached
	assert.False(t, watchdog.IsCached(contact.ID))

	// Add to cache
	watchdog.cache[contact.ID] = &cachedContact{
		contact:   contact,
		timestamp: time.Now(),
	}

	// Should be cached
	assert.True(t, watchdog.IsCached(contact.ID))

	// Test expiration
	watchdog.cache[contact.ID].timestamp = time.Now().Add(-2 * time.Minute)
	assert.False(t, watchdog.IsCached(contact.ID))
}

func TestIsBlacklisted(t *testing.T) {
	watchdog := New(
		WithBlacklistTimeout(time.Minute),
	)

	contact := createTestContact(t)

	// Initially not blacklisted
	assert.False(t, watchdog.IsBlacklisted(contact.ID))

	// Add to blacklist
	watchdog.blacklist[contact.ID] = &blacklistedContact{
		timestamp: time.Now(),
	}

	// Should be blacklisted
	assert.True(t, watchdog.IsBlacklisted(contact.ID))

	// Test expiration
	watchdog.blacklist[contact.ID].timestamp = time.Now().Add(-2 * time.Minute)
	assert.False(t, watchdog.IsBlacklisted(contact.ID))
}

func TestRemoveFromCache(t *testing.T) {
	watchdog := New()

	contact := createTestContact(t)

	// Add to cache
	watchdog.cache[contact.ID] = &cachedContact{
		contact:   contact,
		timestamp: time.Now(),
	}

	assert.True(t, watchdog.IsCached(contact.ID))

	// Remove from cache
	watchdog.RemoveFromCache(contact.ID)
	assert.False(t, watchdog.IsCached(contact.ID))
}

func TestRemoveFromBlacklist(t *testing.T) {
	watchdog := New()

	contact := createTestContact(t)

	// Add to blacklist
	watchdog.blacklist[contact.ID] = &blacklistedContact{
		timestamp: time.Now(),
	}

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

	contact1 := createTestContact(t)
	contact2 := createTestContact(t)
	contact2.ID = bits.Bitmap{9, 8, 7} // Different ID

	// Add expired cache entry
	watchdog.cache[contact1.ID] = &cachedContact{
		contact:   contact1,
		timestamp: time.Now().Add(-2 * time.Minute),
	}

	// Add valid cache entry
	watchdog.cache[contact2.ID] = &cachedContact{
		contact:   contact2,
		timestamp: time.Now(),
	}

	// Add expired blacklist entry
	watchdog.blacklist[contact1.ID] = &blacklistedContact{
		timestamp: time.Now().Add(-2 * time.Minute),
	}

	// Add valid blacklist entry
	watchdog.blacklist[contact2.ID] = &blacklistedContact{
		timestamp: time.Now(),
	}

	// Before cleanup
	assert.Equal(t, 2, len(watchdog.cache))
	assert.Equal(t, 2, len(watchdog.blacklist))

	// Run cleanup
	watchdog.CleanupExpired()

	// After cleanup - only valid entries should remain
	assert.Equal(t, 1, len(watchdog.cache))
	assert.Equal(t, 1, len(watchdog.blacklist))
	assert.True(t, watchdog.IsCached(contact2.ID))
	assert.True(t, watchdog.IsBlacklisted(contact2.ID))
	assert.False(t, watchdog.IsCached(contact1.ID))
	assert.False(t, watchdog.IsBlacklisted(contact1.ID))
}

func TestGetStats(t *testing.T) {
	watchdog := New()

	contact := createTestContact(t)

	// Add some entries
	watchdog.cache[contact.ID] = &cachedContact{
		contact:   contact,
		timestamp: time.Now(),
	}

	watchdog.blacklist[contact.ID] = &blacklistedContact{
		timestamp: time.Now(),
	}

	stats := watchdog.GetStats()
	assert.Equal(t, 1, stats.CachedContacts)
	assert.Equal(t, 1, stats.BlacklistedContacts)
	assert.False(t, stats.LastCleanupTime.IsZero())
}

func TestConcurrentAccess(t *testing.T) {
	watchdog := New(
		WithTestTimeout(time.Millisecond * 100),
	)

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
				watchdog.ValidateContact(ctx, contact)
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

	contact := &dht.Contact{
		ID:   bits.Bitmap{1, 2, 3},
		IP:   net.ParseIP("127.0.0.1"),
		Port: 4444,
	}

	ctx := context.Background()
	result := watchdog.ValidateContact(ctx, contact)

	// Should succeed since we have a server running
	assert.True(t, result)
	assert.True(t, watchdog.IsCached(contact.ID))
	assert.False(t, watchdog.IsBlacklisted(contact.ID))

	// Contact's PeerPort should be updated if it was 0
	if contact.PeerPort == 0 {
		assert.Equal(t, port, contact.PeerPort)
	}
}
