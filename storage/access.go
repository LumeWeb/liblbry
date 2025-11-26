package storage

import "context"

// AccessControl defines the interface for controlling access to blobs based on peer identity
type AccessControl interface {
	Allow(ctx context.Context, hash string, peerIP string) bool
}

// AllowAllAccess is an access control that allows all requests
type AllowAllAccess struct{}

func (_ *AllowAllAccess) Allow(_ context.Context, _ string, _ string) bool {
	return true
}

// DenyAllAccess is an access control that denies all requests
type DenyAllAccess struct{}

func (_ *DenyAllAccess) Allow(_ context.Context, _ string, _ string) bool {
	return false
}

// NewAllowAllAccess creates a new AllowAllAccess instance
func NewAllowAllAccess() *AllowAllAccess {
	return &AllowAllAccess{}
}

// NewDenyAllAccess creates a new DenyAllAccess instance
func NewDenyAllAccess() *DenyAllAccess {
	return &DenyAllAccess{}
}
