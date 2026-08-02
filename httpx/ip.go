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

func (r *IPResolver) ClientIP(req *http.Request) string {
	peer := peerAddr(req)
	// Unmap so a 4-in-6 peer (e.g. ::ffff:10.1.2.3) matches an IPv4 trusted prefix.
	if addr, err := netip.ParseAddr(peer); err == nil {
		peer = addr.Unmap().String()
	}

	if !r.trusts(peer) {
		return peer
	}

	forwarded := req.Header.Get(headerForwardedFor)
	if forwarded == "" {
		return peer
	}

	first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
	addr, err := netip.ParseAddr(first)
	if err != nil {
		return peer
	}
	return addr.Unmap().String()
}

func (r *IPResolver) trusts(peer string) bool {
	if r == nil || len(r.trusted) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil {
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
