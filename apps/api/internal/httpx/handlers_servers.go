package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/frix-me/pulse/api/internal/model"
)

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	servers, err := s.store.ListServers(p.OrgID)
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not list servers")
		return
	}
	if servers == nil {
		servers = []model.Server{}
	}
	JSON(w, http.StatusOK, Page{Data: servers})
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	JSON(w, http.StatusOK, srv)
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	if err := s.store.DeleteServer(p.OrgID, r.PathValue("id")); err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "server.delete", "success", clientIP(r),
		map[string]any{"server": r.PathValue("id")})
	// Deleting a server removes it from Pulse only; the VPS is untouched.
	JSON(w, http.StatusOK, map[string]string{"status": "removed_from_pulse"})
}

// serverSummary is the overview payload that answers "is this server healthy?".
type serverSummary struct {
	Server     model.Server `json:"server"`
	CPUPercent float64      `json:"cpu_percent"`
	MemUsedPct int          `json:"mem_used_pct"`
	DiskUsePct int          `json:"disk_used_pct"`
	NetRxBytes uint64       `json:"net_rx_bytes"`
	NetTxBytes uint64       `json:"net_tx_bytes"`
	UptimeSec  int64        `json:"uptime_sec"`
	Health     string       `json:"health"`
	Counts     summaryCount `json:"counts"`
}

type summaryCount struct {
	ServicesHealthy    int `json:"services_healthy"`
	ServicesDegraded   int `json:"services_degraded"`
	ServicesDown       int `json:"services_down"`
	ContainersRunning  int `json:"containers_running"`
	ContainersUnhealthy int `json:"containers_unhealthy"`
	DomainsOnline      int `json:"domains_online"`
	DomainsSSLExpiring int `json:"domains_ssl_expiring"`
	AlertsCritical     int `json:"alerts_critical"`
	AlertsWarning      int `json:"alerts_warning"`
}

func (s *Server) handleServerSummary(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	sum := serverSummary{Server: *srv, Health: string(model.HealthUnknown)}

	// Latest metrics sample (if any).
	if raw, err := s.store.GetMetrics(p.OrgID, srv.ServerID); err == nil {
		var sample struct {
			CPUPercent    float64 `json:"cpu_percent"`
			MemUsedBytes  uint64  `json:"mem_used_bytes"`
			MemTotalBytes uint64  `json:"mem_total_bytes"`
			NetRxBytes    uint64  `json:"net_rx_bytes"`
			NetTxBytes    uint64  `json:"net_tx_bytes"`
		}
		if json.Unmarshal(raw, &sample) == nil {
			sum.CPUPercent = sample.CPUPercent
			if sample.MemTotalBytes > 0 {
				sum.MemUsedPct = int(sample.MemUsedBytes * 100 / sample.MemTotalBytes)
			}
			sum.NetRxBytes = sample.NetRxBytes
			sum.NetTxBytes = sample.NetTxBytes
		}
	}

	// Counts from the latest discovery snapshot.
	if snap, err := s.loadSnapshot(p.OrgID, srv.ID); err == nil {
		for _, res := range snap.Resources {
			switch res.Type {
			case "docker_container":
				// Only RUNNING containers count toward health. A container that
				// exited (seeders, one-shot jobs, init tasks) or was created but
				// not started is a normal state — not "unhealthy".
				switch res.Status {
				case "running":
					sum.Counts.ContainersRunning++
					switch res.Health {
					case string(model.HealthDegraded):
						sum.Counts.ServicesDegraded++
					case string(model.HealthDown):
						sum.Counts.ContainersUnhealthy++
						sum.Counts.ServicesDown++
					default:
						sum.Counts.ServicesHealthy++
					}
				case "restarting", "dead":
					sum.Counts.ContainersUnhealthy++
					sum.Counts.ServicesDown++
				}
			case "database", "reverse_proxy", "application", "systemd_unit":
				countService(&sum.Counts, res.Health)
			case "nginx_vhost":
				sum.Counts.DomainsOnline++
				countService(&sum.Counts, res.Health)
			case "caddy_site", "apache_vhost", "traefik_router":
				sum.Counts.DomainsOnline++
			case "filesystem":
				if v, ok := res.Attributes["used_pct"].(float64); ok && int(v) > sum.DiskUsePct {
					sum.DiskUsePct = int(v)
				}
			case "system":
				if v, ok := res.Attributes["uptime_sec"].(float64); ok {
					sum.UptimeSec = int64(v)
				}
			case "tls_certificate":
				if v, ok := res.Attributes["days_left"].(float64); ok && v < 30 {
					sum.Counts.DomainsSSLExpiring++
				}
			}
		}
	}

	// Alert instance counts.
	if insts, err := s.store.ListAlertInstances(p.OrgID); err == nil {
		for _, ai := range insts {
			if ai.State != "firing" || ai.ServerID != srv.ServerID {
				continue
			}
			switch ai.Severity {
			case model.SevCritical:
				sum.Counts.AlertsCritical++
			case model.SevWarning:
				sum.Counts.AlertsWarning++
			}
		}
	}

	// VPS health reflects the machine, not individual workloads. It is driven by
	// whether the agent is reporting (server status) and host resource pressure —
	// NOT by a single stopped container. Per-container/service health is shown on
	// their own pages.
	switch {
	case srv.Status == "" || srv.Status == model.HealthUnknown:
		sum.Health = string(model.HealthUnknown)
	case sum.DiskUsePct >= 95:
		sum.Health = string(model.HealthDown)
	case sum.DiskUsePct >= 85 || sum.Counts.ContainersUnhealthy >= 3:
		sum.Health = string(model.HealthDegraded)
	default:
		sum.Health = string(model.HealthHealthy)
	}

	JSON(w, http.StatusOK, sum)
}

// countService tallies a service-type resource by its health.
func countService(c *summaryCount, health string) {
	switch health {
	case string(model.HealthHealthy):
		c.ServicesHealthy++
	case string(model.HealthDegraded):
		c.ServicesDegraded++
	case string(model.HealthDown):
		c.ServicesDown++
	}
}
