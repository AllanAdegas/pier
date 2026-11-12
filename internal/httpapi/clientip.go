package httpapi

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// proxyTrust decides which peers may describe the client they forward.
// Forwarding headers from anyone else are ignored: they are just text the
// client wrote.
type proxyTrust struct {
	prefixes []netip.Prefix
}

func (t proxyTrust) trusts(address netip.Addr) bool {
	address = address.Unmap()
	for _, prefix := range t.prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// clientAddress returns the caller's IP. X-Forwarded-For is read from the
// right, skipping trusted proxies, so the first untrusted hop is the client:
// a value the client put on the left cannot choose its own identity.
func (t proxyTrust) clientAddress(r *http.Request) string {
	peer := peerAddress(r)
	if !peer.IsValid() || !t.trusts(peer) {
		return remoteHost(r)
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for index := len(hops) - 1; index >= 0; index-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[index]))
		if err != nil {
			// A malformed hop means the chain cannot be trusted past it.
			break
		}
		if !t.trusts(hop) {
			return hop.Unmap().String()
		}
	}
	return remoteHost(r)
}

// isHTTPS reports whether the client reached Pier over TLS, directly or
// through a trusted proxy that terminated it.
func (t proxyTrust) isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	peer := peerAddress(r)
	return peer.IsValid() && t.trusts(peer) && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func peerAddress(r *http.Request) netip.Addr {
	address, err := netip.ParseAddr(remoteHost(r))
	if err != nil {
		return netip.Addr{}
	}
	return address
}
