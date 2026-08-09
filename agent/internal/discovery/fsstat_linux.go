//go:build linux

package discovery

import "syscall"

// fsStat holds filesystem capacity/inode figures.
type fsStat struct {
	TotalBytes uint64
	FreeBytes  uint64
	UsedBytes  uint64
	TotalInode uint64
	FreeInode  uint64
	UsedInode  uint64
}

// statfs returns capacity and inode usage for the filesystem containing path.
// Read-only; uses the statfs(2) syscall.
func statfs(path string) (fsStat, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(path, &s); err != nil {
		return fsStat{}, err
	}
	bs := uint64(s.Bsize)
	total := s.Blocks * bs
	free := s.Bavail * bs
	fst := fsStat{
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  total - (s.Bfree * bs),
		TotalInode: s.Files,
		FreeInode:  s.Ffree,
		UsedInode:  s.Files - s.Ffree,
	}
	return fst, nil
}

const statfsSupported = true
