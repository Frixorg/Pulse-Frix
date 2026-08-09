package discovery

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// ProcessDetector summarises running processes: total count, zombies, and the
// top CPU/memory consumers. Reads /proc/<pid>/stat and /proc/<pid>/status
// (read-only). Command lines are redacted before leaving the agent.
type ProcessDetector struct{}

func (ProcessDetector) ID() string      { return "process" }
func (ProcessDetector) Name() string    { return "Process Detector" }
func (ProcessDetector) Version() string { return "1.0" }

func (ProcessDetector) Available(context.Context) model.Availability {
	if fileExists("/proc/self/stat") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "/proc not present"}
}

type procInfo struct {
	PID    int
	Comm   string
	State  string
	RSSKiB uint64
}

func (ProcessDetector) Detect(context.Context) ([]model.Resource, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	pageSize := uint64(4096)
	var procs []procInfo
	zombies := 0
	total := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		stat := readTrim("/proc/" + e.Name() + "/stat")
		if stat == "" {
			continue
		}
		total++
		// comm is within parentheses; fields after ')' are space separated.
		openIdx := strings.IndexByte(stat, '(')
		closeIdx := strings.LastIndexByte(stat, ')')
		if openIdx < 0 || closeIdx < 0 || closeIdx < openIdx {
			continue
		}
		comm := stat[openIdx+1 : closeIdx]
		rest := strings.Fields(stat[closeIdx+1:])
		if len(rest) < 22 {
			continue
		}
		state := rest[0]
		if state == "Z" {
			zombies++
		}
		rssPages, _ := strconv.ParseUint(rest[21], 10, 64)
		procs = append(procs, procInfo{PID: pid, Comm: comm, State: state, RSSKiB: rssPages * pageSize / 1024})
	}

	// Top memory consumers.
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSKiB > procs[j].RSSKiB })
	top := procs
	if len(top) > 10 {
		top = top[:10]
	}
	topList := make([]map[string]any, 0, len(top))
	for _, p := range top {
		topList = append(topList, map[string]any{
			"pid":     p.PID,
			"name":    p.Comm,
			"rss_kib": p.RSSKiB,
			"state":   p.State,
		})
	}

	r := model.Resource{
		Type:       "process_summary",
		ID:         "processes",
		Name:       "processes",
		Health:     model.StatusHealthy,
		DetectedBy: "process",
		DetectedAt: time.Now().UTC(),
		Attributes: map[string]any{
			"total":       total,
			"zombies":     zombies,
			"top_by_memory": topList,
		},
	}
	if zombies > 0 {
		r.Health = model.StatusDegraded
	}
	return []model.Resource{r}, nil
}

func (ProcessDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}
