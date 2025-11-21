package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.lumeweb.com/lbry-dht"
	"go.lumeweb.com/lbry-dht/bits"
	"go.lumeweb.com/liblbry/protocol/mocks"
)

func TestNewNetworkCrawler(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx := context.Background()

	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	assert.NotNil(t, crawler)
	// Verify it's not nil and implements the interface
	assert.Implements(t, (*NetworkCrawler)(nil), crawler)
}

func TestNetworkCrawler_Run_Success(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx := context.Background()

	// Create a test node ID
	nodeID := bits.Bitmap{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Mock the DHT methods
	mockDHTNode.EXPECT().ID().Return(nodeID)
	mockDHTNode.EXPECT().ExploreKeyspace(nodeID).Return([]dht.Contact{}, nil)

	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	// Run should not panic
	crawler.Run()
}

func TestNetworkCrawler_Run_Error(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx := context.Background()

	// Create a test node ID
	nodeID := bits.Bitmap{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Mock the DHT methods to return an error
	mockDHTNode.EXPECT().ID().Return(nodeID)
	mockDHTNode.EXPECT().ExploreKeyspace(nodeID).Return([]dht.Contact{}, assert.AnError)

	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	// Run should not panic even with error
	crawler.Run()
}

func TestNetworkCrawler_Run_ContextCancelled(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context immediately
	cancel()

	// No expectations needed since Run() should return early due to cancelled context
	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	// Run should return early without calling DHT methods when context is cancelled
	crawler.Run()
}

func TestNetworkCrawler_Run_WithContacts_Success(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx := context.Background()

	// Create a test node ID
	nodeID := bits.Bitmap{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Create test contacts
	contact1 := dht.Contact{ID: bits.Bitmap{1, 1, 1, 1}}
	contact2 := dht.Contact{ID: bits.Bitmap{2, 2, 2, 2}}
	contact3 := dht.Contact{ID: bits.Bitmap{3, 3, 3, 3}}
	contacts := []dht.Contact{contact1, contact2, contact3}

	// Mock the DHT methods
	mockDHTNode.EXPECT().ID().Return(nodeID)
	mockDHTNode.EXPECT().ExploreKeyspace(nodeID).Return(contacts, nil)

	// Expect AddContact to be called for each contact
	mockDHTNode.EXPECT().AddContact(contact1).Return(nil)
	mockDHTNode.EXPECT().AddContact(contact2).Return(nil)
	mockDHTNode.EXPECT().AddContact(contact3).Return(nil)

	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	// Run should process all contacts successfully
	crawler.Run()
}

func TestNetworkCrawler_Run_WithContacts_PartialFailures(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx := context.Background()

	// Create a test node ID
	nodeID := bits.Bitmap{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Create test contacts
	contact1 := dht.Contact{ID: bits.Bitmap{1, 1, 1, 1}}
	contact2 := dht.Contact{ID: bits.Bitmap{2, 2, 2, 2}}
	contact3 := dht.Contact{ID: bits.Bitmap{3, 3, 3, 3}}
	contacts := []dht.Contact{contact1, contact2, contact3}

	// Mock the DHT methods
	mockDHTNode.EXPECT().ID().Return(nodeID)
	mockDHTNode.EXPECT().ExploreKeyspace(nodeID).Return(contacts, nil)

	// Expect AddContact to be called for each contact, with the second one failing
	mockDHTNode.EXPECT().AddContact(contact1).Return(nil)
	mockDHTNode.EXPECT().AddContact(contact2).Return(assert.AnError)
	mockDHTNode.EXPECT().AddContact(contact3).Return(nil)

	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	// Run should continue processing even when one contact fails
	crawler.Run()
}

func TestNetworkCrawler_Run_WithContacts_AllFailures(t *testing.T) {
	mockDHTNode := mocks.NewMockDHTNode(t)
	ctx := context.Background()

	// Create a test node ID
	nodeID := bits.Bitmap{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}

	// Create test contacts
	contact1 := dht.Contact{ID: bits.Bitmap{1, 1, 1, 1}}
	contact2 := dht.Contact{ID: bits.Bitmap{2, 2, 2, 2}}
	contacts := []dht.Contact{contact1, contact2}

	// Mock the DHT methods
	mockDHTNode.EXPECT().ID().Return(nodeID)
	mockDHTNode.EXPECT().ExploreKeyspace(nodeID).Return(contacts, nil)

	// Expect AddContact to be called for each contact, all failing
	mockDHTNode.EXPECT().AddContact(contact1).Return(assert.AnError)
	mockDHTNode.EXPECT().AddContact(contact2).Return(assert.AnError)

	crawler := NewNetworkCrawler(mockDHTNode, ctx, nil)

	// Run should attempt to add all contacts even when they all fail
	crawler.Run()
}
