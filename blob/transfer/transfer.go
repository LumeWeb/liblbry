package transfer

// Transfer defines the interface for blob acquisition/transfer
type Transfer interface {
	Get(hash string) ([]byte, error)
	Name() string
}
