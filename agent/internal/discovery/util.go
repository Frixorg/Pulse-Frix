package discovery

import (
	"bufio"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

// readable reports whether a file can actually be opened for reading. Files
// under /proc often exist for callers that are not permitted to read them, so
// os.Stat alone is not enough.
func readable(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

// hostRoot is the prefix under which the HOST filesystem is visible. It is ""
// for an agent installed directly on the VPS and typically "/host" when the
// agent runs containerised with the host root bind-mounted read-only
// (PULSE_ROOTFS — see infrastructure/docker-compose.agent.yml).
func hostRoot() string {
	return strings.TrimRight(os.Getenv("PULSE_ROOTFS"), "/")
}

// hostPath maps an absolute host path into the agent's view of the filesystem.
// Always use it for anything read out of /etc, /var or /run.
func hostPath(p string) string { return hostRoot() + p }

// hostGlob expands a host-absolute glob pattern under the host rootfs.
func hostGlob(pattern string) []string {
	m, _ := filepath.Glob(hostPath(pattern))
	return m
}

// hostFileExists reports whether an absolute host path exists.
func hostFileExists(p string) bool { return fileExists(hostPath(p)) }

// displayPath strips the rootfs prefix so the dashboard shows the path as the
// operator knows it (/etc/nginx/..., not /host/etc/nginx/...).
func displayPath(p string) string {
	root := hostRoot()
	if root != "" && strings.HasPrefix(p, root+"/") {
		return strings.TrimPrefix(p, root)
	}
	return p
}

// atoiHex parses a hex string (used for /proc/net socket tables).
func atoiHex(s string) int {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 16, 64)
	return int(n)
}

// atoiDec parses an unsigned decimal field (e.g. a socket inode). Unparseable
// values become 0, which every caller already treats as "unknown".
func atoiDec(s string) uint64 {
	n, _ := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	return n
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
