package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/frix-me/pulse/api/internal/metricsproxy"
)

// promTemplates maps the core friendly metric names to PromQL for the optional
// Prometheus backend. The full valid set (incl. derived series) is validMetric.
var promTemplates = map[string]string{
	"cpu":     `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",instance="%s"}[5m])) * 100)`,
	"memory":  `(1 - (node_memory_MemAvailable_bytes{instance="%s"} / node_memory_MemTotal_bytes{instance="%s"})) * 100`,
	"disk":    `100 - (node_filesystem_avail_bytes{instance="%s",mountpoint="/"} / node_filesystem_size_bytes{instance="%s",mountpoint="/"} * 100)`,
	"network": `rate(node_network_receive_bytes_total{instance="%s"}[5m])`,
	"load":    `node_load1{instance="%s"}`,
}

// rateFields are cumulative counters — the series is their per-second rate.
var rateFields = map[string]func(sampleShape) float64{
	"network":    func(s sampleShape) float64 { return float64(s.NetRxBytes) },
	"net_in":     func(s sampleShape) float64 { return float64(s.NetRxBytes) },
	"net_out":    func(s sampleShape) float64 { return float64(s.NetTxBytes) },
	"disk_read":  func(s sampleShape) float64 { return float64(s.DiskReadBytes) },
	"disk_write": func(s sampleShape) float64 { return float64(s.DiskWriteBytes) },
}

// sampleShape is the subset of the agent's metrics sample the charts need.
type sampleShape struct {
	Timestamp      time.Time `json:"timestamp"`
	CPUPercent     float64   `json:"cpu_percent"`
	CPUUserPct     float64   `json:"cpu_user_pct"`
	CPUSystemPct   float64   `json:"cpu_system_pct"`
	CPUIowaitPct   float64   `json:"cpu_iowait_pct"`
	Load1          float64   `json:"load1"`
	Load5          float64   `json:"load5"`
	Load15         float64   `json:"load15"`
	MemUsedBytes   uint64    `json:"mem_used_bytes"`
	MemTotalBytes  uint64    `json:"mem_total_bytes"`
	MemAvailBytes  uint64    `json:"mem_avail_bytes"`
	SwapUsed       uint64    `json:"swap_used_bytes"`
	SwapTotal      uint64    `json:"swap_total_bytes"`
	NetRxBytes     uint64    `json:"net_rx_bytes"`
	NetTxBytes     uint64    `json:"net_tx_bytes"`
	DiskUsedBytes  uint64    `json:"disk_used_bytes"`
	DiskTotalBytes uint64    `json:"disk_total_bytes"`
	DiskReadBytes  uint64    `json:"disk_read_bytes"`
	DiskWriteBytes uint64    `json:"disk_write_bytes"`
}

func validMetric(m string) bool {
	if _, ok := rateFields[m]; ok {
		return true
	}
	switch m {
	case "cpu", "cpu_user", "cpu_system", "cpu_iowait",
		"memory", "swap", "disk", "load", "load5", "load15":
		return true
	}
	return false
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}

	metric := r.URL.Query().Get("query")
	if metric == "" {
		metric = "cpu"
	}
	if !validMetric(metric) {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "unknown metric")
		return
	}
	rangeTok := r.URL.Query().Get("range")
	if rangeTok == "" {
		rangeTok = "1h"
	}
	dur, step := metricsproxy.RangeFor(rangeTok)
	end := time.Now()
	start := end.Add(-dur)

	// 1) Prefer the agent's ingested history — works in cloud with no Prometheus.
	if hist := s.historySeries(p.OrgID, srv.ServerID, metric, start); hist != nil {
		JSON(w, http.StatusOK, map[string]any{"series": hist, "degraded": false})
		return
	}
	// 2) Prometheus, if configured and this is a core metric.
	if promQL, ok := buildPromQL(metric, srv.ServerID); ok {
		if series, err := s.metrics.QueryRange(r.Context(), promQL, start, end, step); err == nil && len(series) > 0 {
			JSON(w, http.StatusOK, map[string]any{"series": series, "degraded": false})
			return
		}
	}
	// 3) Last resort: the single latest stored sample.
	if fallback := s.latestSampleSeries(p.OrgID, srv.ServerID, metric); fallback != nil {
		JSON(w, http.StatusOK, map[string]any{"series": fallback, "degraded": true, "note": "no history yet; showing the latest sample"})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"series": []any{}, "degraded": false})
}

func buildPromQL(metric, instance string) (string, bool) {
	tmpl, ok := promTemplates[metric]
	if !ok {
		return "", false
	}
	return strings.ReplaceAll(tmpl, "%s", instance), true
}

// historySeries builds a real time series from the agent's ingested samples.
// Cumulative counters (rateFields) become a per-second rate; the rest are the
// instantaneous values.
func (s *Server) historySeries(orgID, serverID, metric string, since time.Time) []metricsproxy.Series {
	rows, err := s.store.QueryMetricHistory(orgID, serverID, since)
	if err != nil || len(rows) == 0 {
		return nil
	}
	pts := make([]metricsproxy.Point, 0, len(rows))

	if rateFn, isRate := rateFields[metric]; isRate {
		var prevV float64
		var prevT time.Time
		havePrev := false
		for _, row := range rows {
			var smp sampleShape
			if json.Unmarshal(row.Sample, &smp) != nil {
				continue
			}
			cur := rateFn(smp)
			if havePrev {
				if dt := row.TS.Sub(prevT).Seconds(); dt > 0 && cur >= prevV {
					pts = append(pts, metricsproxy.Point{T: float64(row.TS.Unix()), V: roundTo2((cur - prevV) / dt)})
				}
			}
			prevV, prevT, havePrev = cur, row.TS, true
		}
	} else {
		for _, row := range rows {
			var smp sampleShape
			if json.Unmarshal(row.Sample, &smp) != nil {
				continue
			}
			v, ok := instantValue(metric, smp)
			if !ok {
				return nil
			}
			pts = append(pts, metricsproxy.Point{T: float64(row.TS.Unix()), V: roundTo2(v)})
		}
	}
	if len(pts) == 0 {
		return nil
	}
	pts = decimate(pts, 1500)
	return []metricsproxy.Series{{Name: metric, Unit: metricUnit(metric), Points: pts}}
}

func (s *Server) latestSampleSeries(orgID, serverID, metric string) []metricsproxy.Series {
	raw, err := s.store.GetMetrics(orgID, serverID)
	if err != nil {
		return nil
	}
	var smp sampleShape
	if json.Unmarshal(raw, &smp) != nil {
		return nil
	}
	v, ok := instantValue(metric, smp)
	if !ok {
		return nil // rate metrics need at least two points
	}
	t := float64(smp.Timestamp.Unix())
	return []metricsproxy.Series{{
		Name:   metric,
		Unit:   metricUnit(metric),
		Points: []metricsproxy.Point{{T: t, V: roundTo2(v)}},
	}}
}

func pct(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

// instantValue returns the instantaneous value for non-rate metrics.
func instantValue(metric string, s sampleShape) (float64, bool) {
	switch metric {
	case "cpu":
		return s.CPUPercent, true
	case "cpu_user":
		return s.CPUUserPct, true
	case "cpu_system":
		return s.CPUSystemPct, true
	case "cpu_iowait":
		return s.CPUIowaitPct, true
	case "memory":
		return pct(s.MemUsedBytes, s.MemTotalBytes), true
	case "swap":
		return pct(s.SwapUsed, s.SwapTotal), true
	case "disk":
		return pct(s.DiskUsedBytes, s.DiskTotalBytes), true
	case "load":
		return s.Load1, true
	case "load5":
		return s.Load5, true
	case "load15":
		return s.Load15, true
	}
	return 0, false
}

func metricUnit(metric string) string {
	switch metric {
	case "cpu", "cpu_user", "cpu_system", "cpu_iowait", "memory", "swap", "disk":
		return "%"
	case "network", "net_in", "net_out", "disk_read", "disk_write":
		return "B/s"
	default:
		return ""
	}
}

func roundTo2(f float64) float64 { return float64(int64(f*100+0.5)) / 100 }

// decimate reduces points to at most max by even striding, keeping first & last.
func decimate(pts []metricsproxy.Point, max int) []metricsproxy.Point {
	if max <= 0 || len(pts) <= max {
		return pts
	}
	out := make([]metricsproxy.Point, 0, max)
	stride := float64(len(pts)-1) / float64(max-1)
	for i := 0; i < max; i++ {
		idx := int(float64(i)*stride + 0.5)
		if idx >= len(pts) {
			idx = len(pts) - 1
		}
		out = append(out, pts[idx])
	}
	return out
}
