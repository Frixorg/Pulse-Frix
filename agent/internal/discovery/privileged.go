package discovery

import (
	"os"
	"regexp"
	"strconv"
	"strings"
)

// Privilege escalation, in the one place it can happen.
//
// Pulse discovery is designed to need none: it reads /proc, /sys, config files
// and a read-only Docker socket, and the supported containerised deployment
// grants that visibility with `pid: host` plus a read-only `/:/host:ro` mount.
// A host-installed agent running as an unprivileged user, however, cannot read
// /proc/<pid>/fd and therefore cannot say which process owns a listening
// socket — the ports are found, but nothing can be attributed to them.
//
// For that case ONLY, and only when the operator opts in with PULSE_USE_SUDO,
// the two read-only commands below may be re-run through `sudo -n`:
//
//	systemctl list-units --type=service --state=<running|failed> ...
//	ss -tulpnH
//
// Both are inspection commands that cannot change anything, both are invoked
// with a FIXED argument vector (no shell, no interpolation, no user input), and
// `sudo -n` never prompts — a missing or denied rule fails immediately and the
// detector degrades instead of hanging. The matching least-privilege rule ships
// as infrastructure/pulse-discovery.sudoers.
//
// Nothing else in the agent may ever escalate.

// sudoEnabled reports whether the operator opted in to the read-only sudo
// fallback. It is off by default.
func sudoEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PULSE_USE_SUDO"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// runFixedPrivileged runs a fixed argument vector directly and, only if that
// fails and sudo is enabled, retries it once through `sudo -n`. The argument
// vector is identical in both attempts, so the sudoers rule can match it
// exactly.
func runFixedPrivileged(name string, args ...string) (string, error) {
	out, err := runFixed(name, args...)
	if err == nil || !sudoEnabled() {
		return out, err
	}
	if _, ok := lookPath("sudo"); !ok {
		return out, err
	}
	// -n: never prompt. Without a matching NOPASSWD rule this returns an error
	// immediately rather than blocking a discovery run on a password prompt.
	sudoArgs := append([]string{"-n", name}, args...)
	return runFixed("sudo", sudoArgs...)
}

// ssOwner is one socket-to-process attribution reported by `ss`.
type ssOwner struct {
	PID     int
	Process string
}

// ssUsers matches the users:(("sshd",pid=812,fd=3)) column of `ss -tulpnH`.
var ssUsers = regexp.MustCompile(`\(\("([^"]+)",pid=(\d+)`)

// ssListenerOwners asks `ss` which process owns each listening socket, keyed by
// "<protocol>:<port>". This is the documented fallback for when /proc/<pid>/fd
// is unreadable; it returns an empty map when ss is absent or not permitted,
// and callers simply keep their unattributed listeners.
func ssListenerOwners() map[string]ssOwner {
	owners := map[string]ssOwner{}
	if _, ok := lookPath("ss"); !ok {
		return owners
	}
	out, err := runFixedPrivileged("ss", "-tulpnH")
	if err != nil {
		return owners
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		proto := strings.ToLower(fields[0])
		if proto != "tcp" && proto != "udp" {
			continue
		}
		port := portFromSSAddress(fields[4])
		if port == 0 {
			continue
		}
		m := ssUsers.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		pid, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		key := proto + ":" + strconv.Itoa(port)
		if _, exists := owners[key]; !exists {
			owners[key] = ssOwner{PID: pid, Process: m[1]}
		}
	}
	return owners
}

// portFromSSAddress pulls the port out of an ss local-address column, which is
// "0.0.0.0:80", "[::]:80", "*:80" or "127.0.0.1:5432".
func portFromSSAddress(addr string) int {
	i := strings.LastIndex(addr, ":")
	if i < 0 || i == len(addr)-1 {
		return 0
	}
	port, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return 0
	}
	return port
}
