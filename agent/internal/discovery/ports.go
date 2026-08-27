package discovery

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// PortDetector enumerates listening TCP/UDP sockets by parsing /proc/net/* and
// attributes each one to the process that owns it — the same correlation
// `ss -tulpn` and `lsof -i` perform, done here by reading /proc directly so no
// binary has to exist on the host and no shell is ever involved.
//
// The owning process also tells us whether the listener is a host-native
// workload or one published from a container, which is what separates the two
// halves of the inventory view.
//
// This is read-only and needs no privileges beyond reading /proc. It also backs
// the installer's "never assume a port is free" guarantee.
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
	listeners := ListeningPortsWithOwners()
	var out []model.Resource
	for _, l := range listeners {
		attrs := map[string]any{
			"exposure": l.Exposure(),
		}
		if l.PID > 0 {
			attrs["pid"] = l.PID
			attrs["process"] = l.Process
			attrs["workload"] = ternaryString(l.ContainerID != "", "container", "host")
			if l.Unit != "" {
				attrs["unit"] = l.Unit
			}
			if l.ContainerID != "" {
				attrs["container_id"] = l.ContainerID
			}
		}
		name := fmt.Sprintf("%d/%s", l.Port, l.Protocol)
		if l.Process != "" {
			name = fmt.Sprintf("%d/%s (%s)", l.Port, l.Protocol, l.Process)
		}
		out = append(out, model.Resource{
			Type:       "listening_port",
			ID:         fmt.Sprintf("port:%s:%d/%s", l.Address, l.Port, l.Protocol),
			Name:       name,
			Health:     model.StatusHealthy,
			DetectedBy: "ports",
			DetectedAt: time.Now().UTC(),
			Ports:      []model.Port{{Host: l.Port, Protocol: l.Protocol, Address: l.Address, State: "LISTEN"}},
			Attributes: attrs,
		})
	}
	return out, nil
}

func (PortDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// Listener describes a single listening socket and, when the agent can read the
// owning process, what is behind it.
type Listener struct {
	Address  string
	Port     int
	Protocol string
	// Inode is the socket inode from /proc/net/*, used to find the owner.
	Inode uint64
	// PID is 0 when the owner could not be determined (a non-root agent can
	// only read its own file descriptors). Treat 0 as "unknown", never as
	// "nothing is listening".
	PID         int
	Process     string
	Unit        string
	ContainerID string
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
	out = append(out, parseProcNet(procNetFile("tcp"), "tcp", true)...)
	out = append(out, parseProcNet(procNetFile("tcp6"), "tcp", true)...)
	out = append(out, parseProcNet(procNetFile("udp"), "udp", false)...)
	out = append(out, parseProcNet(procNetFile("udp6"), "udp", false)...)
	return out
}

// procNetFile picks the socket table to read.
//
// /proc/net/* is always the CALLER's network namespace, which for a
// containerised agent is the container's — it would report the agent's own
// sockets and miss every host service. /proc/1/net/* is PID 1's namespace
// instead, so with `pid: host` it is the host's. Without `pid: host` (and for a
// host-installed agent) PID 1 is the same namespace anyway, which makes this
// the correct choice in every deployment rather than a special case.
//
// It must be READABLE, not merely present: /proc/1/net exists for an
// unprivileged agent that is not allowed to read it, and silently returning an
// empty table would look like "nothing is listening".
func procNetFile(name string) string {
	if initNet := "/proc/1/net/" + name; readable(initNet) {
		return initNet
	}
	return "/proc/net/" + name
}

// ListeningPortsWithOwners is ListeningPorts plus the socket -> process
// correlation, attempted two ways:
//
//  1. Socket inodes matched against /proc/<pid>/fd. This is the primary path:
//     no binary, no shell, no privilege escalation.
//  2. `ss -tulpnH`, for a host-installed unprivileged agent that cannot read
//     other processes' descriptors. Only tried for listeners step 1 missed,
//     and only escalates when the operator opted in (see privileged.go).
//
// A listener whose owner neither path can name keeps its zero PID. Treat that
// as "owner unknown", never as "nothing is listening".
func ListeningPortsWithOwners() []Listener {
	listeners := ListeningPorts()
	procs := IndexProcesses(ScanProcesses())

	if owners := SocketOwners(); len(owners) > 0 {
		for i := range listeners {
			pid, ok := owners[listeners[i].Inode]
			if !ok {
				continue
			}
			listeners[i].PID = pid
			applyOwner(&listeners[i], procs, pid, "")
		}
	}

	// Fall back only for what is still unattributed, so the common case never
	// pays for an extra process.
	if !anyUnattributed(listeners) {
		return listeners
	}
	ssOwners := ssListenerOwners()
	if len(ssOwners) == 0 {
		return listeners
	}
	for i := range listeners {
		if listeners[i].PID != 0 {
			continue
		}
		owner, ok := ssOwners[listeners[i].Protocol+":"+strconv.Itoa(listeners[i].Port)]
		if !ok {
			continue
		}
		listeners[i].PID = owner.PID
		applyOwner(&listeners[i], procs, owner.PID, owner.Process)
	}
	return listeners
}

// applyOwner copies what is known about the owning process onto a listener.
// fallbackName is used when the process is no longer in the scan (it exited
// between the two reads) but the source still named it.
func applyOwner(l *Listener, procs ProcessIndex, pid int, fallbackName string) {
	if p, ok := procs[pid]; ok {
		l.Process = p.Comm
		l.Unit = p.Unit
		l.ContainerID = p.ContainerID
		return
	}
	l.Process = fallbackName
}

func anyUnattributed(listeners []Listener) bool {
	for _, l := range listeners {
		if l.PID == 0 {
			return true
		}
	}
	return false
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
		l := Listener{Address: ip, Port: port, Protocol: proto}
		// Column 9 is the socket inode, which links this row to a /proc/<pid>/fd
		// entry. Its absence is not fatal — the socket is still reported.
		if len(fields) > 9 {
			l.Inode = atoiDec(fields[9])
		}
		out = append(out, l)
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
