package transfer

import "context"

// Transfer defines the interface for blob acquisition/transfer
type Transfer interface {
	Get(ctx context.Context, hash string) ([]byte, error)
	Name() string
}

// TransferOption defines a generic interface for configuring transfer implementations
// This allows the server builder to remain agnostic about specific transfer types
// while providing type-safe configuration for each implementation.
type TransferOption interface {
	// Apply applies the option to the given transfer implementation
	// The transfer parameter should be type-asserted to the expected type
	Apply(transfer any) error
}
