package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// DatabaseDetector detects common databases by their listening ports and probes
// reachability with a read-only TCP connection. It does NOT require application
// credentials and never modifies database configuration. Deeper metrics are
// gathered by official read-only exporters (see docs/MONITORING.md).
type DatabaseDetector struct{}

func (DatabaseDetector) ID() string      { return "databases" }
func (DatabaseDetector) Name() string    { return "Database Detector" }
func (DatabaseDetector) Version() string { return "1.0" }

func (DatabaseDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: true}
}

type dbSignature struct {
	engine string
	port   int
}

var knownDatabases = []dbSignature{
	{"postgresql", 5432},
	{"mysql", 3306},
	{"mariadb", 3307},
	{"redis", 6379},
	{"mongodb", 27017},
	{"memcached", 11211},
	{"elasticsearch", 9200},
}

func (DatabaseDetector) Detect(context.Context) ([]model.Resource, error) {
	listeners := ListeningPorts()
	byPort := map[int]Listener{}
	for _, l := range listeners {
		if l.Protocol == "tcp" {
			byPort[l.Port] = l
		}
	}

	var out []model.Resource
	for _, sig := range knownDatabases {
		l, ok := byPort[sig.port]
		if !ok {
			continue
		}
		reachable := tcpProbe(fmt.Sprintf("127.0.0.1:%d", sig.port), 2*time.Second)
		health := model.StatusDown
		if reachable {
			health = model.StatusHealthy
		}
		out = append(out, model.Resource{
			Type:       "database",
			ID:         fmt.Sprintf("db:%s:%d", sig.engine, sig.port),
			Name:       sig.engine,
			Status:     ternaryString(reachable, "reachable", "unreachable"),
			Health:     health,
			DetectedBy: "databases",
			DetectedAt: time.Now().UTC(),
			Ports:      []model.Port{{Host: sig.port, Protocol: "tcp"}},
			Attributes: map[string]any{
				"engine":   sig.engine,
				"exposure": l.Exposure(), // "public" here is a Security-view finding
			},
		})
	}
	return out, nil
}

func (DatabaseDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

func ternaryString(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
