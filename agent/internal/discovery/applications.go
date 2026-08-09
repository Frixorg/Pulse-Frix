package discovery

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// ApplicationDetector recognises common application runtimes/frameworks using
// HEURISTIC signals only (process names). It never modifies application files
// and never surfaces secrets. Signals can be extended with container labels,
// package manifests, and reverse-proxy upstreams. See docs/DISCOVERY.md.
type ApplicationDetector struct{}

func (ApplicationDetector) ID() string      { return "applications" }
func (ApplicationDetector) Name() string    { return "Application Detector" }
func (ApplicationDetector) Version() string { return "1.0" }

func (ApplicationDetector) Available(context.Context) model.Availability {
	if fileExists("/proc/self/comm") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "/proc not present"}
}

// runtimeSignatures maps a process comm substring to a runtime label.
var runtimeSignatures = map[string]string{
	"node":        "Node.js",
	"deno":        "Deno",
	"bun":         "Bun",
	"python":      "Python",
	"gunicorn":    "Python (Gunicorn)",
	"uvicorn":     "Python (Uvicorn/ASGI)",
	"php-fpm":     "PHP",
	"java":        "Java/JVM",
	"ruby":        "Ruby",
	"puma":        "Ruby (Puma)",
	"dotnet":      ".NET",
	"caddy":       "Caddy",
	"gunicorn3":   "Python (Gunicorn)",
}

func (ApplicationDetector) Detect(context.Context) ([]model.Resource, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		comm := readTrim("/proc/" + e.Name() + "/comm")
		if comm == "" {
			continue
		}
		for sig, label := range runtimeSignatures {
			if strings.Contains(comm, sig) {
				counts[label]++
				break
			}
		}
	}

	var out []model.Resource
	for label, n := range counts {
		out = append(out, model.Resource{
			Type:       "application",
			ID:         "app:" + strings.ToLower(strings.ReplaceAll(label, " ", "_")),
			Name:       label,
			Health:     model.StatusHealthy,
			DetectedBy: "applications",
			DetectedAt: time.Now().UTC(),
			Attributes: map[string]any{"process_count": n, "signal": "process"},
		})
	}
	return out, nil
}

func (ApplicationDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}
