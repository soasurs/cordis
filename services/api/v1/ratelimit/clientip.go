package ratelimit

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIPResolver extracts client addresses without trusting forwarded
// headers from peers outside the configured proxy networks.
type ClientIPResolver struct {
	trustedProxies []netip.Prefix
}

// NewClientIPResolver parses the CIDR ranges of trusted reverse proxies.
func NewClientIPResolver(trustedProxies []string) (*ClientIPResolver, error) {
	prefixes := make([]netip.Prefix, 0, len(trustedProxies))
	for _, value := range trustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return &ClientIPResolver{trustedProxies: prefixes}, nil
}

// Resolve returns the original client address for a Connect request.
//
// Forwarded headers are considered only when the immediate peer is trusted.
// X-Forwarded-For is walked from right to left and the first untrusted address
// is selected. X-Real-IP is used only when X-Forwarded-For is absent.
func (r *ClientIPResolver) Resolve(peerAddress string, header http.Header) (netip.Addr, error) {
	peer, err := parseAddress(peerAddress)
	if err != nil {
		return netip.Addr{}, err
	}
	if !r.isTrusted(peer) {
		return peer, nil
	}

	if forwarded := header.Values("X-Forwarded-For"); len(forwarded) > 0 {
		addresses, ok := parseForwardedFor(forwarded)
		if !ok {
			return peer, nil
		}
		for i := len(addresses) - 1; i >= 0; i-- {
			if !r.isTrusted(addresses[i]) {
				return addresses[i], nil
			}
		}
		if len(addresses) > 0 {
			return addresses[0], nil
		}
	}

	if realIP := strings.TrimSpace(header.Get("X-Real-IP")); realIP != "" {
		if addr, err := netip.ParseAddr(realIP); err == nil {
			return addr.Unmap(), nil
		}
	}
	return peer, nil
}

func (r *ClientIPResolver) isTrusted(addr netip.Addr) bool {
	for _, prefix := range r.trustedProxies {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func parseAddress(value string) (netip.Addr, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, errors.New("peer address is required")
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return netip.Addr{}, err
	}
	return addr.Unmap(), nil
}

func parseForwardedFor(values []string) ([]netip.Addr, bool) {
	var addresses []netip.Addr
	for _, value := range values {
		for part := range strings.SplitSeq(value, ",") {
			addr, err := netip.ParseAddr(strings.TrimSpace(part))
			if err != nil {
				return nil, false
			}
			addresses = append(addresses, addr.Unmap())
		}
	}
	return addresses, len(addresses) > 0
}
