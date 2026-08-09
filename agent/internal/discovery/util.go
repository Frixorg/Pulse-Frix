package discovery

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// readTrim reads a file and trims surrounding whitespace. Missing files return "".
func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readLines reads a file into a slice of lines.
func readLines(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// fileExists reports whether a path exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// atoiHex parses a hex string (used for /proc/net socket tables).
func atoiHex(s string) int {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 16, 64)
	return int(n)
}

// tcpProbe attempts a TCP connection and reports success within the timeout.
// Used for read-only service health checks (no data is written).
func tcpProbe(address string, timeout time.Duration) bool {
	c, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// FindFreePort asks the kernel for an unused TCP port. The port is released
// immediately; callers must bind promptly. Pulse NEVER assumes a port is free.
func FindFreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// lookPath is a thin wrapper so detectors can check for a binary's presence
// without ever executing user-controlled input.
func lookPath(bin string) (string, bool) {
	p, err := exec.LookPath(bin)
	return p, err == nil
}

// runFixed runs a binary with a FIXED argument vector (never user input) and
// returns stdout. This is not shell execution (no shell, no interpolation) and
// has no injection surface. Used only for read-only inspection where /proc does
// not expose the data (e.g. `systemctl` state).
func runFixed(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}
