package discovery

import (
	"context"
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

// realFSTypes are the mount types we care about (skip virtual/pseudo fs).
var realFSTypes = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true,
	"zfs": true, "f2fs": true, "reiserfs": true, "jfs": true, "vfat": true,
	"ntfs": true, "overlay": true,
}

func (d FilesystemDetector) Detect(context.Context) ([]model.Resource, error) {
	var out []model.Resource
	seen := map[string]bool{}
	for _, line := range readLines("/proc/mounts") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		device, mount, fstype := fields[0], fields[1], fields[2]
		if !realFSTypes[fstype] {
			continue
		}
		if seen[mount] {
			continue
		}
		seen[mount] = true

		st, err := statfs(mount)
		if err != nil || st.TotalBytes == 0 {
			continue
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
