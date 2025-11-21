package protocol

import (
	"context"

	"go.uber.org/zap"
)

// NetworkCrawler handles systematic network exploration to populate the routing table
type NetworkCrawler interface {
	Run()
}

// DefaultNetworkCrawler is the default implementation of NetworkCrawler
type DefaultNetworkCrawler struct {
	dhtNode DHTNode
	ctx     context.Context
	logger  *zap.Logger
}

// NewNetworkCrawler creates a new NetworkCrawler instance
func NewNetworkCrawler(dhtNode DHTNode, ctx context.Context, logger *zap.Logger) NetworkCrawler {
	if logger == nil {
		logger = zap.NewNop()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return &DefaultNetworkCrawler{
		dhtNode: dhtNode,
		ctx:     ctx,
		logger:  logger,
	}
}

// Run executes the network exploration
func (c *DefaultNetworkCrawler) Run() {
	// Check if context is cancelled before starting work
	if err := c.ctx.Err(); err != nil {
		c.logger.Info("Network crawler cancelled", zap.Error(err))
		return
	}

	// Perform systematic network exploration
	// This runs in the background and populates the routing table
	// with discovered contacts from across the network

	// Explore keyspace around the node ID
	nodeID := c.dhtNode.ID()
	contacts, err := c.dhtNode.ExploreKeyspace(nodeID)
	if err != nil {
		// Log error but don't fail - the DHT will still function
		// with basic routing table population
		c.logger.Error("Failed to explore keyspace during network crawling", zap.Error(err))
		return
	}

	c.logger.Info("Network crawler discovered contacts", zap.Int("count", len(contacts)))

	// Add all discovered contacts to the routing table
	addedCount := 0
	for _, contact := range contacts {
		// Check context before processing each contact
		if err := c.ctx.Err(); err != nil {
			c.logger.Info("Network crawler cancelled during contact processing",
				zap.Int("processed", addedCount),
				zap.Int("remaining", len(contacts)-addedCount),
				zap.Error(err))
			return
		}

		err := c.dhtNode.AddContact(contact)
		if err != nil {
			// Continue adding other contacts even if one fails
			c.logger.Warn("Failed to add contact to routing table",
				zap.String("contact_id", contact.ID.HexShort()),
				zap.Error(err))
			continue
		}
		addedCount++
	}

	c.logger.Info("Network crawler completed",
		zap.Int("discovered", len(contacts)),
		zap.Int("added", addedCount))
}
