package liblbry

// AccessControl defines the interface for controlling access to blobs based on peer identity
type AccessControl interface {
	Allow(hash string, peerIP string) bool
}
