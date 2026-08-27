package discovery

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// InitDDetector inventories SysV-style services from /etc/init.d, for hosts
// that predate systemd or run OpenRC.
//
// Running state is inferred from a matching live process — the init scripts
// themselves are NEVER executed, not even with "status". Some distributions'
// scripts have side effects on that path (creating pid directories, rotating
// state), and Pulse does not touch what it inspects.
type InitDDetector struct{}

func (InitDDetector) ID() string      { return "initd" }
func (InitDDetector) Name() string    { return "SysV Init Detector" }
func (InitDDetector) Version() string { return "1.0" }

// initdIgnore are the helper files every distro drops into /etc/init.d that do
// not represent a service.
var initdIgnore = map[string]bool{
	"README": true, "skeleton": true, "rc": true, "rcS": true,
	"functions": true, "rc.local": true, "hwclock.sh": true,
	"single": true, "killprocs": true, "sendsigs": true, "halt": true,
	"reboot": true, "bootmisc.sh": true, "mountall.sh": true,
}

func (InitDDetector) Available(context.Context) model.Availability {
	if hostFileExists("/etc/init.d") {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "/etc/init.d not present"}
}

func (InitDDetector) Detect(context.Context) ([]model.Resource, error) {
	entries, err := os.ReadDir(hostPath("/etc/init.d"))
	if err != nil {
		return nil, err
	}

	// Index live processes by command name so a script can be matched to one.
	live := map[string]ProcInfo{}
	for _, p := range ScanProcesses() {
		if p.Containerised() {
			continue
		}
		if _, seen := live[p.Comm]; !seen {
			live[p.Comm] = p
		}
	}

	var out []model.Resource
	now := time.Now().UTC()
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || initdIgnore[name] || strings.HasPrefix(name, ".") ||
			strings.HasSuffix(name, ".dpkg-dist") || strings.HasSuffix(name, ".bak") {
			continue
		}
		if info, err := e.Info(); err == nil && !isExecutable(info) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		proc, running := matchInitService(name, live)
		status, health := "stopped", model.StatusUnknown
		attrs := map[string]any{
			"script":   displayPath(filepath.Join(hostPath("/etc/init.d"), name)),
			"workload": "host",
		}
		if running {
			status, health = "running", model.StatusHealthy
			attrs["pid"] = proc.PID
			attrs["process"] = proc.Comm
		}
		out = append(out, model.Resource{
			Type:       "initd_service",
			ID:         "initd:" + name,
			Name:       name,
			Status:     status,
			Health:     health,
			DetectedBy: "initd",
			DetectedAt: now,
			Attributes: attrs,
		})
	}

	out = append(out, model.Resource{
		Type:       "init_system",
		ID:         "init:sysv",
		Name:       "sysvinit",
		Health:     model.StatusHealthy,
		DetectedBy: "initd",
		DetectedAt: now,
		Attributes: map[string]any{"scripts": len(names)},
	})
	return out, nil
}

func (InitDDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// matchInitService looks for a live process matching an init script's name.
// Script names and process names diverge often enough (apache2 -> apache2,
// ssh -> sshd, mysql -> mysqld) that a couple of suffix variants are tried.
func matchInitService(script string, live map[string]ProcInfo) (ProcInfo, bool) {
	base := strings.TrimSuffix(script, ".sh")
	for _, candidate := range []string{base, base + "d", strings.TrimSuffix(base, "d")} {
		if candidate == "" {
			continue
		}
		if p, ok := live[candidate]; ok {
			return p, true
		}
	}
	return ProcInfo{}, false
}

// isExecutable reports whether any execute bit is set.
func isExecutable(info fs.FileInfo) bool {
	return info.Mode().Perm()&0o111 != 0
}
