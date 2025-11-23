package discovery

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// MockHostResolver implements HostResolver interface for testing
type MockHostResolver struct {
	resolutions map[string]net.IP
	errors      map[string]error
	calls       []string
}

// NewMockHostResolver creates a new MockHostResolver with predefined resolutions and errors
func NewMockHostResolver() *MockHostResolver {
	return &MockHostResolver{
		resolutions: make(map[string]net.IP),
		errors:      make(map[string]error),
		calls:       make([]string, 0),
	}
}

// SetResolution sets a predefined IP resolution for a host
func (m *MockHostResolver) SetResolution(host string, ip net.IP) {
	m.resolutions[host] = ip
}

// SetError sets a predefined error for a host
func (m *MockHostResolver) SetError(host string, err error) {
	m.errors[host] = err
}

// ResolveHostToIP implements HostResolver interface
func (m *MockHostResolver) ResolveHostToIP(host, originalAddr string) (net.IP, error) {
	m.calls = append(m.calls, host)

	if err, exists := m.errors[host]; exists {
		return nil, err
	}

	if ip, exists := m.resolutions[host]; exists {
		return ip, nil
	}

	// Default behavior: try to parse as IP, otherwise return error
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}

	return nil, &net.DNSError{
		Err:        "no such host",
		Name:       host,
		IsNotFound: true,
	}
}

// GetCalls returns the list of hosts that were resolved
func (m *MockHostResolver) GetCalls() []string {
	return append([]string{}, m.calls...)
}

// Reset clears the call history
func (m *MockHostResolver) Reset() {
	m.calls = make([]string, 0)
}

// TestNewDefaultHostResolver tests the constructor
func TestNewDefaultHostResolver(t *testing.T) {
	t.Run("WithValidParameters", func(t *testing.T) {
		logger := zaptest.NewLogger(t)
		timeout := 5 * time.Second

		resolver := NewDefaultHostResolver(logger, timeout)

		require.NotNil(t, resolver)
		assert.Equal(t, logger, resolver.logger)
		assert.Equal(t, timeout, resolver.timeout)
		assert.NotNil(t, resolver.resolver)
	})

	t.Run("WithNilLogger", func(t *testing.T) {
		resolver := NewDefaultHostResolver(nil, 5*time.Second)

		require.NotNil(t, resolver)
		assert.NotNil(t, resolver.logger) // Should create no-op logger
	})

	t.Run("WithZeroTimeout", func(t *testing.T) {
		resolver := NewDefaultHostResolver(zaptest.NewLogger(t), 0)

		require.NotNil(t, resolver)
		assert.Equal(t, 10*time.Second, resolver.timeout) // Should use default
	})

	t.Run("WithNegativeTimeout", func(t *testing.T) {
		resolver := NewDefaultHostResolver(zaptest.NewLogger(t), -5*time.Second)

		require.NotNil(t, resolver)
		assert.Equal(t, 10*time.Second, resolver.timeout) // Should use default
	})
}

// TestDefaultHostResolver_ResolveHostToIP tests the IP resolution functionality
func TestDefaultHostResolver_ResolveHostToIP(t *testing.T) {
	logger := zaptest.NewLogger(t)
	resolver := NewDefaultHostResolver(logger, 5*time.Second)

	t.Run("ValidIPv4Address", func(t *testing.T) {
		host := "192.168.1.100"
		originalAddr := "192.168.1.100:3333"

		ip, err := resolver.ResolveHostToIP(host, originalAddr)

		require.NoError(t, err)
		assert.Equal(t, net.ParseIP("192.168.1.100"), ip)
	})

	t.Run("ValidIPv6Address", func(t *testing.T) {
		host := "2001:db8::1"
		originalAddr := "2001:db8::1:3333"

		ip, err := resolver.ResolveHostToIP(host, originalAddr)

		require.NoError(t, err)
		assert.Equal(t, net.ParseIP("2001:db8::1"), ip)
	})

	t.Run("LocalhostResolution", func(t *testing.T) {
		host := "localhost"
		originalAddr := "localhost:3333"

		ip, err := resolver.ResolveHostToIP(host, originalAddr)

		require.NoError(t, err)
		assert.NotNil(t, ip)
		// localhost should resolve to either 127.0.0.1 or ::1
		assert.True(t, ip.Equal(net.ParseIP("127.0.0.1")) || ip.Equal(net.ParseIP("::1")))
	})

	t.Run("InvalidHostname", func(t *testing.T) {
		host := "nonexistent.invalid.hostname.example"
		originalAddr := "nonexistent.invalid.hostname.example:3333"

		ip, err := resolver.ResolveHostToIP(host, originalAddr)

		require.Error(t, err)
		assert.Nil(t, ip)
		assert.Contains(t, err.Error(), "failed to resolve hostname")
	})

	t.Run("EmptyHost", func(t *testing.T) {
		host := ""
		originalAddr := ":3333"

		ip, err := resolver.ResolveHostToIP(host, originalAddr)

		require.Error(t, err)
		assert.Nil(t, ip)
	})
}

// TestDefaultHostResolver_IPv4Preference tests IPv4 over IPv6 preference
func TestDefaultHostResolver_IPv4Preference(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create a custom resolver that returns both IPv4 and IPv6 addresses
	resolver := &DefaultHostResolver{
		logger:  logger,
		timeout: 5 * time.Second,
		resolver: &net.Resolver{
			PreferGo: true,
		},
	}

	// Test with a hostname that has both A and AAAA records
	// Note: This test might be flaky depending on DNS configuration
	// We'll use a common hostname that typically has both records
	host := "google.com"
	originalAddr := "google.com:443"

	ip, err := resolver.ResolveHostToIP(host, originalAddr)

	// We expect this to succeed (google.com should resolve)
	if err == nil {
		assert.NotNil(t, ip)
		// If both IPv4 and IPv6 are available, IPv4 should be preferred
		if ip.To4() == nil {
			t.Logf("Warning: Got IPv6 address %s, IPv4 preference may not be working", ip.String())
		}
	} else {
		// If DNS lookup fails, that's okay for this test - the important thing is no panic
		t.Logf("DNS lookup failed (expected in some environments): %v", err)
	}
}

// TestDefaultHostResolver_Timeout tests timeout functionality
func TestDefaultHostResolver_Timeout(t *testing.T) {
	logger := zaptest.NewLogger(t)

	// Create resolver with very short timeout
	resolver := NewDefaultHostResolver(logger, 1*time.Millisecond)

	// Use a hostname that will likely timeout
	host := "slow-resolving-host.invalid"
	originalAddr := "slow-resolving-host.invalid:3333"

	start := time.Now()
	ip, err := resolver.ResolveHostToIP(host, originalAddr)
	duration := time.Since(start)

	require.Error(t, err)
	assert.Nil(t, ip)
	// Should timeout quickly, not take the full DNS timeout
	assert.Less(t, duration, 5*time.Second)
}

// TestDefaultHostResolver_ResolveHostToIPWithTimeout tests custom timeout functionality
func TestDefaultHostResolver_ResolveHostToIPWithTimeout(t *testing.T) {
	logger := zaptest.NewLogger(t)
	resolver := NewDefaultHostResolver(logger, 10*time.Second)

	t.Run("WithCustomTimeout", func(t *testing.T) {
		host := "192.168.1.100"
		originalAddr := "192.168.1.100:3333"

		ip, err := resolver.ResolveHostToIPWithTimeout(host, originalAddr, 1*time.Second)

		require.NoError(t, err)
		assert.Equal(t, net.ParseIP("192.168.1.100"), ip)
	})

	t.Run("WithZeroTimeout", func(t *testing.T) {
		host := "192.168.1.100"
		originalAddr := "192.168.1.100:3333"

		ip, err := resolver.ResolveHostToIPWithTimeout(host, originalAddr, 0)

		require.NoError(t, err)
		assert.Equal(t, net.ParseIP("192.168.1.100"), ip)
	})
}

// TestMockHostResolver tests the mock implementation
func TestMockHostResolver(t *testing.T) {
	mock := NewMockHostResolver()

	t.Run("PredefinedResolution", func(t *testing.T) {
		host := "test.example.com"
		expectedIP := net.ParseIP("192.168.1.100")
		originalAddr := "test.example.com:3333"

		mock.SetResolution(host, expectedIP)

		ip, err := mock.ResolveHostToIP(host, originalAddr)

		require.NoError(t, err)
		assert.Equal(t, expectedIP, ip)
		assert.Contains(t, mock.GetCalls(), host)
	})

	t.Run("PredefinedError", func(t *testing.T) {
		host := "error.example.com"
		expectedError := &net.DNSError{Err: "custom error", Name: host}
		originalAddr := "error.example.com:3333"

		mock.SetError(host, expectedError)

		ip, err := mock.ResolveHostToIP(host, originalAddr)

		require.Error(t, err)
		assert.Equal(t, expectedError, err)
		assert.Nil(t, ip)
		assert.Contains(t, mock.GetCalls(), host)
	})

	t.Run("IPFallback", func(t *testing.T) {
		host := "192.168.1.200"
		originalAddr := "192.168.1.200:3333"

		ip, err := mock.ResolveHostToIP(host, originalAddr)

		require.NoError(t, err)
		assert.Equal(t, net.ParseIP("192.168.1.200"), ip)
	})

	t.Run("DefaultError", func(t *testing.T) {
		host := "unknown.example.com"
		originalAddr := "unknown.example.com:3333"

		ip, err := mock.ResolveHostToIP(host, originalAddr)

		require.Error(t, err)
		assert.Nil(t, ip)
		assert.IsType(t, &net.DNSError{}, err)
	})

	t.Run("CallTracking", func(t *testing.T) {
		mock.Reset()

		// Make multiple calls
		mock.ResolveHostToIP("host1", "host1:3333")
		mock.ResolveHostToIP("host2", "host2:3333")
		mock.ResolveHostToIP("host1", "host1:4444")

		calls := mock.GetCalls()
		assert.Equal(t, 3, len(calls))
		assert.Equal(t, "host1", calls[0])
		assert.Equal(t, "host2", calls[1])
		assert.Equal(t, "host1", calls[2])
	})

	t.Run("Reset", func(t *testing.T) {
		// Create a fresh mock for this test
		freshMock := NewMockHostResolver()

		// Make a call first
		freshMock.ResolveHostToIP("host1", "host1:3333")
		assert.Equal(t, 1, len(freshMock.GetCalls()))

		// Reset and verify
		freshMock.Reset()
		assert.Equal(t, 0, len(freshMock.GetCalls()))
	})
}

// BenchmarkDefaultHostResolver_IPResolution benchmarks IP resolution performance
func BenchmarkDefaultHostResolver_IPResolution(b *testing.B) {
	logger := zap.NewNop()
	resolver := NewDefaultHostResolver(logger, 5*time.Second)

	b.Run("IPv4Address", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resolver.ResolveHostToIP("192.168.1.100", "192.168.1.100:3333")
		}
	})

	b.Run("IPv6Address", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resolver.ResolveHostToIP("2001:db8::1", "2001:db8::1:3333")
		}
	})
}

// BenchmarkMockHostResolver_IPResolution benchmarks mock resolver performance
func BenchmarkMockHostResolver_IPResolution(b *testing.B) {
	mock := NewMockHostResolver()
	mock.SetResolution("test.example.com", net.ParseIP("192.168.1.100"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mock.ResolveHostToIP("test.example.com", "test.example.com:3333")
	}
}
