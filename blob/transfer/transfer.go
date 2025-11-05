package transfer

import "context"

// Transfer defines the interface for blob acquisition/transfer
type Transfer interface {
	Get(ctx context.Context, hash string) ([]byte, error)
	Name() string
}
