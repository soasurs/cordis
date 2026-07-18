package ratelimit

import "github.com/soasurs/cordis/pkg/clientip"

// ClientIPResolver extracts original client addresses across trusted proxies.
type ClientIPResolver = clientip.Resolver

// NewClientIPResolver parses the CIDR ranges of trusted reverse proxies.
func NewClientIPResolver(trustedProxies []string) (*ClientIPResolver, error) {
	return clientip.New(trustedProxies)
}
