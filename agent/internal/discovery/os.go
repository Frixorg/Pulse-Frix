package discovery

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// OSDetector reports the operating system, kernel, architecture and uptime.
// Always available; reads /etc/os-release and /proc (read-only).
type OSDetector struct{}

func (OSDetector) ID() string      { return "os" }
func (OSDetector) Name() string    { return "OS Detector" }
func (OSDetector) Version() string { return "1.0" }

func (OSDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: true}
}

func (OSDetector) Detect(context.Context) ([]model.Resource, error) {
	hostname, _ := os.Hostname()
	osName, osVersion := parseOSRelease()
	kernel := readTrim("/proc/sys/kernel/osrelease")
	if kernel == "" {
		kernel = runtime.GOOS
	}
	uptime := parseUptime()

	r := model.Resource{
		Type:       "system",
		ID:         "system:" + hostname,
		Name:       hostname,
		Health:     model.StatusHealthy,
		DetectedBy: "os",
		DetectedAt: time.Now().UTC(),
		Attributes: map[string]any{
			"os":           osName,
			"os_version":   osVersion,
			"kernel":       kernel,
			"architecture": runtime.GOARCH,
			"hostname":     hostname,
			"cpu_cores":    runtime.NumCPU(),
			"uptime_sec":   uptime,
		},
	}
	return []model.Resource{r}, nil
}

func (OSDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

func parseOSRelease() (name, version string) {
	for _, line := range readLines("/etc/os-release") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		v = strings.Trim(v, `"`)
		switch k {
		case "NAME":
			name = v
		case "VERSION":
			version = v
		case "PRETTY_NAME":
			if name == "" {
				name = v
			}
		}
	}
	if name == "" {
		name = runtime.GOOS
	}
	return name, version
}

func parseUptime() int64 {
	fields := strings.Fields(readTrim("/proc/uptime"))
	if len(fields) == 0 {
		return 0
	}
	f, _ := strconv.ParseFloat(fields[0], 64)
	return int64(f)
}
