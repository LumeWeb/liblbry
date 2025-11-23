package discovery

import (
	"context"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
)

// HostResolver defines the interface for hostname resolution functionality
// This allows for different implementations (DNS, mock, caching, etc.)
type HostResolver interface {
	// ResolveHostToIP resolves a host to an IP address, supporting both IP addresses and hostnames
	// First attempts to parse as IP address for efficiency, then falls back to DNS resolution
	// Returns an error if resolution fails
	ResolveHostToIP(host, originalAddr string) (net.IP, error)
}

// DefaultHostResolver provides a standard DNS-based hostname resolution implementation
type DefaultHostResolver struct {
	logger   *zap.Logger
	timeout  time.Duration
	resolver *net.Resolver
}

// NewDefaultHostResolver creates a new DefaultHostResolver with specified logger and timeout
func NewDefaultHostResolver(logger *zap.Logger, timeout time.Duration) *DefaultHostResolver {
	if logger == nil {
		logger = zap.NewNop()
	}
	if timeout <= 0 {
		timeout = 10 * time.Second // Default timeout
	}

	return &DefaultHostResolver{
		logger:   logger,
		timeout:  timeout,
		resolver: &net.Resolver{},
	}
}

// ResolveHostToIP resolves a host to an IP address, supporting both IP addresses and hostnames
// First attempts to parse as IP address for efficiency, then falls back to DNS resolution
// Returns an error if resolution fails
func (dhr *DefaultHostResolver) ResolveHostToIP(host, originalAddr string) (net.IP, error) {
	// First try to parse as IP address (most efficient path)
	if ip := net.ParseIP(host); ip != nil {
		dhr.logger.Debug("Host is already an IP address",
			zap.String("host", host),
			zap.String("ip", ip.String()))
		return ip, nil
	}

	// If not an IP address, resolve using DNS
	dhr.logger.Debug("Resolving hostname to IP address",
		zap.String("hostname", host),
		zap.String("original_address", originalAddr))

	// Perform DNS lookup with context to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), dhr.timeout)
	defer cancel()

	ips, err := dhr.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve hostname '%s' in peer address '%s': %w", host, originalAddr, err)
	}

	if len(ips) == 0 {
		return nil, fmt.Errorf("no IP addresses found for hostname '%s' in peer address '%s'", host, originalAddr)
	}

	// Use the first IP address returned
	// Prefer IPv4 over IPv6 if both are available
	var selectedIP net.IP
	for _, ipAddr := range ips {
		if ipAddr.IP.To4() != nil {
			// Found IPv4 address, use it
			selectedIP = ipAddr.IP
			break
		}
		if selectedIP == nil {
			// Use first available IP if no IPv4 found yet
			selectedIP = ipAddr.IP
		}
	}

	if selectedIP == nil {
		return nil, fmt.Errorf("no valid IP addresses found for hostname '%s' in peer address '%s'", host, originalAddr)
	}

	dhr.logger.Debug("Successfully resolved hostname to IP",
		zap.String("hostname", host),
		zap.String("resolved_ip", selectedIP.String()),
		zap.String("original_address", originalAddr))

	return selectedIP, nil
}

// ResolveHostToIPWithTimeout resolves a host to an IP address with a custom timeout
// This is a convenience method that allows overriding the default timeout for a single resolution
func (dhr *DefaultHostResolver) ResolveHostToIPWithTimeout(host, originalAddr string, timeout time.Duration) (net.IP, error) {
	// Normalize timeout to match constructor behavior
	if timeout <= 0 {
		timeout = dhr.timeout
	}

	// Create a temporary resolver with the custom timeout
	tempResolver := &DefaultHostResolver{
		logger:   dhr.logger,
		timeout:  timeout,
		resolver: dhr.resolver,
	}
	return tempResolver.ResolveHostToIP(host, originalAddr)
}

// SetLogger updates the logger for this host resolver instance
func (dhr *DefaultHostResolver) SetLogger(logger *zap.Logger) {
	if logger == nil {
		logger = zap.NewNop()
	}
	dhr.logger = logger
}
