package ssrf

import (
	"net"
	"testing"
)

func TestIPAllowedBlocksInternal(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "::1",
		"10.0.0.5", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", // cloud metadata
		"172.17.0.2",      // docker bridge
		"0.0.0.0",
		"fc00::1", "fe80::1",
		"100.64.0.1",
		"::ffff:127.0.0.1",     // IPv4-mapped loopback must still be blocked
		"::ffff:169.254.169.254", // IPv4-mapped metadata must still be blocked
	}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if IPAllowed(ip) {
			t.Errorf("IPAllowed(%s) = true, want false (should be blocked)", s)
		}
	}
}

func TestIPAllowedPermitsPublic(t *testing.T) {
	allowed := []string{
		"8.8.8.8", "1.1.1.1", "93.184.216.34",
		"2606:2800:220:1:248:1893:25c8:1946",
		"::ffff:8.8.8.8", // IPv4-mapped public address must be allowed
	}
	for _, s := range allowed {
		ip := net.ParseIP(s)
		if !IPAllowed(ip) {
			t.Errorf("IPAllowed(%s) = false, want true (public address)", s)
		}
	}
}

func TestValidateSchemeAndNames(t *testing.T) {
	bad := []string{
		"file:///etc/passwd",
		"gopher://evil/",
		"http://localhost/admin",
		"https://metadata.google.internal/",
		"http://foo.internal/",
		"ftp://example.com/",
	}
	for _, u := range bad {
		if _, err := Validate(u); err == nil {
			t.Errorf("Validate(%q) = nil error, want blocked", u)
		}
	}
}
