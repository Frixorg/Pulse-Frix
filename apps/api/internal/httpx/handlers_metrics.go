package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/frix-me/pulse/api/internal/metricsproxy"
)

// promTemplates maps friendly metric names to PromQL, parameterised by the
// server's instance label. The dashboard never sends PromQL (spec section 12).
var promTemplates = map[string]string{
	"cpu":     `100 - (avg by (instance) (rate(node_cpu_seconds_total{mode="idle",instance="%s"}[5m])) * 100)`,
	"memory":  `(1 - (node_memory_MemAvailable_bytes{instance="%s"} / node_memory_MemTotal_bytes{instance="%s"})) * 100`,
	"disk":    `100 - (node_filesystem_avail_bytes{instance="%s",mountpoint="/"} / node_filesystem_size_bytes{instance="%s",mountpoint="/"} * 100)`,
	"network": `rate(node_network_receive_bytes_total{instance="%s"}[5m])`,
	"load":    `node_load1{instance="%s"}`,
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
	rangeTok := r.URL.Query().Get("range")
	if rangeTok == "" {
		rangeTok = "1h"
	}
	dur, step := metricsproxy.RangeFor(rangeTok)
	end := time.Now()
	start := end.Add(-dur)

	promQL, ok := buildPromQL(metric, srv.ServerID)
	if !ok {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "unknown metric")
		return
	}

	series, err := s.metrics.QueryRange(r.Context(), promQL, start, end, step)
	if err != nil {
		// Graceful degradation: fall back to the latest stored sample so the UI
		// still shows *something*, never a 500. Only real data is returned.
		if fallback := s.latestSampleSeries(p.OrgID, srv.ServerID, metric); fallback != nil {
			JSON(w, http.StatusOK, map[string]any{
				"series":   fallback,
				"degraded": true,
				"note":     "metrics backend unavailable; showing latest sample",
			})
			return
		}
		JSON(w, http.StatusOK, map[string]any{"series": []any{}, "degraded": true})
		return
	}
	JSON(w, http.StatusOK, map[string]any{"series": series, "degraded": false})
}

func buildPromQL(metric, instance string) (string, bool) {
	tmpl, ok := promTemplates[metric]
	if !ok {
		return "", false
	}
	// Substitute the instance label into every %s placeholder.
	return strings.ReplaceAll(tmpl, "%s", instance), true
}

func (s *Server) latestSampleSeries(orgID, serverID, metric string) []metricsproxy.Series {
	raw, err := s.store.GetMetrics(orgID, serverID)
	if err != nil {
		return nil
	}
	var sample struct {
		Timestamp    time.Time `json:"timestamp"`
		CPUPercent   float64   `json:"cpu_percent"`
		Load1        float64   `json:"load1"`
		MemUsedBytes uint64    `json:"mem_used_bytes"`
		MemTotal     uint64    `json:"mem_total_bytes"`
		NetRxBytes   uint64    `json:"net_rx_bytes"`
	}
	if json.Unmarshal(raw, &sample) != nil {
		return nil
	}
	t := float64(sample.Timestamp.Unix())
	var v float64
	switch metric {
	case "cpu":
		v = sample.CPUPercent
	case "memory":
		if sample.MemTotal > 0 {
			v = float64(sample.MemUsedBytes) / float64(sample.MemTotal) * 100
		}
	case "load":
		v = sample.Load1
	case "network":
		v = float64(sample.NetRxBytes)
	}
	return []metricsproxy.Series{{Name: metric, Points: []metricsproxy.Point{{T: t, V: v}}}}
}
