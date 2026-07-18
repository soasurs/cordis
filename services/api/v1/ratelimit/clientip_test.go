package ratelimit

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientIPResolverIgnoresUntrustedForwardingHeaders(t *testing.T) {
	resolver, err := NewClientIPResolver(nil)
	require.NoError(t, err)
	header := http.Header{
		"X-Forwarded-For": []string{"198.51.100.7"},
		"X-Real-Ip":       []string{"198.51.100.8"},
	}

	addr, err := resolver.Resolve("203.0.113.10:443", header)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.10", addr.String())
}

func TestClientIPResolverWalksTrustedProxyChain(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8", "192.0.2.0/24"})
	require.NoError(t, err)
	header := http.Header{
		"X-Forwarded-For": []string{"198.51.100.7, 192.0.2.20"},
	}

	addr, err := resolver.Resolve("10.0.0.5:443", header)
	require.NoError(t, err)
	require.Equal(t, "198.51.100.7", addr.String())
}

func TestClientIPResolverFallsBackOnMalformedForwardedFor(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)
	header := http.Header{"X-Forwarded-For": []string{"not-an-ip"}}

	addr, err := resolver.Resolve("10.0.0.5:443", header)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.5", addr.String())
}

func TestClientIPResolverUsesRealIPFromTrustedPeer(t *testing.T) {
	resolver, err := NewClientIPResolver([]string{"2001:db8::/32"})
	require.NoError(t, err)
	header := http.Header{"X-Real-Ip": []string{"2001:db9::7"}}

	addr, err := resolver.Resolve("[2001:db8::5]:443", header)
	require.NoError(t, err)
	require.Equal(t, "2001:db9::7", addr.String())
}

func TestNewClientIPResolverRejectsInvalidCIDR(t *testing.T) {
	_, err := NewClientIPResolver([]string{"10.0.0.1"})
	require.Error(t, err)
}
