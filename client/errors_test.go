package client

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lbryTesting "go.lumeweb.com/liblbry/internal/testing"
	"go.lumeweb.com/liblbry/stream"
)

func TestDefaultRetryOptions(t *testing.T) {
	options := DefaultRetryOptions()

	// Verify default options are set
	require.NotEmpty(t, options)
}

func TestWithRetry_Success(t *testing.T) {
	calls := 0
	operation := func() error {
		calls++
		return nil // Success on first call
	}

	err := WithRetry(nil, operation)
	assert.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestWithRetry_RetryableError(t *testing.T) {
	calls := 0
	retryableErr := errors.New("temporary network error")
	operation := func() error {
		calls++
		if calls < 3 {
			return retryableErr
		}
		return nil // Success on third call
	}

	err := WithRetry(nil, operation)
	assert.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestWithRetry_NonRetryableError(t *testing.T) {
	calls := 0
	nonRetryableErr := stream.ErrInvalidSDBlob
	operation := func() error {
		calls++
		return nonRetryableErr
	}

	err := WithRetry(nil, operation)
	assert.Error(t, err)
	assert.Equal(t, 1, calls) // Should not retry
	assert.True(t, errors.Is(err, stream.ErrInvalidSDBlob))
}

func TestWithRetry_MaxAttemptsExhausted(t *testing.T) {
	calls := 0
	retryableErr := errors.New("persistent network error")
	operation := func() error {
		calls++
		return retryableErr
	}

	err := WithRetry(nil, operation)
	assert.Error(t, err)
	assert.Equal(t, 3, calls) // Default max attempts
}

func TestWithRetry_CustomOptions(t *testing.T) {
	calls := 0
	retryableErr := errors.New("temporary error")

	// Custom retry options with 2 attempts and short delay
	customOptions := []retry.Option{
		retry.Attempts(2),
		retry.Delay(10 * time.Millisecond),
		retry.RetryIf(func(err error) bool {
			return !errors.Is(err, stream.ErrInvalidSDBlob)
		}),
		retry.LastErrorOnly(true),
	}

	operation := func() error {
		calls++
		if calls < 5 {
			return retryableErr
		}
		return nil
	}

	start := time.Now()
	err := WithRetry(customOptions, operation)
	duration := time.Since(start)

	assert.Error(t, err) // Should fail after 2 attempts
	assert.Equal(t, 2, calls)
	assert.GreaterOrEqual(t, duration, 10*time.Millisecond) // At least one delay
}

func TestWithRetry_ContextCancellation(t *testing.T) {
	calls := 0
	retryableErr := errors.New("temporary error")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Custom options with context
	customOptions := []retry.Option{
		retry.Attempts(10), // High attempts
		retry.Delay(20 * time.Millisecond),
		retry.Context(ctx),
	}

	operation := func() error {
		calls++
		return retryableErr
	}

	err := WithRetry(customOptions, operation)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled))
}

func TestIsRetryableError(t *testing.T) {
	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"context cancelled", ErrContextCancelled, false},
		{"invalid SD blob", stream.ErrInvalidSDBlob, false},
		{"stream corrupted", ErrStreamCorrupted, false},
		{"invalid hash", ErrInvalidHash, false},
		{"decryption failed", ErrDecryptionFailed, false},
		{"network error", errors.New("network timeout"), true},
		{"temporary error", errors.New("temporary failure"), true},
		{"wrapped retryable error", NewStreamError("test", "sdhash", "blobhash", errors.New("network error"), 1), true},
		{"wrapped non-retryable error", NewStreamError("test", "sdhash", "blobhash", stream.ErrInvalidSDBlob, 1), false}, // Should not retry non-retryable wrapped error
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := isRetryableError(tc.err)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestStreamError(t *testing.T) {
	originalErr := errors.New("original error")
	streamErr := NewStreamError("test_operation", "sd_hash", "blob_hash", originalErr, 2)

	// Test Error method
	expectedMsg := "stream error during test_operation for blob blob_hash in stream sd_hash: original error"
	assert.Equal(t, expectedMsg, streamErr.Error())

	// Test Unwrap method
	assert.Equal(t, originalErr, streamErr.Unwrap())

	// Test IsStreamError
	assert.True(t, IsStreamError(streamErr))
	assert.False(t, IsStreamError(originalErr))

	// Test GetStreamError
	extracted, ok := GetStreamError(streamErr)
	assert.True(t, ok)
	assert.Equal(t, streamErr, extracted)

	extracted, ok = GetStreamError(originalErr)
	assert.False(t, ok)
	assert.Nil(t, extracted)
}

func TestValidateBlobHash(t *testing.T) {
	testCases := []struct {
		name     string
		hash     string
		expected error
	}{
		{"valid hash", lbryTesting.ValidLBRYHashes[lbryTesting.ValidHashKeyBlob], nil},
		{"invalid hash", "invalid_hash", ErrInvalidHash},
		{"empty hash", "", ErrInvalidHash},
		{"too short", "abc123", ErrInvalidHash},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateBlobHash(tc.hash)
			if tc.expected == nil {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
				assert.True(t, errors.Is(err, tc.expected))
			}
		})
	}
}
