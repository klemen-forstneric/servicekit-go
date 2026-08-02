package httpx_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/klemen-forstneric/servicekit-go/httpx"
)

func request(remoteAddr, xff string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = remoteAddr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIPHonoursForwardedForFromTrustedPeer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestClientIPWalksChainRightToLeftPastTrustedHops(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7, 10.1.2.3")))
}

// X-Forwarded-For accumulates left-to-right by appending, so the leftmost entry
// is whatever the client sent. Only the rightmost untrusted hop is attested by a
// proxy we trust.
func TestClientIPIgnoresSpoofedLeftmostEntry(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	got := res.ClientIP(request("10.1.2.3:5555", "1.2.3.4, 198.51.100.9"))
	assert.Equal(t, "198.51.100.9", got)
	assert.NotEqual(t, "1.2.3.4", got)
}

func TestClientIPFullyTrustedChainFallsBackToPeer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "10.9.9.9, 10.8.8.8")))
}

func TestClientIPMalformedHopDuringWalkFallsBackToPeer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7, garbage, 10.4.4.4")))
}

// The whole point: an untrusted caller cannot name its own IP.
func TestClientIPIgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "198.51.100.9", res.ClientIP(request("198.51.100.9:5555", "203.0.113.7")))
}

func TestClientIPEmptyConfigTrustsNothing(t *testing.T) {
	res, err := httpx.NewIPResolver(nil)
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestClientIPMalformedForwardedForFallsBack(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "not-an-ip")))
}

func TestClientIPAcceptsBareAddressAsTrustedProxy(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"172.18.0.10"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("172.18.0.10:5555", "203.0.113.7")))
	assert.Equal(t, "172.18.0.11", res.ClientIP(request("172.18.0.11:5555", "203.0.113.7")))
}

func TestClientIPHandlesRemoteAddrWithoutPort(t *testing.T) {
	res, err := httpx.NewIPResolver(nil)
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3", "")))
}

func TestClientIPSkipsBlankEntries(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"", " 10.0.0.0/8 ", ""})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestNewIPResolverRejectsGarbage(t *testing.T) {
	_, err := httpx.NewIPResolver([]string{"not-a-cidr"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-cidr")
}

func TestClientIPNilResolverTrustsNothing(t *testing.T) {
	var res *httpx.IPResolver
	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7")))
}

func TestClientIPHonoursForwardedForFromTrustedIPv6Peer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"2001:db8::/32"})
	require.NoError(t, err)

	// The forwarded client sits outside the trusted prefix; one inside it would
	// be indistinguishable from another proxy hop.
	assert.Equal(t, "2606:4700::99", res.ClientIP(request("[2001:db8::5]:5555", "2606:4700::99")))
}

func TestClientIPIgnoresForwardedForFromUntrustedIPv6Peer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"2001:db8::/32"})
	require.NoError(t, err)

	assert.Equal(t, "2001:db9::5", res.ClientIP(request("[2001:db9::5]:5555", "2001:db8::99")))
}

// The bug this fix round exists for: a 4-in-6 peer must still match an IPv4 trusted prefix.
func TestClientIPTrustsIPv4MappedIPv6Peer(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("[::ffff:10.1.2.3]:5555", "203.0.113.7")))
}

func TestClientIPReturnsForwardedIPv6Address(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "2001:db8::1", res.ClientIP(request("10.1.2.3:5555", "2001:db8::1")))
}

func TestClientIPUnmapsIPv4MappedIPv6ForwardedFor(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", "::ffff:203.0.113.7")))
}

// The walk stops at the rightmost untrusted hop, so a blank segment further
// left is never examined.
func TestClientIPEmptyLeadingSegmentIsNeverExamined(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "203.0.113.7", res.ClientIP(request("10.1.2.3:5555", ",203.0.113.7")))
}

func TestClientIPEmptyTrailingSegmentFallsBack(t *testing.T) {
	res, err := httpx.NewIPResolver([]string{"10.0.0.0/8"})
	require.NoError(t, err)

	assert.Equal(t, "10.1.2.3", res.ClientIP(request("10.1.2.3:5555", "203.0.113.7,")))
}
