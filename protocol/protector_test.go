package protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewBlacklistProtector(t *testing.T) {
	protector := NewBlacklistProtector()
	assert.NotNil(t, protector)
	assert.Empty(t, protector.GetBlacklist())
}

func TestNewBlacklistProtectorFromHashes(t *testing.T) {
	hashes := []string{"hash1", "hash2", "hash3"}
	protector := NewBlacklistProtectorFromHashes(hashes)
	
	assert.NotNil(t, protector)
	blacklist := protector.GetBlacklist()
	assert.Len(t, blacklist, 3)
	
	// Check that all hashes are in the blacklist
	for _, hash := range hashes {
		assert.True(t, protector.IsProtected(hash))
	}
}

func TestBlacklistProtector_IsProtected(t *testing.T) {
	hashes := []string{"protected_hash1", "protected_hash2"}
	protector := NewBlacklistProtectorFromHashes(hashes)
	
	// Test protected hashes
	assert.True(t, protector.IsProtected("protected_hash1"))
	assert.True(t, protector.IsProtected("protected_hash2"))
	
	// Test unprotected hash
	assert.False(t, protector.IsProtected("unprotected_hash"))
	
	// Test with empty protector
	emptyProtector := NewBlacklistProtector()
	assert.False(t, emptyProtector.IsProtected("any_hash"))
}

func TestBlacklistProtector_Name(t *testing.T) {
	protector := NewBlacklistProtector()
	assert.Equal(t, "blacklist", protector.Name())
}

func TestBlacklistProtector_AddHash(t *testing.T) {
	protector := NewBlacklistProtector()
	
	// Initially empty
	assert.Empty(t, protector.GetBlacklist())
	
	// Add a hash
	protector.AddHash("new_hash")
	
	// Verify it's protected now
	assert.True(t, protector.IsProtected("new_hash"))
	
	// Verify blacklist contains the hash
	blacklist := protector.GetBlacklist()
	assert.Len(t, blacklist, 1)
	assert.Contains(t, blacklist, "new_hash")
}

func TestBlacklistProtector_RemoveHash(t *testing.T) {
	hashes := []string{"hash1", "hash2", "hash3"}
	protector := NewBlacklistProtectorFromHashes(hashes)
	
	// Verify initial state
	assert.True(t, protector.IsProtected("hash2"))
	
	// Remove a hash
	protector.RemoveHash("hash2")
	
	// Verify it's no longer protected
	assert.False(t, protector.IsProtected("hash2"))
	
	// Verify blacklist no longer contains the hash
	blacklist := protector.GetBlacklist()
	assert.Len(t, blacklist, 2)
	assert.NotContains(t, blacklist, "hash2")
	assert.Contains(t, blacklist, "hash1")
	assert.Contains(t, blacklist, "hash3")
}

func TestBlacklistProtector_GetBlacklist(t *testing.T) {
	hashes := []string{"hash1", "hash2", "hash3"}
	protector := NewBlacklistProtectorFromHashes(hashes)
	
	blacklist := protector.GetBlacklist()
	assert.Len(t, blacklist, 3)
	
	// Verify all hashes are present
	for _, hash := range hashes {
		assert.Contains(t, blacklist, hash)
	}
	
	// Verify it's a copy (modifying it doesn't affect the protector)
	blacklist = append(blacklist, "additional_hash")
	newBlacklist := protector.GetBlacklist()
	assert.Len(t, newBlacklist, 3) // Should still be 3
}
