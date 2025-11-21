// Package watchdog provides DHT contact validation with caching and blacklisting functionality.
//
// IMPORTANT: The validation methods in this package may mutate contact parameters by updating
// the PeerPort field when a working port is discovered. This behavior is intentional for DHT
// integration to dynamically update contact information with discovered working ports.
package watchdog

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.uber.org/zap"
)

// DHTWatchdog extends the existing dht.ContactValidator interface with caching and blacklisting functionality
type DHTWatchdog interface {
	dht.ContactValidator

	// IsCached checks if a contact is in the successful validation cache
	IsCached(contactID bits.Bitmap) bool

	// IsBlacklisted checks if a contact is in the blacklist
	IsBlacklisted(contactID bits.Bitmap) bool

	// RemoveFromCache removes a contact from the validation cache
	RemoveFromCache(contactID bits.Bitmap)

	// RemoveFromBlacklist removes a contact from the blacklist
	RemoveFromBlacklist(contactID bits.Bitmap)

	// AddToCache manually adds a contact to the cache with the specified timestamp
	// This bypasses normal validation and should be used carefully (e.g., in tests or migrations)
	AddToCache(contact *dht.Contact, timestamp time.Time)

	// AddToBlacklist manually adds a contact to the blacklist with the specified timestamp
	// This bypasses normal validation and should be used carefully (e.g., in tests or migrations)
	AddToBlacklist(contactID bits.Bitmap, timestamp time.Time)

	// CleanupExpired removes expired entries from cache and blacklist
	CleanupExpired()

	// GetStats returns statistics about the watchdog state
	GetStats() WatchdogStats

	// Name returns the watchdog name
	Name() string

	// Stop stops the watchdog and cleans up resources
	Stop()
}

// WatchdogStats contains statistics about the watchdog state
type WatchdogStats struct {
	CachedContacts      int
	BlacklistedContacts int
	LastCleanupTime     time.Time
}

// cachedContact represents a successfully validated contact with timestamp
type cachedContact struct {
	contact   *dht.Contact
	timestamp time.Time
}

// blacklistedContact represents a failed contact with timestamp
type blacklistedContact struct {
	timestamp time.Time
}

// WatchdogConfig holds configuration for the DHT watchdog
type WatchdogConfig struct {
	// TestPorts are the ports to test when validating contacts
	TestPorts []int

	// CacheTimeout is how long to keep successful validations in cache
	CacheTimeout time.Duration

	// BlacklistTimeout is how long to keep failed contacts in blacklist
	BlacklistTimeout time.Duration

	// CleanupInterval is how often to run cleanup of expired entries
	CleanupInterval time.Duration

	// TestTimeout is the timeout for individual port tests
	TestTimeout time.Duration

	// Logger for watchdog operations
	Logger *zap.Logger
}

// WatchdogOption configures watchdog instances
type WatchdogOption func(*WatchdogConfig)

// DefaultConfig creates a default watchdog configuration
func DefaultConfig() *WatchdogConfig {
	return &WatchdogConfig{
		TestPorts:        []int{3333, 4444, 5567}, // Default LBRY peer ports
		CacheTimeout:     30 * time.Minute,
		BlacklistTimeout: 24 * time.Hour,
		CleanupInterval:  10 * time.Minute,
		TestTimeout:      5 * time.Second,
	}
}

// WithTestPorts sets the ports to test when validating contacts
func WithTestPorts(ports ...int) WatchdogOption {
	return func(c *WatchdogConfig) {
		c.TestPorts = ports
	}
}

// WithCacheTimeout sets the timeout for cached successful validations
func WithCacheTimeout(timeout time.Duration) WatchdogOption {
	return func(c *WatchdogConfig) {
		c.CacheTimeout = timeout
	}
}

// WithBlacklistTimeout sets the timeout for blacklisted contacts
func WithBlacklistTimeout(timeout time.Duration) WatchdogOption {
	return func(c *WatchdogConfig) {
		c.BlacklistTimeout = timeout
	}
}

// WithCleanupInterval sets the interval for cleanup of expired entries
func WithCleanupInterval(interval time.Duration) WatchdogOption {
	return func(c *WatchdogConfig) {
		c.CleanupInterval = interval
	}
}

// WithTestTimeout sets the timeout for individual port tests
func WithTestTimeout(timeout time.Duration) WatchdogOption {
	return func(c *WatchdogConfig) {
		c.TestTimeout = timeout
	}
}

// WithLogger sets the logger for watchdog operations
func WithLogger(logger *zap.Logger) WatchdogOption {
	return func(c *WatchdogConfig) {
		c.Logger = logger
	}
}

// DefaultDHTWatchdog implements DHTWatchdog interface
type DefaultDHTWatchdog struct {
	config      *WatchdogConfig
	cache       map[bits.Bitmap]*cachedContact
	blacklist   map[bits.Bitmap]*blacklistedContact
	mutex       sync.RWMutex
	lastCleanup time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	wg          sync.WaitGroup
}

// New creates a new DHT watchdog with the given options
func New(options ...WatchdogOption) *DefaultDHTWatchdog {
	config := DefaultConfig()
	for _, option := range options {
		option(config)
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &DefaultDHTWatchdog{
		config:      config,
		cache:       make(map[bits.Bitmap]*cachedContact),
		blacklist:   make(map[bits.Bitmap]*blacklistedContact),
		lastCleanup: time.Now(),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start cleanup goroutine
	w.wg.Add(1)
	go w.cleanupRoutine()

	return w
}

// ValidateContactForHash implements dht.ContactValidator interface
// Validates if a contact is suitable for storing/returning for a specific blob hash
func (w *DefaultDHTWatchdog) ValidateContactForHash(_ bits.Bitmap, contact dht.Contact) bool {
	// Use our port testing validation logic
	return w.ValidateAndUpdateContact(w.ctx, &contact)
}

// ValidateAndUpdateContact tests connectivity to a contact and updates its port information.
// This method mutates the contact's PeerPort field if it is 0 and a working port is discovered.
// The mutation is intentional for DHT integration to update contact information with discovered working ports.
func (w *DefaultDHTWatchdog) ValidateAndUpdateContact(ctx context.Context, contact *dht.Contact) bool {
	if contact == nil {
		return false
	}

	contactID := contact.ID

	// Check if already cached as successful
	if w.IsCached(contactID) {
		if w.config.Logger != nil {
			w.config.Logger.Debug("Contact already cached as valid",
				zap.String("contact_id", contactID.HexShort()))
		}
		return true
	}

	// Check if blacklisted
	if w.IsBlacklisted(contactID) {
		if w.config.Logger != nil {
			w.config.Logger.Debug("Contact is blacklisted",
				zap.String("contact_id", contactID.HexShort()))
		}
		return false
	}

	// Test connectivity on configured ports
	successfulPort, success := w.testContactPorts(ctx, contact)

	w.mutex.Lock()
	defer w.mutex.Unlock()

	if success {
		// Cache the successful validation and update contact port
		contactCopy := *contact
		w.cache[contactID] = &cachedContact{
			contact:   &contactCopy,
			timestamp: time.Now(),
		}

		// Update the contact's PeerPort if we found a working port
		if successfulPort > 0 && contact.PeerPort == 0 {
			contact.PeerPort = successfulPort
			if w.config.Logger != nil {
				w.config.Logger.Debug("Updated contact PeerPort",
					zap.String("contact_id", contactID.HexShort()),
					zap.Int("port", successfulPort))
			}
		}

		if w.config.Logger != nil {
			w.config.Logger.Debug("Contact validation succeeded",
				zap.String("contact_id", contactID.HexShort()),
				zap.Int("successful_port", successfulPort))
		}
	} else {
		// Add to blacklist
		w.blacklist[contactID] = &blacklistedContact{
			timestamp: time.Now(),
		}

		if w.config.Logger != nil {
			w.config.Logger.Debug("Contact validation failed, blacklisting",
				zap.String("contact_id", contactID.HexShort()))
		}
	}

	return success
}

// IsCached checks if a contact is in the successful validation cache
func (w *DefaultDHTWatchdog) IsCached(contactID bits.Bitmap) bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	cached, exists := w.cache[contactID]
	if !exists {
		return false
	}

	// Check if entry has expired
	if time.Since(cached.timestamp) > w.config.CacheTimeout {
		return false
	}

	return true
}

// IsBlacklisted checks if a contact is in the blacklist
func (w *DefaultDHTWatchdog) IsBlacklisted(contactID bits.Bitmap) bool {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	blacklisted, exists := w.blacklist[contactID]
	if !exists {
		return false
	}

	// Check if entry has expired
	if time.Since(blacklisted.timestamp) > w.config.BlacklistTimeout {
		return false
	}

	return true
}

// RemoveFromCache removes a contact from the validation cache
func (w *DefaultDHTWatchdog) RemoveFromCache(contactID bits.Bitmap) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	delete(w.cache, contactID)
	if w.config.Logger != nil {
		w.config.Logger.Debug("Removed contact from cache",
			zap.String("contact_id", contactID.HexShort()))
	}
}

// RemoveFromBlacklist removes a contact from the blacklist
func (w *DefaultDHTWatchdog) RemoveFromBlacklist(contactID bits.Bitmap) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	delete(w.blacklist, contactID)
	if w.config.Logger != nil {
		w.config.Logger.Debug("Removed contact from blacklist",
			zap.String("contact_id", contactID.HexShort()))
	}
}

// AddToCache manually adds a contact to the cache with the specified timestamp
// This bypasses normal validation and should be used carefully (e.g., in tests or migrations)
func (w *DefaultDHTWatchdog) AddToCache(contact *dht.Contact, timestamp time.Time) {
	if contact == nil {
		return
	}

	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.cache[contact.ID] = &cachedContact{
		contact:   contact,
		timestamp: timestamp,
	}

	if w.config.Logger != nil {
		w.config.Logger.Debug("Manually added contact to cache",
			zap.String("contact_id", contact.ID.HexShort()))
	}
}

// AddToBlacklist manually adds a contact to the blacklist with the specified timestamp
// This bypasses normal validation and should be used carefully (e.g., in tests or migrations)
func (w *DefaultDHTWatchdog) AddToBlacklist(contactID bits.Bitmap, timestamp time.Time) {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	w.blacklist[contactID] = &blacklistedContact{
		timestamp: timestamp,
	}

	if w.config.Logger != nil {
		w.config.Logger.Debug("Manually added contact to blacklist",
			zap.String("contact_id", contactID.HexShort()))
	}
}

// CleanupExpired removes expired entries from cache and blacklist
func (w *DefaultDHTWatchdog) CleanupExpired() {
	w.mutex.Lock()
	defer w.mutex.Unlock()

	now := time.Now()
	w.lastCleanup = now

	// Clean expired cache entries
	for id, cached := range w.cache {
		if now.Sub(cached.timestamp) > w.config.CacheTimeout {
			delete(w.cache, id)
		}
	}

	// Clean expired blacklist entries
	for id, blacklisted := range w.blacklist {
		if now.Sub(blacklisted.timestamp) > w.config.BlacklistTimeout {
			delete(w.blacklist, id)
		}
	}

	if w.config.Logger != nil {
		w.config.Logger.Debug("Completed cleanup of expired entries",
			zap.Time("cleanup_time", now),
			zap.Int("cache_size", len(w.cache)),
			zap.Int("blacklist_size", len(w.blacklist)))
	}
}

// GetStats returns statistics about the watchdog state
func (w *DefaultDHTWatchdog) GetStats() WatchdogStats {
	w.mutex.RLock()
	defer w.mutex.RUnlock()

	return WatchdogStats{
		CachedContacts:      len(w.cache),
		BlacklistedContacts: len(w.blacklist),
		LastCleanupTime:     w.lastCleanup,
	}
}

// Name returns the watchdog name
func (w *DefaultDHTWatchdog) Name() string {
	return "dht_watchdog"
}

// testContactPorts tests connectivity to a contact on configured ports
func (w *DefaultDHTWatchdog) testContactPorts(ctx context.Context, contact *dht.Contact) (int, bool) {
	// First try the contact's advertised PeerPort if available
	if contact.PeerPort > 0 {
		if w.testPort(ctx, contact.IP.String(), contact.PeerPort) {
			return contact.PeerPort, true
		}
	}

	// Try configured test ports
	for _, port := range w.config.TestPorts {
		if w.testPort(ctx, contact.IP.String(), port) {
			return port, true
		}
	}

	return 0, false
}

// testPort tests connectivity to a specific IP and port
func (w *DefaultDHTWatchdog) testPort(ctx context.Context, ip string, port int) bool {
	address := net.JoinHostPort(ip, strconv.Itoa(port))

	dialer := &net.Dialer{
		Timeout: w.config.TestTimeout,
	}

	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		if w.config.Logger != nil {
			w.config.Logger.Debug("Port test failed",
				zap.String("address", address),
				zap.Error(err))
		}
		return false
	}

	if err = conn.Close(); err != nil {
		if w.config.Logger != nil {
			w.config.Logger.Debug("Failed to close connection after successful port test",
				zap.String("address", address),
				zap.Error(err))
		}
		// Connection was successfully established, so we still return true
		// The close error is logged for debugging purposes
	}
	return true
}

// cleanupRoutine runs periodic cleanup of expired entries
func (w *DefaultDHTWatchdog) cleanupRoutine() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.CleanupExpired()
		case <-w.ctx.Done():
			return
		}
	}
}

// Stop stops the watchdog and waits for the cleanup goroutine to finish
func (w *DefaultDHTWatchdog) Stop() {
	w.cancel()
	w.wg.Wait()
}
