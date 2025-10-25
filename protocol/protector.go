package protocol

import "sync"

// Protector interface for content protection
type Protector interface {
	IsProtected(hash string) bool
	Name() string
}

// BlacklistProtector implements Protector interface using a blacklist
type BlacklistProtector struct {
	blacklist map[string]bool
	mutex     sync.RWMutex
}

// NewBlacklistProtector creates a new blacklist protector
func NewBlacklistProtector() *BlacklistProtector {
	return &BlacklistProtector{
		blacklist: make(map[string]bool),
	}
}

// NewBlacklistProtectorFromHashes creates a blacklist protector from initial hashes
func NewBlacklistProtectorFromHashes(hashes []string) *BlacklistProtector {
	bp := &BlacklistProtector{
		blacklist: make(map[string]bool),
	}
	for _, hash := range hashes {
		bp.blacklist[hash] = true
	}
	return bp
}

// IsProtected checks if a hash is in the blacklist
func (bp *BlacklistProtector) IsProtected(hash string) bool {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()
	return bp.blacklist[hash]
}

// Name returns the protector name
func (bp *BlacklistProtector) Name() string {
	return "blacklist"
}

// AddHash adds a hash to the blacklist
func (bp *BlacklistProtector) AddHash(hash string) {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()
	bp.blacklist[hash] = true
}

// RemoveHash removes a hash from the blacklist
func (bp *BlacklistProtector) RemoveHash(hash string) {
	bp.mutex.Lock()
	defer bp.mutex.Unlock()
	delete(bp.blacklist, hash)
}

// GetBlacklist returns a copy of the current blacklist
func (bp *BlacklistProtector) GetBlacklist() []string {
	bp.mutex.RLock()
	defer bp.mutex.RUnlock()

	hashes := make([]string, 0, len(bp.blacklist))
	for hash := range bp.blacklist {
		hashes = append(hashes, hash)
	}
	return hashes
}
