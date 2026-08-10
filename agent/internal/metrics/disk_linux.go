//go:build linux

package metrics

import "syscall"

// diskUsage returns total and used bytes for the filesystem containing root.
// In cloud mode the host root is mounted read-only at /host (PULSE_ROOTFS).
func diskUsage(root string) (total, used uint64) {
	if root == "" {
		root = "/"
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(root, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	total = st.Blocks * bsize
	free := st.Bavail * bsize
	if total >= free {
		used = total - free
	}
	return total, used
}
