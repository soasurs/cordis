package clientip

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSourceScopeUsesIPv4Address(t *testing.T) {
	scope := SourceScope(netip.MustParseAddr("198.51.100.7"))
	require.Equal(t, FamilyIPv4, scope.Family)
	require.Equal(t, "ipv4:198.51.100.7/32", scope.Key())
}

func TestSourceScopeMasksIPv6PrivacyAddressTo64(t *testing.T) {
	first := SourceScope(netip.MustParseAddr("2001:db8:1234:5678::1"))
	second := SourceScope(netip.MustParseAddr("2001:db8:1234:5678:abcd::2"))
	require.Equal(t, FamilyIPv6, first.Family)
	require.Equal(t, "ipv6:2001:db8:1234:5678::/64", first.Key())
	require.Equal(t, first, second)
}
