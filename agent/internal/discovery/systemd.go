package discovery

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// SystemdDetector inventories the host's systemd services: which are RUNNING
// and which have FAILED.
//
// It has two independent sources and prefers whichever is available:
//
//  1. systemctl, called with a FIXED argument vector (no user input, no shell,
//     no injection surface) purely for read-only state.
//  2. /proc/<pid>/cgroup, which names the unit that owns every process. This is
//     what makes the detector work for a containerised agent, where systemctl
//     does not exist but `pid: host` still exposes the host's processes.
//
// Neither path can start, stop or reconfigure a unit.
type SystemdDetector struct{}

func (SystemdDetector) ID() string      { return "systemd" }
func (SystemdDetector) Name() string    { return "Systemd Detector" }
func (SystemdDetector) Version() string { return "1.1" }

// maxReportedUnits bounds the snapshot on hosts with unusually many services.
const maxReportedUnits = 200

func (SystemdDetector) Available(context.Context) model.Availability {
	if fileExists("/run/systemd/system") || hostFileExists("/run/systemd/system") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "systemd not running"}
}

func (SystemdDetector) Detect(context.Context) ([]model.Resource, error) {
	var out []model.Resource
	now := time.Now().UTC()

	running, source := runningUnits()
	failed := failedUnits()

	for _, unit := range failed {
		out = append(out, model.Resource{
			Type:       "systemd_unit",
			ID:         "systemd:" + unit,
			Name:       unit,
			Status:     "failed",
			Health:     model.StatusDown,
			DetectedBy: "systemd",
			DetectedAt: now,
			Attributes: map[string]any{"state": "failed", "workload": "host"},
		})
	}

	failedSet := map[string]bool{}
	for _, u := range failed {
		failedSet[u] = true
	}
	for _, u := range running {
		if failedSet[u.Name] {
			continue
		}
		attrs := map[string]any{
			"state":    "running",
			"source":   source,
			"workload": "host",
		}
		if u.MainPID > 0 {
			attrs["pid"] = u.MainPID
		}
		if u.Description != "" {
			attrs["description"] = u.Description
		}
		out = append(out, model.Resource{
			Type:       "systemd_unit",
			ID:         "systemd:" + u.Name,
			Name:       u.Name,
			Status:     "running",
			Health:     model.StatusHealthy,
			DetectedBy: "systemd",
			DetectedAt: now,
			Attributes: attrs,
		})
	}

	out = append(out, model.Resource{
		Type:       "init_system",
		ID:         "init:systemd",
		Name:       "systemd",
		Health:     ternaryStatus(len(failed) == 0, model.StatusHealthy, model.StatusDegraded),
		DetectedBy: "systemd",
		DetectedAt: now,
		Attributes: map[string]any{
			"failed_units":  len(failed),
			"running_units": len(running),
			"source":        source,
		},
	})
	return out, nil
}

func (SystemdDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// systemdUnit is one running service.
type systemdUnit struct {
	Name        string
	Description string
	MainPID     int
}

// runningUnits lists running services, reporting which source answered so the
// dashboard can explain a partial result.
func runningUnits() ([]systemdUnit, string) {
	if units := runningUnitsFromSystemctl(); len(units) > 0 {
		return units, "systemctl"
	}
	return runningUnitsFromProc(), "proc"
}

// runningUnitsFromSystemctl runs systemctl with a constant argument vector —
// no shell, no interpolation. On a host where an unprivileged agent cannot
// query systemd, and only when the operator opted in, it retries once through
// `sudo -n` (see privileged.go).
func runningUnitsFromSystemctl() []systemdUnit {
	if _, ok := lookPath("systemctl"); !ok {
		return nil
	}
	outStr, err := runFixedPrivileged("systemctl", "list-units", "--type=service",
		"--state=running", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return nil
	}
	var units []systemdUnit
	for _, line := range strings.Split(strings.TrimSpace(outStr), "\n") {
		// UNIT LOAD ACTIVE SUB DESCRIPTION...
		fields := strings.Fields(line)
		if len(fields) < 4 || !strings.HasSuffix(fields[0], ".service") {
			continue
		}
		u := systemdUnit{Name: fields[0]}
		if len(fields) > 4 {
			u.Description = strings.Join(fields[4:], " ")
		}
		units = append(units, u)
		if len(units) >= maxReportedUnits {
			break
		}
	}
	return units
}

// runningUnitsFromProc derives the set of active units from the cgroup of every
// visible process. Units owning at least one live process are running by
// definition, which is all the dashboard needs.
func runningUnitsFromProc() []systemdUnit {
	lowestPID := map[string]int{}
	for _, p := range ScanProcesses() {
		// A process inside a container belongs to the container's unit, not to
		// a host service, so it must not be counted as one.
		if p.Unit == "" || p.Containerised() {
			continue
		}
		if cur, ok := lowestPID[p.Unit]; !ok || p.PID < cur {
			lowestPID[p.Unit] = p.PID
		}
	}
	units := make([]systemdUnit, 0, len(lowestPID))
	for name, pid := range lowestPID {
		units = append(units, systemdUnit{Name: name, MainPID: pid})
	}
	sort.Slice(units, func(i, j int) bool { return units[i].Name < units[j].Name })
	if len(units) > maxReportedUnits {
		units = units[:maxReportedUnits]
	}
	return units
}

// failedUnits lists services in the failed state. There is no /proc equivalent,
// so this is empty when systemctl is unavailable.
func failedUnits() []string {
	if _, ok := lookPath("systemctl"); !ok {
		return nil
	}
	outStr, err := runFixedPrivileged("systemctl", "list-units", "--type=service",
		"--state=failed", "--no-legend", "--no-pager", "--plain")
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(outStr), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		out = append(out, fields[0])
	}
	return out
}

func ternaryStatus(cond bool, a, b model.Status) model.Status {
	if cond {
		return a
	}
	return b
}
