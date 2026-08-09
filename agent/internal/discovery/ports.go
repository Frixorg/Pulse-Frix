package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// PortDetector enumerates listening TCP/UDP sockets by parsing /proc/net/*.
// This is read-only and requires no privileges beyond reading /proc. It also
// backs the installer's "never assume a port is free" guarantee.
type PortDetector struct{}

func (PortDetector) ID() string      { return "ports" }
func (PortDetector) Name() string    { return "Port Detector" }
func (PortDetector) Version() string { return "1.0" }

func (PortDetector) Available(context.Context) model.Availability {
	if fileExists("/proc/net/tcp") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "/proc/net/tcp not present"}
}

func (PortDetector) Detect(context.Context) ([]model.Resource, error) {
	listeners := ListeningPorts()
	var out []model.Resource
	for _, l := range listeners {
		out = append(out, model.Resource{
			Type:       "listening_port",
			ID:         fmt.Sprintf("port:%s:%d/%s", l.Address, l.Port, l.Protocol),
			Name:       fmt.Sprintf("%d/%s", l.Port, l.Protocol),
			Health:     model.StatusHealthy,
			DetectedBy: "ports",
			DetectedAt: time.Now().UTC(),
			Ports:      []model.Port{{Host: l.Port, Protocol: l.Protocol, Address: l.Address, State: "LISTEN"}},
			Attributes: map[string]any{
				"exposure": l.Exposure(),
			},
		})
	}
	return out, nil
}

func (PortDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// Listener describes a single listening socket.
type Listener struct {
	Address  string
	Port     int
	Protocol string
}

// Exposure classifies whether the listener is bound to loopback or all
// interfaces (informational; used by the Security view).
func (l Listener) Exposure() string {
	switch l.Address {
	case "127.0.0.1", "::1":
		return "loopback"
	case "0.0.0.0", "::", "*":
		return "public"
	default:
		return "bound"
	}
}

// ListeningPorts parses the /proc/net socket tables for sockets in LISTEN state.
func ListeningPorts() []Listener {
	var out []Listener
	out = append(out, parseProcNet("/proc/net/tcp", "tcp", true)...)
	out = append(out, parseProcNet("/proc/net/tcp6", "tcp", true)...)
	out = append(out, parseProcNet("/proc/net/udp", "udp", false)...)
	out = append(out, parseProcNet("/proc/net/udp6", "udp", false)...)
	return out
}

// parseProcNet parses a /proc/net/{tcp,udp}[6] table. For TCP, only sockets in
// the LISTEN state (0x0A) are returned.
func parseProcNet(path, proto string, listenOnly bool) []Listener {
	lines := readLines(path)
	var out []Listener
	seen := map[string]bool{}
	for i, line := range lines {
		if i == 0 { // header
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		if listenOnly && fields[3] != "0A" {
			continue
		}
		addrHex, portHex, ok := strings.Cut(fields[1], ":")
		if !ok {
			continue
		}
		ip := hexToIP(addrHex)
		port := atoiHex(portHex)
		key := fmt.Sprintf("%s:%d/%s", ip, port, proto)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Listener{Address: ip, Port: port, Protocol: proto})
	}
	return out
}

// hexToIP converts the little-endian hex address used by /proc/net tables into
// a human-readable IP. IPv6 is summarised (full expansion is not needed here).
func hexToIP(h string) string {
	switch len(h) {
	case 8: // IPv4, little-endian
		b0 := atoiHex(h[6:8])
		b1 := atoiHex(h[4:6])
		b2 := atoiHex(h[2:4])
		b3 := atoiHex(h[0:2])
		return fmt.Sprintf("%d.%d.%d.%d", b0, b1, b2, b3)
	case 32: // IPv6 (address stored as 4 little-endian 32-bit words)
		up := strings.ToUpper(h)
		if up == "00000000000000000000000000000000" {
			return "::"
		}
		// ::1 loopback appears as the last word = 01000000 (little-endian 1).
		if up == "00000000000000000000000001000000" {
			return "::1"
		}
		return "::"
	default:
		return "0.0.0.0"
	}
}
