package discovery

import (
	"context"
	"sort"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// ProcessDetector summarises running processes: total count, zombies, the split
// between host-native and containerised workloads, and the top memory
// consumers. It reads /proc only (see procscan.go). Command lines are redacted
// before leaving the agent.
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

func (ProcessDetector) Detect(context.Context) ([]model.Resource, error) {
	procs := ScanProcesses()

	zombies, containerised := 0, 0
	for _, p := range procs {
		if p.State == "Z" {
			zombies++
		}
		if p.Containerised() {
			containerised++
		}
	}

	// Top memory consumers.
	sort.Slice(procs, func(i, j int) bool { return procs[i].RSSKiB > procs[j].RSSKiB })
	top := procs
	if len(top) > 10 {
		top = top[:10]
	}
	topList := make([]map[string]any, 0, len(top))
	for _, p := range top {
		entry := map[string]any{
			"pid":     p.PID,
			"name":    p.Comm,
			"rss_kib": p.RSSKiB,
			"state":   p.State,
		}
		if p.Unit != "" {
			entry["unit"] = p.Unit
		}
		if p.ContainerID != "" {
			entry["container_id"] = p.ContainerID
		}
		topList = append(topList, entry)
	}

	r := model.Resource{
		Type:       "process_summary",
		ID:         "processes",
		Name:       "processes",
		Health:     model.StatusHealthy,
		DetectedBy: "process",
		DetectedAt: time.Now().UTC(),
		Attributes: map[string]any{
			"total":         len(procs),
			"zombies":       zombies,
			"containerised": containerised,
			"host_native":   len(procs) - containerised,
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
