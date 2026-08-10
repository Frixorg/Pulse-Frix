// Package metrics collects system metrics for the VPS the agent runs on.
// It reads /proc and /sys (read-only). All values are best-effort: on a
// non-Linux dev machine the samples are simply zeroed.
package metrics

import (
	"bufio"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sample is a point-in-time system metrics reading.
type Sample struct {
	Timestamp    time.Time `json:"timestamp"`
	CPUPercent   float64   `json:"cpu_percent"`
	CPUUserPct   float64   `json:"cpu_user_pct"`
	CPUSystemPct float64   `json:"cpu_system_pct"`
	CPUIowaitPct float64   `json:"cpu_iowait_pct"`
	Load1        float64   `json:"load1"`
	Load5        float64   `json:"load5"`
	Load15       float64   `json:"load15"`
	MemTotalBytes  uint64 `json:"mem_total_bytes"`
	MemUsedBytes   uint64 `json:"mem_used_bytes"`
	MemAvailBytes  uint64 `json:"mem_avail_bytes"`
	SwapTotal      uint64 `json:"swap_total_bytes"`
	SwapUsed       uint64 `json:"swap_used_bytes"`
	NetRxBytes     uint64 `json:"net_rx_bytes"`
	NetTxBytes     uint64 `json:"net_tx_bytes"`
	DiskTotalBytes uint64 `json:"disk_total_bytes"`
	DiskUsedBytes  uint64 `json:"disk_used_bytes"`
	DiskReadBytes  uint64 `json:"disk_read_bytes"`
	DiskWriteBytes uint64 `json:"disk_write_bytes"`
}

// cpuTimes holds a snapshot of aggregate CPU jiffies from /proc/stat.
type cpuTimes struct {
	user   uint64
	system uint64
	iowait uint64
	idle   uint64
	total  uint64
}

// Collector produces Samples, keeping the previous CPU reading to compute usage.
type Collector struct {
	prev   cpuTimes
	have   bool
	rootfs string // host filesystem prefix for disk stats (e.g. "/host")
}

// NewCollector creates a metrics collector. It reads PULSE_ROOTFS to locate the
// host filesystem for disk usage (defaults to "/").
func NewCollector() *Collector {
	root := os.Getenv("PULSE_ROOTFS")
	if root == "" {
		root = "/"
	}
	return &Collector{rootfs: root}
}

// Sample reads current metrics.
func (c *Collector) Sample() Sample {
	s := Sample{Timestamp: time.Now().UTC()}
	s.CPUPercent, s.CPUUserPct, s.CPUSystemPct, s.CPUIowaitPct = c.cpuBreakdown()
	s.Load1, s.Load5, s.Load15 = loadAvg()
	s.MemTotalBytes, s.MemUsedBytes, s.MemAvailBytes, s.SwapTotal, s.SwapUsed = memInfo()
	s.NetRxBytes, s.NetTxBytes = netTotals()
	s.DiskTotalBytes, s.DiskUsedBytes = diskUsage(c.rootfs)
	s.DiskReadBytes, s.DiskWriteBytes = diskIO()
	return s
}

// cpuBreakdown returns total busy%, and the user / system / iowait shares.
func (c *Collector) cpuBreakdown() (total, user, system, iowait float64) {
	cur, ok := readCPUTimes()
	if !ok {
		return 0, 0, 0, 0
	}
	defer func() { c.prev = cur; c.have = true }()
	if !c.have {
		return 0, 0, 0, 0
	}
	td := float64(cur.total - c.prev.total)
	if td <= 0 {
		return 0, 0, 0, 0
	}
	idleD := float64(cur.idle - c.prev.idle)
	total = round2((1 - idleD/td) * 100)
	user = round2(float64(cur.user-c.prev.user) / td * 100)
	system = round2(float64(cur.system-c.prev.system) / td * 100)
	iowait = round2(float64(cur.iowait-c.prev.iowait) / td * 100)
	return total, user, system, iowait
}

func readCPUTimes() (cpuTimes, bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)[1:]
		vals := make([]uint64, len(fields))
		var total uint64
		for i, fld := range fields {
			v, _ := strconv.ParseUint(fld, 10, 64)
			vals[i] = v
			total += v
		}
		// user nice system idle iowait irq softirq steal
		ct := cpuTimes{total: total}
		if len(vals) > 1 {
			ct.user = vals[0] + vals[1]
		}
		if len(vals) > 2 {
			ct.system = vals[2]
		}
		if len(vals) > 6 {
			ct.system += vals[5] + vals[6]
		}
		if len(vals) > 3 {
			ct.idle = vals[3]
		}
		if len(vals) > 4 {
			ct.iowait = vals[4]
			ct.idle += vals[4]
		}
		return ct, true
	}
	return cpuTimes{}, false
}

func loadAvg() (l1, l5, l15 float64) {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	f := strings.Fields(string(b))
	if len(f) < 3 {
		return 0, 0, 0
	}
	l1, _ = strconv.ParseFloat(f[0], 64)
	l5, _ = strconv.ParseFloat(f[1], 64)
	l15, _ = strconv.ParseFloat(f[2], 64)
	return l1, l5, l15
}

func memInfo() (total, used, avail, swapTotal, swapUsed uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return
	}
	defer f.Close()
	m := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(val)
		if len(fields) == 0 {
			continue
		}
		kb, _ := strconv.ParseUint(fields[0], 10, 64)
		m[key] = kb * 1024
	}
	total = m["MemTotal"]
	avail = m["MemAvailable"]
	if total >= avail {
		used = total - avail
	}
	swapTotal = m["SwapTotal"]
	if swapFree := m["SwapFree"]; swapTotal >= swapFree {
		swapUsed = swapTotal - swapFree
	}
	return
}

func netTotals() (rx, tx uint64) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	i := 0
	for sc.Scan() {
		i++
		if i < 3 {
			continue
		}
		name, rest, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(name) == "lo" {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) < 16 {
			continue
		}
		r, _ := strconv.ParseUint(fields[0], 10, 64)
		t, _ := strconv.ParseUint(fields[8], 10, 64)
		rx += r
		tx += t
	}
	return
}

// diskIO sums cumulative read/write bytes across whole block devices from
// /proc/diskstats (sectors are 512 bytes). Rates are derived from history.
func diskIO() (readBytes, writeBytes uint64) {
	f, err := os.Open("/proc/diskstats")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 {
			continue
		}
		if !isWholeDisk(fields[2]) {
			continue
		}
		rd, _ := strconv.ParseUint(fields[5], 10, 64)
		wr, _ := strconv.ParseUint(fields[9], 10, 64)
		readBytes += rd * 512
		writeBytes += wr * 512
	}
	return readBytes, writeBytes
}

func isWholeDisk(name string) bool {
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") ||
		strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "sr") ||
		strings.HasPrefix(name, "fd") || strings.HasPrefix(name, "md") {
		return false
	}
	if strings.HasPrefix(name, "nvme") {
		return !strings.Contains(name, "p")
	}
	if strings.HasPrefix(name, "sd") || strings.HasPrefix(name, "vd") ||
		strings.HasPrefix(name, "xvd") || strings.HasPrefix(name, "hd") {
		last := name[len(name)-1]
		return last < '0' || last > '9'
	}
	return false
}

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
