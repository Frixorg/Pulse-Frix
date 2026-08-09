// Package ssrf provides a URL/IP validation layer to prevent Server-Side
// Request Forgery. The platform fetches user/discovery-provided domains and
// service URLs (for health/TLS checks), so every server-side fetch MUST pass
// through here. See docs/THREAT_MODEL.md#t5-ssrf and spec section 66.
package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrBlocked is returned when a target is not allowed.
var ErrBlocked = errors.New("ssrf: target blocked")

// Validate parses a URL and ensures it uses http/https and does not resolve to
// a loopback, link-local, private, or otherwise internal address. It resolves
// DNS and checks EVERY resolved IP (mitigating DNS-rebinding / TOCTOU).
func Validate(rawURL string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid url", ErrBlocked)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return nil, fmt.Errorf("%w: scheme %q not allowed", ErrBlocked, u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", ErrBlocked)
	}
	// Reject obvious names outright.
	if isBlockedName(host) {
		return nil, fmt.Errorf("%w: blocked host %q", ErrBlocked, host)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil {
		return nil, fmt.Errorf("%w: dns resolution failed", ErrBlocked)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%w: no addresses", ErrBlocked)
	}
	for _, ip := range ips {
		if !IPAllowed(ip.IP) {
			return nil, fmt.Errorf("%w: address %s is internal", ErrBlocked, ip.IP)
		}
	}
	return u, nil
}

// isBlockedName rejects hostnames that must never be fetched regardless of DNS.
func isBlockedName(host string) bool {
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	switch h {
	case "localhost", "metadata.google.internal":
		return true
	}
	if strings.HasSuffix(h, ".localhost") || strings.HasSuffix(h, ".internal") {
		return true
	}
	return false
}

// IPAllowed reports whether an IP is an acceptable external target.
func IPAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	if ip.IsPrivate() { // RFC1918 + IPv6 ULA (fc00::/7)
		return false
	}
	// Explicit extras: cloud metadata, carrier-grade NAT, Docker bridge ranges.
	// NOTE: IPv4-mapped IPv6 (::ffff:a.b.c.d) is handled by the checks above and
	// by these v4 CIDRs, because Go's net.IP methods and IPNet.Contains normalise
	// via To4(). Do NOT add ::ffff:0:0/96 here — its mask degenerates to /0 in
	// 4-byte space and would block ALL IPv4 addresses.
	for _, cidr := range extraBlocked {
		if cidr.Contains(ip) {
			return false
		}
	}
	if v4 := ip.To4(); v4 != nil {
		// 0.0.0.0/8 and 240.0.0.0/4 are not routable.
		if v4[0] == 0 || v4[0] >= 240 {
			return false
		}
	}
	return true
}

var extraBlocked = mustCIDRs(
	"169.254.0.0/16", // link-local / cloud metadata (169.254.169.254)
	"100.64.0.0/10",  // carrier-grade NAT
	"192.0.0.0/24",   // IETF protocol assignments
	"192.0.2.0/24",   // TEST-NET-1
	"198.18.0.0/15",  // benchmarking
	"172.17.0.0/16",  // default docker bridge
	"fc00::/7",       // IPv6 ULA (also covered by IsPrivate, belt and braces)
)

func mustCIDRs(cidrs ...string) []*net.IPNet {
	var out []*net.IPNet
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			out = append(out, n)
		}
	}
	return out
}

// SafeClient returns an *http.Client whose dialer re-validates the destination
// IP at connect time and whose redirect policy re-validates each hop. This
// closes the gap where DNS could resolve differently between Validate() and the
// actual dial.
func SafeClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			for _, ip := range ips {
				if !IPAllowed(ip.IP) {
					return nil, fmt.Errorf("%w: dial to internal %s", ErrBlocked, ip.IP)
				}
			}
			// Dial the first allowed IP explicitly to avoid a re-resolve.
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if _, err := Validate(req.URL.String()); err != nil {
				return err
			}
			return nil
		},
	}
}
