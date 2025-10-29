package e2e

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	liblbryerrors "go.lumeweb.com/liblbry/errors"
)

// errorContains checks if an error contains any of the given substrings
func errorContains(err error, indicators []string) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	for _, indicator := range indicators {
		if containsIgnoreCase(errStr, indicator) {
			return true
		}
	}

	return false
}

// containsIgnoreCase checks if a string contains a substring ignoring case
func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// isNetworkError checks if an error is a network-related error
func isNetworkError(err error) bool {
	// Check for context/timeout errors first
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	networkErrorIndicators := []string{
		"i/o timeout",
		"connection refused",
		"connection reset",
		"network is unreachable",
		"no route to host",
		"not connected",
		"no such host",
		"timeout",
		"deadline exceeded",
	}

	// Check for wrapped network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}

	return errorContains(err, networkErrorIndicators)
}

// isValidationError checks if an error is due to invalid input validation
func isValidationError(err error) bool {
	// Check for wrapped validation errors
	if liblbryerrors.Is(err, liblbryerrors.ErrInvalidHashLen) {
		return true
	}

	// Check for server validation errors (when server closes connection due to validation)
	if liblbryerrors.Is(err, liblbryerrors.ErrServerValidation) {
		return true
	}

	validationIndicators := []string{
		"invalid request",
		"malformed",
	}

	return errorContains(err, validationIndicators)
}

// isBlobNotFoundError checks if an error indicates a blob was not found
func isBlobNotFoundError(err error) bool {
	// First check if it's a context/timeout error - those aren't "not found" errors
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}

	notFoundIndicators := []string{
		"not found",
		liblbryerrors.ErrBlobNotFound.Error(),
	}

	// Check if it's a wrapped liblbry not found error
	if liblbryerrors.Is(err, liblbryerrors.ErrBlobNotFound) {
		return true
	}

	return errorContains(err, notFoundIndicators)
}

// assertValidationError checks that an error is a validation error
func assertValidationError(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	require.True(t, isValidationError(err), "Expected validation error, got: %v", err)
}

// assertBlobNotFoundError checks that an error indicates a blob was not found
func assertBlobNotFoundError(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	require.True(t, isBlobNotFoundError(err), "Expected blob not found error, got: %v", err)
}

// assertNetworkError checks that an error is a network-related error
func assertNetworkError(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	require.True(t, isNetworkError(err), "Expected network error, got: %v", err)
}

// assertUploadValidationError checks that an error is a validation error
func assertUploadValidationError(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	require.True(t, isValidationError(err), "Expected validation error, got: %v", err)
}

// assertUploadNetworkError checks that an error is a network-related error
func assertUploadNetworkError(t *testing.T, err error, msg string) {
	t.Helper()
	require.Error(t, err, msg)
	require.True(t, isNetworkError(err), "Expected network error, got: %v", err)
}
