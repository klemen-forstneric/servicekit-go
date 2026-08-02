package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const headerForwardedFor = "X-Forwarded-For"

// IPResolver reports the client address for a request, honouring
// X-Forwarded-For only when the peer is a configured trusted proxy. The
// zero value trusts nothing and always reports the peer address.
type IPResolver struct {
	trusted []netip.Prefix
}

// NewIPResolver builds a resolver from CIDRs or bare addresses (treated as
// single-host prefixes). Blank entries are ignored.
func NewIPResolver(cidrs []string) (*IPResolver, error) {
	trusted := make([]netip.Prefix, 0, len(cidrs))

	for _, raw := range cidrs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		if p, err := netip.ParsePrefix(entry); err == nil {
			trusted = append(trusted, p)
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, fmt.Errorf("httpx: invalid trusted proxy %q: %w", entry, err)
		}
		trusted = append(trusted, netip.PrefixFrom(addr, addr.BitLen()))
	}

	return &IPResolver{trusted: trusted}, nil
}

// ClientIP walks X-Forwarded-For right to left and reports the first hop that is
// not a trusted proxy. The header accumulates by appending, so anything left of
// that hop is caller-supplied and must not be trusted. Any other outcome — an
// untrusted peer, an absent header, a fully trusted chain, a malformed hop —
// falls back to the peer address.
func (r *IPResolver) ClientIP(req *http.Request) string {
	raw := peerAddr(req)
	peer, ok := parseHop(raw)
	if !ok {
		return raw
	}

	if !r.trusts(peer) {
		return peer.String()
	}

	// Repeated field lines are semantically the comma-joined list (RFC 7230
	// §3.2.2); Header.Get would see only a prefix of the chain.
	forwarded := strings.Join(req.Header.Values(headerForwardedFor), ",")
	if forwarded == "" {
		return peer.String()
	}

	hops := strings.Split(forwarded, ",")
	for i := len(hops) - 1; i >= 0; i-- {
		hop, ok := parseHop(strings.TrimSpace(hops[i]))
		if !ok {
			return peer.String()
		}
		if !r.trusts(hop) {
			return hop.String()
		}
	}
	return peer.String()
}

// parseHop normalizes one address: proxies such as Azure Front Door append
// ip:port, Unmap lets a 4-in-6 address match an IPv4 prefix, and a zone must go
// because netip.Prefix.Contains rejects every zoned address.
func parseHop(s string) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(s)
	if err != nil {
		host, _, splitErr := net.SplitHostPort(s)
		if splitErr != nil {
			return netip.Addr{}, false
		}
		if addr, err = netip.ParseAddr(host); err != nil {
			return netip.Addr{}, false
		}
	}
	return addr.Unmap().WithZone(""), true
}

func (r *IPResolver) trusts(addr netip.Addr) bool {
	if r == nil {
		return false
	}
	for _, p := range r.trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

func peerAddr(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
