// Package clientip resolves original client addresses across trusted proxies.
package clientip

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
)

// Resolver extracts client addresses without trusting forwarded headers from
// peers outside the configured proxy networks.
type Resolver struct {
	trustedProxies []netip.Prefix
}

// Family identifies the source address family used for limit selection.
type Family string

const (
	FamilyIPv4 Family = "ipv4"
	FamilyIPv6 Family = "ipv6"
)

// Scope is the canonical network source used by IP-based safety limits.
// IPv4 addresses use a /32 and IPv6 addresses use a /64 so privacy addresses
// from the same subscriber network share one bucket.
type Scope struct {
	Family Family
	Prefix netip.Prefix
}

// SourceScope constructs the canonical limiting scope for addr.
func SourceScope(addr netip.Addr) Scope {
	addr = addr.Unmap()
	bits := 64
	family := FamilyIPv6
	if addr.Is4() {
		bits = 32
		family = FamilyIPv4
	}
	return Scope{Family: family, Prefix: netip.PrefixFrom(addr, bits).Masked()}
}

// Key returns a family-qualified, stable bucket key.
func (s Scope) Key() string {
	return string(s.Family) + ":" + s.Prefix.String()
}

// New parses the CIDR ranges of trusted reverse proxies.
func New(trustedProxies []string) (*Resolver, error) {
	prefixes := make([]netip.Prefix, 0, len(trustedProxies))
	for _, value := range trustedProxies {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			return nil, err
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return &Resolver{trustedProxies: prefixes}, nil
}

// Resolve returns the original client address for an HTTP request.
//
// Forwarded headers are considered only when the immediate peer is trusted.
// X-Forwarded-For is walked from right to left and the first untrusted address
// is selected. X-Real-IP is used only when X-Forwarded-For is absent.
func (r *Resolver) Resolve(peerAddress string, header http.Header) (netip.Addr, error) {
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
		for _, addresse := range slices.Backward(addresses) {
			if !r.isTrusted(addresse) {
				return addresse, nil
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

func (r *Resolver) isTrusted(addr netip.Addr) bool {
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
