package httpx

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/frix-me/pulse/api/internal/scanner"
	"github.com/frix-me/pulse/api/internal/ssrf"
)

// buildScanInput maps a discovery snapshot into the scanner's input: every
// resource the passive checks reason over, plus the public URLs the active
// checks may probe (validated through the SSRF guard so internal names drop out).
func buildScanInput(serverID string, snap *agentSnapshot) *scanner.Input {
	in := &scanner.Input{ServerID: serverID, Hostname: snap.Hostname}

	for _, r := range snap.Resources {
		res := scanner.Resource{
			Type:       r.Type,
			Name:       r.Name,
			Status:     r.Status,
			Health:     r.Health,
			Attributes: r.Attributes,
		}
		for _, p := range r.Ports {
			res.Ports = append(res.Ports, scanner.Port{
				Host: p.Host, Container: p.Container, Protocol: p.Protocol,
				Address: p.Address, State: p.State,
			})
		}
		in.Resources = append(in.Resources, res)
	}

	// Derive public target URLs from reverse-proxy vhosts. Prefer HTTPS when the
	// vhost terminates TLS or a matching certificate exists.
	certNames := map[string]bool{}
	for _, c := range snap.Resources {
		if c.Type == "tls_certificate" {
			certNames[c.Name] = true
		}
	}
	seen := map[string]bool{}
	const maxTargets = 6
	for _, vh := range snap.Resources {
		switch vh.Type {
		case "nginx_vhost", "caddy_site", "apache_vhost", "traefik_router":
		default:
			continue
		}
		ssl, _ := vh.Attributes["ssl"].(bool)
		for _, fqdn := range splitServerNames(vh.Name) {
			if len(in.Targets) >= maxTargets {
				break
			}
			if fqdn == "" || fqdn == "_" || seen[fqdn] || strings.Contains(fqdn, "*") {
				continue
			}
			seen[fqdn] = true
			scheme := "http"
			if ssl || certNames[fqdn] {
				scheme = "https"
			}
			raw := scheme + "://" + fqdn
			// Only keep public, resolvable targets (SSRF guard is authoritative).
			if _, err := ssrf.Validate(raw); err != nil {
				continue
			}
			in.Targets = append(in.Targets, scanner.Target{URL: raw, FQDN: fqdn, TLS: scheme == "https"})
		}
	}
	return in
}

// handleSecurity returns the check catalogue plus the latest scan for a server.
// If no scan has run yet, it runs the fast passive checks inline so the page is
// never empty on first load.
func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no security data yet")
		return
	}

	audit := scanner.Audit{
		Categories: scanner.Categories(),
		Checks:     scanner.Catalogue(),
	}
	if latest, ok := s.sec.Latest(srv.ServerID); ok {
		audit.Latest = &latest
	} else {
		st := s.sec.RunPassiveSync(buildScanInput(srv.ServerID, snap))
		audit.Latest = &st
	}
	JSON(w, http.StatusOK, audit)
}

type scanRequest struct {
	Mode       string   `json:"mode"`
	Categories []string `json:"categories"`
}

// handleSecurityScanStart launches a scan (passive/active/full, optionally
// filtered to categories) and returns the scan id to poll.
func (s *Server) handleSecurityScanStart(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no security data yet")
		return
	}

	var req scanRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req) // body is optional
	}
	switch req.Mode {
	case scanner.ModePassive, scanner.ModeActive, scanner.ModeFull:
	default:
		req.Mode = scanner.ModeFull
	}

	id := s.sec.StartScan(buildScanInput(srv.ServerID, snap), req.Mode, req.Categories)
	s.audit.Record(p.OrgID, p.Email, "security.scan.start", "success", clientIP(r),
		map[string]any{"mode": req.Mode, "scan_id": id, "server": srv.ID})
	JSON(w, http.StatusAccepted, map[string]string{"scan_id": id})
}

// handleSecurityScanGet returns the live state of a scan (progress, logs,
// findings), scoped to the caller's server.
func (s *Server) handleSecurityScanGet(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	st, ok := s.sec.Get(r.PathValue("scanId"))
	if !ok || st.ServerID != srv.ServerID {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "scan not found")
		return
	}
	JSON(w, http.StatusOK, st)
}
