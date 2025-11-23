package protocol

import (
	"net"
)

// Protocol port constants
const (
	// DHT Protocol
	// DefaultDHTPort is the default port for DHT protocol (4444)
	// These are default values only - actual ports may be configured differently
	DefaultDHTPort = DefaultDistributedPeerPort

	// Distributed Peer Protocol
	// DefaultDistributedPeerPortLegacy is the legacy port for distributed peer protocol (3333)
	DefaultDistributedPeerPortLegacy = 3333

	// DefaultDistributedPeerPort is the current default port for distributed peer protocol (4444)
	// Note: DHT and distributed peer currently share the same port
	DefaultDistributedPeerPort = 4444

	// LBRY Peer Protocol
	// DefaultPeerPort is the default port for LBRY peer protocol (5567)
	// Used for both DHT-discovered peers and manually configured fixed peers
	// Note: Peer ports in DHT can be either DefaultDistributedPeerPort (4444) or DefaultPeerPort (5567)
	DefaultPeerPort = 5567

	// Reflector Protocol
	// DefaultReflectorPort is the default port for LBRY reflector protocol (5566)
	DefaultReflectorPort = 5566
)

// ConnectionHandler defines the interface for handling network connections
type ConnectionHandler interface {
	HandleConnection(conn net.Conn)
}
