package protocol

import (
	"context"
	"net"
	"net/netip"
)

// Context key types to avoid collisions
type contextKey string

const (
	// SourceContextKey is the context key for the request source (peer or reflector)
	SourceContextKey contextKey = "source"

	// IPAddressContextKey is the context key for the client IP address
	IPAddressContextKey contextKey = "ip_address"
)

// Source represents the source of a request
type Source string

const (
	// SourcePeer indicates the request came from a peer
	SourcePeer Source = "peer"

	// SourceReflector indicates the request came from a reflector
	SourceReflector Source = "reflector"
)

// GetConnectionIP extracts the IP address from a net.Conn
func GetConnectionIP(conn net.Conn) string {
	addr := conn.RemoteAddr().String()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		// If we can't split the address, try to parse it as an IP directly
		if ip, err := netip.ParseAddr(addr); err == nil {
			return ip.String()
		}
		return ""
	}

	// Validate that the host is a proper IP address
	if ip, err := netip.ParseAddr(host); err == nil {
		return ip.String()
	}

	return ""
}

// GetSourceFromContext safely extracts the Source from a context
func GetSourceFromContext(ctx context.Context) (Source, bool) {
	value := ctx.Value(SourceContextKey)
	if value == nil {
		return "", false
	}

	source, ok := value.(Source)
	return source, ok
}

// GetIPAddressFromContext safely extracts the IP address from a context
func GetIPAddressFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(IPAddressContextKey)
	if value == nil {
		return "", false
	}

	ip, ok := value.(string)
	return ip, ok
}

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
