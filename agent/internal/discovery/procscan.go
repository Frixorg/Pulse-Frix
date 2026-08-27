package discovery

import (
	"os"
	"strconv"
	"strings"
)

// Shared /proc scanning used by the process, port, service and database
// detectors. Everything here is a read of /proc — no command is executed and
// nothing is ever written.
//
// When the agent runs containerised it needs `pid: host` to see the host's
// processes (infrastructure/docker-compose.agent.yml sets it). Without that it
// simply sees fewer processes and every consumer degrades to "unknown owner".

// ProcInfo is one process as read from /proc/<pid>.
type ProcInfo struct {
	PID    int
	Comm   string
	State  string
	RSSKiB uint64
	// Unit is the systemd unit the process belongs to, derived from its
	// cgroup path (e.g. "nginx.service"). Empty on non-systemd hosts.
	Unit string
	// ContainerID is the container the process belongs to, also from the
	// cgroup path. Empty for host-native processes — this is exactly what
	// separates host workloads from containerised ones in the inventory.
	ContainerID string
}

// Containerised reports whether the process runs inside a container.
func (p ProcInfo) Containerised() bool { return p.ContainerID != "" }

// maxFDScan bounds the file-descriptor walk in SocketOwners so an unusually
// busy host can never turn discovery into a long stall.
const maxFDScan = 200000

// ScanProcesses reads every readable process under /proc. Processes that
// disappear mid-scan are skipped rather than reported as errors.
func ScanProcesses() []ProcInfo {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	pageSize := uint64(4096)
	out := make([]ProcInfo, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat := readTrim("/proc/" + e.Name() + "/stat")
		if stat == "" {
			continue
		}
		// comm is inside parentheses and may itself contain spaces, so the
		// remaining fields are taken from after the LAST ')'.
		openIdx := strings.IndexByte(stat, '(')
		closeIdx := strings.LastIndexByte(stat, ')')
		if openIdx < 0 || closeIdx < 0 || closeIdx < openIdx {
			continue
		}
		rest := strings.Fields(stat[closeIdx+1:])
		if len(rest) < 22 {
			continue
		}
		rssPages, _ := strconv.ParseUint(rest[21], 10, 64)
		p := ProcInfo{
			PID:    pid,
			Comm:   stat[openIdx+1 : closeIdx],
			State:  rest[0],
			RSSKiB: rssPages * pageSize / 1024,
		}
		p.Unit, p.ContainerID = parseCgroup(readTrim("/proc/" + e.Name() + "/cgroup"))
		out = append(out, p)
	}
	return out
}

// parseCgroup pulls the systemd unit and container id out of a /proc/<pid>/cgroup
// file. It handles both cgroup v2 ("0::/system.slice/nginx.service") and v1
// ("1:name=systemd:/docker/<id>").
func parseCgroup(content string) (unit, containerID string) {
	for _, line := range strings.Split(content, "\n") {
		// Each line is "hierarchy:controllers:path"; only the path matters.
		path := line
		if parts := strings.SplitN(line, ":", 3); len(parts) == 3 {
			path = parts[2]
		}
		for _, seg := range strings.Split(path, "/") {
			switch {
			case strings.HasSuffix(seg, ".service"):
				unit = seg
			case strings.HasPrefix(seg, "docker-") && strings.HasSuffix(seg, ".scope"):
				// systemd-managed Docker: docker-<64 hex>.scope
				containerID = shortContainerID(strings.TrimSuffix(strings.TrimPrefix(seg, "docker-"), ".scope"))
			case strings.HasPrefix(seg, "cri-containerd-") && strings.HasSuffix(seg, ".scope"):
				containerID = shortContainerID(strings.TrimSuffix(strings.TrimPrefix(seg, "cri-containerd-"), ".scope"))
			case isHexID(seg):
				containerID = shortContainerID(seg)
			}
		}
	}
	// A container's own supervisor scope is not a host service.
	if containerID != "" && strings.HasPrefix(unit, "docker-") {
		unit = ""
	}
	return unit, containerID
}

// isHexID reports whether a cgroup path segment is a bare container id.
func isHexID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// shortContainerID truncates to the 12 characters Docker itself displays.
func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// SocketOwners maps socket inodes to the PID that holds them, by walking
// /proc/<pid>/fd. This is how `ss -tulpn` and `lsof -i` attribute a listening
// port to a process — done here by reading /proc directly, so it needs no
// binary on the host and no shell.
//
// A non-root agent only sees its OWN file descriptors, so the map comes back
// mostly empty and callers must treat a miss as "owner unknown", never as
// "nothing is listening".
func SocketOwners() map[uint64]int {
	owners := map[uint64]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return owners
	}
	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := "/proc/" + e.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // not ours to read, or the process just exited
		}
		for _, fd := range fds {
			if scanned++; scanned > maxFDScan {
				return owners
			}
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			inode, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]"), 10, 64)
			if err != nil {
				continue
			}
			// First writer wins: the listening socket's owner is the process
			// that created it, and forks appear later in pid order.
			if _, exists := owners[inode]; !exists {
				owners[inode] = pid
			}
		}
	}
	return owners
}

// ScanOpenFiles walks /proc/<pid>/fd and returns the regular files matching
// `match`, mapped to the PIDs holding them open. It is how Pulse finds things
// that have no listening port at all — an embedded SQLite database, say.
//
// Like SocketOwners it only sees what the agent is allowed to read, and it
// stops after maxFDScan descriptors so a busy host cannot stall discovery.
func ScanOpenFiles(match func(path string) bool, limit int) map[string][]int {
	found := map[string][]int{}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return found
	}
	scanned := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := "/proc/" + e.Name() + "/fd"
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			if scanned++; scanned > maxFDScan {
				return found
			}
			link, err := os.Readlink(fdDir + "/" + fd.Name())
			if err != nil || !strings.HasPrefix(link, "/") {
				continue // sockets, pipes, anon inodes
			}
			// A deleted file still shows up with a " (deleted)" suffix.
			if strings.HasSuffix(link, " (deleted)") {
				continue
			}
			if !match(link) {
				continue
			}
			if _, known := found[link]; !known && limit > 0 && len(found) >= limit {
				continue
			}
			found[link] = append(found[link], pid)
		}
	}
	return found
}

// ProcessIndex is a pid -> process lookup built from a single scan.
type ProcessIndex map[int]ProcInfo

// IndexProcesses builds a lookup from a process scan.
func IndexProcesses(procs []ProcInfo) ProcessIndex {
	idx := make(ProcessIndex, len(procs))
	for _, p := range procs {
		idx[p.PID] = p
	}
	return idx
}
