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
				if res.Status == "running" {
					sum.Counts.ContainersRunning++
				}
				if res.Health == string(model.HealthDown) {
					sum.Counts.ContainersUnhealthy++
				}
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
			switch res.Health {
			case string(model.HealthHealthy):
				if isServiceType(res.Type) {
					sum.Counts.ServicesHealthy++
				}
			case string(model.HealthDegraded):
				if isServiceType(res.Type) {
					sum.Counts.ServicesDegraded++
				}
			case string(model.HealthDown):
				if isServiceType(res.Type) {
					sum.Counts.ServicesDown++
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

	sum.Health = string(srv.Status)
	if sum.Counts.ServicesDown > 0 || sum.DiskUsePct >= 95 {
		sum.Health = string(model.HealthDown)
	} else if sum.Counts.ServicesDegraded > 0 || sum.DiskUsePct >= 85 {
		sum.Health = string(model.HealthDegraded)
	} else if srv.Status == "" {
		sum.Health = string(model.HealthUnknown)
	}

	JSON(w, http.StatusOK, sum)
}

func isServiceType(t string) bool {
	switch t {
	case "docker_container", "database", "nginx_vhost", "reverse_proxy", "application", "systemd_unit":
		return true
	}
	return false
}
