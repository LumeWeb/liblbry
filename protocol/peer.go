package protocol

import "net"

// PeerServer defines the interface for handling peer-to-peer blob requests
type PeerServer interface {
	HandleConnection(conn net.Conn)
}
