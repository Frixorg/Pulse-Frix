//go:build !linux

package discovery

import "errors"

// fsStat holds filesystem capacity/inode figures.
type fsStat struct {
	TotalBytes uint64
	FreeBytes  uint64
	UsedBytes  uint64
	TotalInode uint64
	FreeInode  uint64
	UsedInode  uint64
}

// statfs is unsupported off Linux; the filesystem detector degrades gracefully.
// The agent targets Linux; this stub only exists so the code builds and its
// tests run on developer machines (macOS/Windows).
func statfs(string) (fsStat, error) { return fsStat{}, errors.New("statfs unsupported on this OS") }

const statfsSupported = false
