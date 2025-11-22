package protocol

import (
	"net"
)

// Protocol port constants
const (
	// DHT Protocol
	// DefaultDHTPort is the default port for DHT protocol (DefaultDistributedPeerPort)
	DefaultDHTPort = DefaultDistributedPeerPort

	// Distributed Peer Protocol
	// DefaultDistributedPeerPortLegacy is legacy port for distributed peer protocol (DefaultDistributedPeerPortLegacy)
	DefaultDistributedPeerPortLegacy = 3333

	// DefaultDistributedPeerPort is the current port for distributed peer protocol (DefaultDistributedPeerPort)
	// Note: DHT and distributed peer currently share the same port
	DefaultDistributedPeerPort = 4444

	// LBRY Peer Protocol
	// DefaultPeerPort is the default port for LBRY peer protocol (5567)
	// Used for both DHT-discovered peers and manually configured fixed peers
	// Note: Peer ports in DHT can be either DefaultDistributedPeerPort (DefaultDistributedPeerPort) or DefaultPeerPort (5567)
	DefaultPeerPort = 5567

	// Reflector Protocol
	// DefaultReflectorPort is the default port for LBRY reflector protocol (5566)
	DefaultReflectorPort = 5566
)

// ConnectionHandler defines the interface for handling network connections
type ConnectionHandler interface {
	HandleConnection(conn net.Conn)
}
