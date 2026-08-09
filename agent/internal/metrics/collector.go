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
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	Load1         float64   `json:"load1"`
	Load5         float64   `json:"load5"`
	Load15        float64   `json:"load15"`
	MemTotalBytes uint64    `json:"mem_total_bytes"`
	MemUsedBytes  uint64    `json:"mem_used_bytes"`
	MemAvailBytes uint64    `json:"mem_avail_bytes"`
	SwapTotal     uint64    `json:"swap_total_bytes"`
	SwapUsed      uint64    `json:"swap_used_bytes"`
	NetRxBytes    uint64    `json:"net_rx_bytes"`
	NetTxBytes    uint64    `json:"net_tx_bytes"`
}

// cpuTimes holds a snapshot of aggregate CPU jiffies from /proc/stat.
type cpuTimes struct {
	idle  uint64
	total uint64
}

// Collector produces Samples, keeping the previous CPU reading to compute usage.
type Collector struct {
	prev cpuTimes
	have bool
}

// NewCollector creates a metrics collector.
func NewCollector() *Collector { return &Collector{} }

// Sample reads current metrics.
func (c *Collector) Sample() Sample {
	s := Sample{Timestamp: time.Now().UTC()}
	s.CPUPercent = c.cpuPercent()
	s.Load1, s.Load5, s.Load15 = loadAvg()
	s.MemTotalBytes, s.MemUsedBytes, s.MemAvailBytes, s.SwapTotal, s.SwapUsed = memInfo()
	s.NetRxBytes, s.NetTxBytes = netTotals()
	return s
}

func (c *Collector) cpuPercent() float64 {
	cur, ok := readCPUTimes()
	if !ok {
		return 0
	}
	defer func() { c.prev = cur; c.have = true }()
	if !c.have {
		return 0
	}
	totalDelta := float64(cur.total - c.prev.total)
	idleDelta := float64(cur.idle - c.prev.idle)
	if totalDelta <= 0 {
		return 0
	}
	return round2((1 - idleDelta/totalDelta) * 100)
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
		var total, idle uint64
		for i, fld := range fields {
			v, _ := strconv.ParseUint(fld, 10, 64)
			total += v
			if i == 3 || i == 4 { // idle + iowait
				idle += v
			}
		}
		return cpuTimes{idle: idle, total: total}, true
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

func round2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }
