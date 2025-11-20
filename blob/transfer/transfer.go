package transfer

import "context"

// Transfer defines the interface for blob acquisition/transfer
type Transfer interface {
	Get(ctx context.Context, hash string) ([]byte, error)
	Name() string
}

// TransferOption defines a generic interface for configuring transfer implementations.
// This allows the server builder to remain agnostic about specific transfer types
// while providing type-safe configuration for each implementation.
//
// Contract and Expectations:
//   - Reusability: Implementations MUST be reusable and safe to apply to multiple
//     transfer instances of the same concrete type.
//   - Side Effects: Implementations SHOULD be side-effect free beyond configuring
//     the target transfer instance. Avoid modifying global state.
//   - Multiple Calls: Implementations MUST be safe to call multiple times on the
//     same transfer instance, with subsequent calls being idempotent or having
//     well-defined cumulative behavior.
//   - Type Safety: Implementations MUST perform type assertion on the transfer
//     parameter and return a descriptive error if the type is unsupported.
//   - Error Handling: Implementations SHOULD return errors for invalid configurations
//     or type mismatches, but MUST NOT panic for expected failure conditions.
//
// Usage Pattern:
//
//	Options are typically created once and reused across multiple transfer instances
//	during server construction. The builder pattern accumulates options and applies
//	them to each compatible transfer implementation.
type TransferOption interface {
	// Apply applies the option to the given transfer implementation.
	// The transfer parameter should be type-asserted to the expected type.
	//
	// Returns an error if:
	//   - The transfer parameter is not of the expected concrete type
	//   - The option configuration is invalid
	//   - The option cannot be applied due to transfer state constraints
	//
	// The implementation must be safe for concurrent use if the option instance
	// is shared across goroutines, and must not modify the option's internal state
	// in a way that affects subsequent applications.
	Apply(transfer any) error
}
