//go:build !linux

package metrics

// diskUsage is a no-op on non-Linux platforms (dev machines); samples zero.
func diskUsage(string) (total, used uint64) { return 0, 0 }
