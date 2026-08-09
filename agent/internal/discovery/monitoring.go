package discovery

import (
	"context"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// MonitoringDetector detects EXISTING monitoring software already present on the
// VPS (so Pulse can coexist and never replace it). Detection is by well-known
// listening ports; Pulse never touches these services.
type MonitoringDetector struct{}

func (MonitoringDetector) ID() string      { return "existing_monitoring" }
func (MonitoringDetector) Name() string    { return "Existing Monitoring Detector" }
func (MonitoringDetector) Version() string { return "1.0" }

func (MonitoringDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: true}
}

type monSig struct {
	name string
	port int
}

var knownMonitoring = []monSig{
	{"prometheus", 9090},
	{"grafana", 3000},
	{"node_exporter", 9100},
	{"cadvisor", 8080},
	{"alertmanager", 9093},
	{"loki", 3100},
	{"influxdb", 8086},
	{"netdata", 19999},
	{"uptime-kuma", 3001},
}

func (MonitoringDetector) Detect(context.Context) ([]model.Resource, error) {
	byPort := map[int]bool{}
	for _, l := range ListeningPorts() {
		byPort[l.Port] = true
	}
	var out []model.Resource
	for _, s := range knownMonitoring {
		if !byPort[s.port] {
			continue
		}
		out = append(out, model.Resource{
			Type:       "existing_monitoring",
			ID:         "monitoring:" + s.name,
			Name:       s.name,
			Health:     model.StatusHealthy,
			DetectedBy: "existing_monitoring",
			DetectedAt: time.Now().UTC(),
			Ports:      []model.Port{{Host: s.port, Protocol: "tcp"}},
			Attributes: map[string]any{"note": "pre-existing; Pulse coexists and never replaces it"},
		})
	}
	return out, nil
}

func (MonitoringDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}
