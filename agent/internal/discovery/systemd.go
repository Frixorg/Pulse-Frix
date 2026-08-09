package discovery

import (
	"context"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// SystemdDetector detects systemd and reports failed units. It uses systemctl
// with a FIXED argument vector (no user input, no shell) purely for read-only
// state that /proc does not expose. If systemctl is unavailable it still reports
// systemd presence and degrades gracefully.
type SystemdDetector struct{}

func (SystemdDetector) ID() string      { return "systemd" }
func (SystemdDetector) Name() string    { return "Systemd Detector" }
func (SystemdDetector) Version() string { return "1.0" }

func (SystemdDetector) Available(context.Context) model.Availability {
	if fileExists("/run/systemd/system") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "systemd not running"}
}

func (SystemdDetector) Detect(context.Context) ([]model.Resource, error) {
	var out []model.Resource

	// Read-only: list failed units. Args are constant; no injection surface.
	failed := 0
	if _, ok := lookPath("systemctl"); ok {
		outStr, err := runFixed("systemctl", "list-units", "--type=service",
			"--state=failed", "--no-legend", "--no-pager", "--plain")
		if err == nil {
			for _, line := range strings.Split(strings.TrimSpace(outStr), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 0 {
					continue
				}
				unit := fields[0]
				failed++
				out = append(out, model.Resource{
					Type:       "systemd_unit",
					ID:         "systemd:" + unit,
					Name:       unit,
					Status:     "failed",
					Health:     model.StatusDown,
					DetectedBy: "systemd",
					DetectedAt: time.Now().UTC(),
				})
			}
		}
	}

	out = append(out, model.Resource{
		Type:       "init_system",
		ID:         "init:systemd",
		Name:       "systemd",
		Health:     ternaryStatus(failed == 0, model.StatusHealthy, model.StatusDegraded),
		DetectedBy: "systemd",
		DetectedAt: time.Now().UTC(),
		Attributes: map[string]any{"failed_units": failed},
	})
	return out, nil
}

func (SystemdDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

func ternaryStatus(cond bool, a, b model.Status) model.Status {
	if cond {
		return a
	}
	return b
}
