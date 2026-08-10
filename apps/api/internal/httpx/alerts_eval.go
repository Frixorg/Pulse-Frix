package httpx

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/frix-me/pulse/api/internal/model"
)

// alertEngine holds transient alert-evaluation state (breach timers + currently
// firing instances). It is in-memory: instances re-derive on the next poll after
// a restart, which is acceptable for v1. Evaluation is driven lazily by the
// dashboard polling the instances endpoint.
type alertEngine struct {
	mu     sync.Mutex
	breach map[string]time.Time                      // "org|rule|server" -> first breach time
	firing map[string]map[string]model.AlertInstance // org -> dedupKey -> instance
}

func newAlertEngine() *alertEngine {
	return &alertEngine{
		breach: map[string]time.Time{},
		firing: map[string]map[string]model.AlertInstance{},
	}
}

// ruleCond is a parsed alert-rule condition.
type ruleCond struct {
	kind      string // "metric" | "container_down"
	metric    string // cpu|memory|disk|load
	op        string // ">" | "<"
	threshold float64
	target    string // container name (container_down)
}

// parseRule parses the compact expr stored on a rule:
//
//	metric rule:    "cpu > 80" | "memory > 90" | "disk > 85" | "load > 4"
//	container rule: "container_down <name>"
func parseRule(expr string) (ruleCond, bool) {
	f := strings.Fields(strings.TrimSpace(expr))
	if len(f) == 2 && f[0] == "container_down" {
		return ruleCond{kind: "container_down", target: f[1]}, true
	}
	if len(f) == 3 && (f[1] == ">" || f[1] == "<") {
		metric := strings.ToLower(f[0])
		switch metric {
		case "cpu", "memory", "disk", "load":
		default:
			return ruleCond{}, false
		}
		th, err := strconv.ParseFloat(f[2], 64)
		if err != nil {
			return ruleCond{}, false
		}
		return ruleCond{kind: "metric", metric: metric, op: f[1], threshold: th}, true
	}
	return ruleCond{}, false
}

// evaluateAlerts re-evaluates every enabled rule for the org against the latest
// data and updates the in-memory firing set. Called before listing instances.
func (s *Server) evaluateAlerts(orgID string) {
	rules, err := s.store.ListAlerts(orgID)
	if err != nil {
		return
	}
	servers, err := s.store.ListServers(orgID)
	if err != nil {
		return
	}
	now := time.Now().UTC()

	s.alerts.mu.Lock()
	defer s.alerts.mu.Unlock()
	if s.alerts.firing[orgID] == nil {
		s.alerts.firing[orgID] = map[string]model.AlertInstance{}
	}
	active := map[string]bool{}

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		cond, ok := parseRule(rule.Expr)
		if !ok {
			continue
		}
		for _, srv := range servers {
			breached, detail := s.checkRule(orgID, srv, cond)
			key := orgID + "|" + rule.ID + "|" + srv.ServerID
			dedup := rule.ID + ":" + srv.ServerID
			if !breached {
				delete(s.alerts.breach, key)
				continue
			}
			start, seen := s.alerts.breach[key]
			if !seen {
				start = now
				s.alerts.breach[key] = now
			}
			if now.Sub(start) < time.Duration(rule.ForSeconds)*time.Second {
				continue // breaching, but not for long enough yet
			}
			active[dedup] = true
			inst, exists := s.alerts.firing[orgID][dedup]
			if !exists {
				inst = model.AlertInstance{
					ID: "ali_" + dedup, OrgID: orgID, AlertID: rule.ID, Name: rule.Name,
					Severity: rule.Severity, ServerID: srv.ServerID, State: "firing",
					DedupKey: dedup, StartedAt: now,
				}
			}
			inst.RootCause = detail
			s.alerts.firing[orgID][dedup] = inst
		}
	}

	// Anything not active any more has recovered — drop it.
	for dedup := range s.alerts.firing[orgID] {
		if !active[dedup] {
			delete(s.alerts.firing[orgID], dedup)
		}
	}
}

// firingInstances returns a snapshot of the org's currently-firing alerts.
func (s *Server) firingInstances(orgID string) []model.AlertInstance {
	s.alerts.mu.Lock()
	defer s.alerts.mu.Unlock()
	out := make([]model.AlertInstance, 0, len(s.alerts.firing[orgID]))
	for _, inst := range s.alerts.firing[orgID] {
		out = append(out, inst)
	}
	return out
}

func (s *Server) checkRule(orgID string, srv model.Server, c ruleCond) (bool, string) {
	switch c.kind {
	case "metric":
		raw, err := s.store.GetMetrics(orgID, srv.ServerID)
		if err != nil {
			return false, ""
		}
		val, ok := metricValueFromSample(raw, c.metric)
		if !ok {
			return false, ""
		}
		if (c.op == ">" && val > c.threshold) || (c.op == "<" && val < c.threshold) {
			return true, fmt.Sprintf("%s is %.1f (threshold %s %.0f) on %s", c.metric, val, c.op, c.threshold, serverLabel(srv))
		}
		return false, ""
	case "container_down":
		snap, err := s.loadSnapshot(orgID, srv.ID)
		if err != nil {
			return false, ""
		}
		for _, res := range snap.Resources {
			if res.Type == "docker_container" && res.Name == c.target {
				if res.Status != "running" {
					return true, fmt.Sprintf("container %s is %s on %s", c.target, res.Status, serverLabel(srv))
				}
				return false, ""
			}
		}
		return false, ""
	}
	return false, ""
}

func metricValueFromSample(raw json.RawMessage, metric string) (float64, bool) {
	var smp struct {
		CPUPercent     float64 `json:"cpu_percent"`
		Load1          float64 `json:"load1"`
		MemUsedBytes   uint64  `json:"mem_used_bytes"`
		MemTotalBytes  uint64  `json:"mem_total_bytes"`
		DiskUsedBytes  uint64  `json:"disk_used_bytes"`
		DiskTotalBytes uint64  `json:"disk_total_bytes"`
	}
	if json.Unmarshal(raw, &smp) != nil {
		return 0, false
	}
	switch metric {
	case "cpu":
		return smp.CPUPercent, true
	case "load":
		return smp.Load1, true
	case "memory":
		if smp.MemTotalBytes > 0 {
			return float64(smp.MemUsedBytes) / float64(smp.MemTotalBytes) * 100, true
		}
		return 0, true
	case "disk":
		if smp.DiskTotalBytes > 0 {
			return float64(smp.DiskUsedBytes) / float64(smp.DiskTotalBytes) * 100, true
		}
		return 0, true
	}
	return 0, false
}

func serverLabel(srv model.Server) string {
	if srv.Hostname != "" {
		return srv.Hostname
	}
	return srv.ServerID
}
