package protocol

import (
	"net"
)

// ConnectionHandler defines the interface for handling network connections
type ConnectionHandler interface {
	HandleConnection(conn net.Conn)
}
