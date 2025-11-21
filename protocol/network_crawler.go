package protocol

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// NetworkCrawler handles systematic network exploration to populate the routing table
type NetworkCrawler interface {
	Run() error
	// GetDuration returns the duration of the last crawl operation
	// Returns zero duration if no crawl has been completed yet
	GetDuration() time.Duration
}

// DefaultNetworkCrawler is the default implementation of NetworkCrawler
type DefaultNetworkCrawler struct {
	dhtNode  DHTNode
	ctx      context.Context
	logger   *zap.Logger
	duration time.Duration
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
func (c *DefaultNetworkCrawler) Run() error {
	// Start timer to track crawl duration
	startTime := time.Now()
	defer func() {
		c.duration = time.Since(startTime)
	}()

	// Check if context is cancelled before starting work
	if err := c.ctx.Err(); err != nil {
		c.logger.Info("Network crawler cancelled", zap.Error(err))
		return err
	}

	// Perform systematic network exploration
	// This runs synchronously in the foreground and populates the routing table
	// with discovered contacts from across the network

	// Explore keyspace around the node ID
	nodeID := c.dhtNode.ID()
	contacts, err := c.dhtNode.ExploreKeyspace(nodeID)
	if err != nil {
		// Return error to caller - the caller is responsible for deciding
		// whether to log or suppress startup failures
		c.logger.Error("Failed to explore keyspace during network crawling", zap.Error(err))
		return err
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
			return err
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
		zap.Int("added", addedCount),
		zap.Duration("duration", c.duration))
	return nil
}

// GetDuration returns the duration of the last crawl operation
// Returns zero duration if no crawl has been completed yet
func (c *DefaultNetworkCrawler) GetDuration() time.Duration {
	return c.duration
}
