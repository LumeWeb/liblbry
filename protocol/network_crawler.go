package protocol

import (
	"context"
)

// NetworkCrawler handles systematic network exploration to populate the routing table
type NetworkCrawler interface {
	Run()
}

// DefaultNetworkCrawler is the default implementation of NetworkCrawler
type DefaultNetworkCrawler struct {
	dht DHT
	ctx context.Context
}

// NewNetworkCrawler creates a new NetworkCrawler instance
func NewNetworkCrawler(dht DHT, ctx context.Context) NetworkCrawler {
	return &DefaultNetworkCrawler{
		dht: dht,
		ctx: ctx,
	}
}

// Run executes the network exploration
func (c *DefaultNetworkCrawler) Run() {
	// Perform systematic network exploration
	// This runs in the background and populates the routing table
	// with discovered contacts from across the network

	// Explore keyspace around the node ID
	nodeID := c.dht.ID()
	_, err := c.dht.ExploreKeyspace(nodeID)
	if err != nil {
		// Log error but don't fail - the DHT will still function
		// with basic routing table population
		return
	}
}
