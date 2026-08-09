// Package model defines the structured data types produced by the discovery
// engine and shared across the agent. Everything here is designed to be
// serialised to JSON and is *always* redacted before it leaves the agent.
package model

import "time"

// Status is a generic service/resource health state.
type Status string

const (
	StatusHealthy  Status = "HEALTHY"
	StatusDegraded Status = "DEGRADED"
	StatusDown     Status = "DOWN"
	StatusUnknown  Status = "UNKNOWN"
)

// Availability reports whether a detector can run in the current environment.
// A detector that is unavailable degrades gracefully instead of failing the run.
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// Port describes a network port associated with a resource.
type Port struct {
	Host      int    `json:"host,omitempty"`
	Container int    `json:"container,omitempty"`
	Protocol  string `json:"protocol"`
	Address   string `json:"address,omitempty"`
	State     string `json:"state,omitempty"`
}

// Resource is a single discovered thing (container, service, mount, domain...).
type Resource struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status,omitempty"`
	Health     Status            `json:"health,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Attributes map[string]any    `json:"attributes,omitempty"`
	Ports      []Port            `json:"ports,omitempty"`
	Networks   []string          `json:"networks,omitempty"`
	Volumes    []string          `json:"volumes,omitempty"`
	DependsOn  []string          `json:"depends_on,omitempty"`
	DetectedBy string            `json:"detected_by"`
	DetectedAt time.Time         `json:"detected_at"`
}

// Check is a single health probe result.
type Check struct {
	Name   string `json:"name"`
	Status Status `json:"status"`
	Detail string `json:"detail,omitempty"`
}

// HealthReport aggregates checks into an overall status.
type HealthReport struct {
	Status Status  `json:"status"`
	Checks []Check `json:"checks,omitempty"`
}

// DetectorResult captures the outcome of running one detector.
type DetectorResult struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Version    string       `json:"version"`
	Available  bool         `json:"available"`
	Reason     string       `json:"reason,omitempty"`
	Count      int          `json:"count"`
	DurationMS int64        `json:"duration_ms"`
	Error      string       `json:"error,omitempty"`
	Health     HealthReport `json:"health"`
}

// TopoNode / TopoEdge form the discovered infrastructure graph.
type TopoNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Type   string `json:"type"`
	Health Status `json:"health,omitempty"`
}

type TopoEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Source string `json:"source"` // nginx_upstream | docker_network | port | compose
}

type Topology struct {
	Nodes []TopoNode `json:"nodes"`
	Edges []TopoEdge `json:"edges"`
}

// Snapshot is the full, redacted discovery output for one run.
type Snapshot struct {
	InstallationID string           `json:"installation_id"`
	ServerID       string           `json:"server_id"`
	Hostname       string           `json:"hostname"`
	GeneratedAt    time.Time        `json:"generated_at"`
	DurationMS     int64            `json:"duration_ms"`
	Detectors      []DetectorResult `json:"detectors"`
	Resources      []Resource       `json:"resources"`
	Topology       *Topology        `json:"topology,omitempty"`
}

// CountByType returns a map of resource type -> count, useful for summaries.
func (s *Snapshot) CountByType() map[string]int {
	out := map[string]int{}
	for _, r := range s.Resources {
		out[r.Type]++
	}
	return out
}
