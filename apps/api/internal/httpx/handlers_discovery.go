package httpx

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// The API consumes the agent's redacted discovery snapshot (see
// agent/internal/model). These types mirror that JSON so the API can derive
// service/container/domain/security views without a shared Go module.

type agentPort struct {
	Host      int    `json:"host,omitempty"`
	Container int    `json:"container,omitempty"`
	Protocol  string `json:"protocol"`
	Address   string `json:"address,omitempty"`
	State     string `json:"state,omitempty"`
}

type agentResource struct {
	Type       string            `json:"type"`
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Status     string            `json:"status,omitempty"`
	Health     string            `json:"health,omitempty"`
	Labels     map[string]string `json:"labels,omitempty"`
	Attributes map[string]any    `json:"attributes,omitempty"`
	Ports      []agentPort       `json:"ports,omitempty"`
	Networks   []string          `json:"networks,omitempty"`
	Volumes    []string          `json:"volumes,omitempty"`
	DependsOn  []string          `json:"depends_on,omitempty"`
	DetectedBy string            `json:"detected_by"`
}

type agentTopology struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
}

type agentSnapshot struct {
	InstallationID string          `json:"installation_id"`
	ServerID       string          `json:"server_id"`
	Hostname       string          `json:"hostname"`
	GeneratedAt    time.Time       `json:"generated_at"`
	DurationMS     int64           `json:"duration_ms"`
	Resources      []agentResource `json:"resources"`
	Topology       *agentTopology  `json:"topology,omitempty"`
}

// loadSnapshot fetches and parses the latest discovery snapshot for a server,
// resolving the internal server row to its public server_id first (org-scoped).
func (s *Server) loadSnapshot(orgID, serverRowID string) (*agentSnapshot, error) {
	srv, err := s.store.GetServer(orgID, serverRowID)
	if err != nil {
		return nil, err
	}
	raw, err := s.store.GetDiscovery(orgID, srv.ServerID)
	if err != nil {
		return nil, err
	}
	var snap agentSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (s *Server) resourcesOfType(snap *agentSnapshot, types ...string) []agentResource {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	var out []agentResource
	for _, r := range snap.Resources {
		if want[r.Type] {
			out = append(out, r)
		}
	}
	return out
}

func (s *Server) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no discovery data for this server yet")
		return
	}
	JSON(w, http.StatusOK, snap)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no topology data yet")
		return
	}
	if snap.Topology == nil {
		JSON(w, http.StatusOK, agentTopology{Nodes: []map[string]any{}, Edges: []map[string]any{}})
		return
	}
	JSON(w, http.StatusOK, snap.Topology)
}

func (s *Server) handleServices(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no services discovered yet")
		return
	}
	services := s.resourcesOfType(snap, "docker_container", "database", "nginx_vhost",
		"apache_vhost", "caddy_site", "traefik_router", "haproxy_frontend",
		"reverse_proxy", "application", "systemd_unit", "initd_service", "existing_monitoring")
	JSON(w, http.StatusOK, Page{Data: services})
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no containers discovered yet")
		return
	}
	JSON(w, http.StatusOK, Page{Data: s.resourcesOfType(snap, "docker_container")})
}

func (s *Server) handleDatabases(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no databases discovered yet")
		return
	}
	out := s.resourcesOfType(snap, "database")
	seen := map[string]bool{}
	for _, d := range out {
		seen[strings.ToLower(d.Name)] = true
	}
	// Also derive databases from Docker container images — this covers
	// containerised engines on any (or no published) port, which the
	// host-port detector alone misses.
	for _, c := range s.resourcesOfType(snap, "docker_container") {
		image, _ := c.Attributes["image"].(string)
		engine := dbEngineFromImage(image)
		if engine == "" || seen[strings.ToLower(c.Name)] {
			continue
		}
		seen[strings.ToLower(c.Name)] = true
		attrs := map[string]any{"engine": engine, "image": image, "container": c.Name, "exposure": "container"}
		if len(c.Ports) > 0 {
			attrs["port"] = c.Ports[0].Host
		}
		out = append(out, agentResource{
			Type: "database", ID: "dbc:" + c.Name, Name: c.Name,
			Status: c.Status, Health: c.Health, DetectedBy: "docker",
			Ports: c.Ports, Attributes: attrs,
		})
	}
	if out == nil {
		out = []agentResource{}
	}
	JSON(w, http.StatusOK, Page{Data: out})
}

// dbEngineFromImage maps a Docker image reference to a database engine, or "".
func dbEngineFromImage(image string) string {
	img := strings.ToLower(image)
	switch {
	case strings.Contains(img, "postgres"), strings.Contains(img, "pgvector"),
		strings.Contains(img, "timescale"), strings.Contains(img, "postgis"):
		return "postgresql"
	case strings.Contains(img, "mariadb"):
		return "mariadb"
	case strings.Contains(img, "mysql"), strings.Contains(img, "percona"):
		return "mysql"
	case strings.Contains(img, "valkey"), strings.Contains(img, "keydb"), strings.Contains(img, "redis"):
		return "redis"
	case strings.Contains(img, "mongo"):
		return "mongodb"
	case strings.Contains(img, "memcached"):
		return "memcached"
	case strings.Contains(img, "opensearch"), strings.Contains(img, "elasticsearch"):
		return "elasticsearch"
	case strings.Contains(img, "clickhouse"):
		return "clickhouse"
	case strings.Contains(img, "cockroach"):
		return "cockroachdb"
	case strings.Contains(img, "scylla"), strings.Contains(img, "cassandra"):
		return "cassandra"
	case strings.Contains(img, "influxdb"):
		return "influxdb"
	case strings.Contains(img, "rabbitmq"):
		return "rabbitmq"
	case strings.Contains(img, "neo4j"):
		return "neo4j"
	case strings.Contains(img, "couchdb"):
		return "couchdb"
	case strings.Contains(img, "surrealdb"):
		return "surrealdb"
	}
	return ""
}

func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no applications discovered yet")
		return
	}
	JSON(w, http.StatusOK, Page{Data: s.resourcesOfType(snap, "application")})
}
