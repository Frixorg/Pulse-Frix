package discovery

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// FilesystemDetector reports mounted filesystems with capacity and inode usage.
// Reads /proc/mounts and calls statfs(2); read-only.
type FilesystemDetector struct{}

func (FilesystemDetector) ID() string      { return "filesystem" }
func (FilesystemDetector) Name() string    { return "Filesystem Detector" }
func (FilesystemDetector) Version() string { return "1.0" }

func (FilesystemDetector) Available(context.Context) model.Availability {
	if !statfsSupported {
		return model.Availability{Available: false, Reason: "statfs unsupported on this OS"}
	}
	if !fileExists("/proc/mounts") {
		return model.Availability{Available: false, Reason: "/proc/mounts not present"}
	}
	return model.Availability{Available: true}
}

// realFSTypes are the real block-backed filesystems we report. Pseudo/virtual
// filesystems and container overlays are intentionally excluded so the Storage
// view lists actual disks, not bind mounts or per-container roots.
var realFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"zfs": true, "f2fs": true, "reiserfs": true, "jfs": true, "vfat": true,
	"ntfs": true,
}

// skipMountPrefixes are locations that are never a "disk" worth reporting:
// kernel pseudo-fs and Docker/Kubernetes per-container overlay mounts.
var skipMountPrefixes = []string{
	"/proc", "/sys", "/dev", "/run",
	"/var/lib/docker/", "/var/lib/kubelet/", "/var/lib/containers/",
	"/snap/", "/host/proc", "/host/sys", "/host/dev", "/host/run",
}

func skipMount(mount string) bool {
	for _, p := range skipMountPrefixes {
		if mount == p || strings.HasPrefix(mount, p) {
			return true
		}
	}
	return false
}

func (d FilesystemDetector) Detect(context.Context) ([]model.Resource, error) {
	// When containerised, read the HOST mount table under PULSE_ROOTFS and stat
	// the real mountpoints through that prefix — otherwise we'd only see the
	// agent container's own bind mounts (all pointing at the same device).
	root := strings.TrimRight(os.Getenv("PULSE_ROOTFS"), "/")
	mountsFile := "/proc/mounts"
	if root != "" && fileExists(root+"/proc/mounts") {
		mountsFile = root + "/proc/mounts"
	}

	var out []model.Resource
	seenMount := map[string]bool{}
	seenDevice := map[string]bool{}
	for _, line := range readLines(mountsFile) {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device, mount, fstype := fields[0], fields[1], fields[2]
		if !realFSTypes[fstype] || skipMount(mount) {
			continue
		}
		// Collapse duplicate views of the same filesystem (bind mounts, subvols).
		if seenMount[mount] || (strings.HasPrefix(device, "/dev/") && seenDevice[device]) {
			continue
		}

		statPath := mount
		if root != "" {
			statPath = filepath.Join(root, mount)
		}
		st, err := statfs(statPath)
		if err != nil || st.TotalBytes == 0 {
			continue
		}
		seenMount[mount] = true
		if strings.HasPrefix(device, "/dev/") {
			seenDevice[device] = true
		}
		usedPct := pct(st.UsedBytes, st.TotalBytes)
		inodePct := pct(st.UsedInode, st.TotalInode)

		health := model.StatusHealthy
		if usedPct >= 95 || inodePct >= 95 {
			health = model.StatusDown
		} else if usedPct >= 85 || inodePct >= 85 {
			health = model.StatusDegraded
		}

		out = append(out, model.Resource{
			Type:       "filesystem",
			ID:         "mount:" + mount,
			Name:       mount,
			Health:     health,
			DetectedBy: "filesystem",
			DetectedAt: time.Now().UTC(),
			Attributes: map[string]any{
				"device":      device,
				"fstype":      fstype,
				"total_bytes": st.TotalBytes,
				"free_bytes":  st.FreeBytes,
				"used_bytes":  st.UsedBytes,
				"used_pct":    usedPct,
				"inode_total": st.TotalInode,
				"inode_used":  st.UsedInode,
				"inode_pct":   inodePct,
			},
		})
	}
	return out, nil
}

func (FilesystemDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

func pct(used, total uint64) int {
	if total == 0 {
		return 0
	}
	return int((used * 100) / total)
}
