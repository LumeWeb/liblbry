package liblbry

// ReflectorClient defines the interface for communicating with LBRY reflector servers
type ReflectorClient interface {
	Connect(address string) error
	Get(hash string) ([]byte, error)
	SendBlob(data []byte) error
	SendSDBlob(data []byte) error
	Close() error
}
