package httpx

import (
	"net/http"
)

// domainView is the shape returned for /servers/{id}/domains.
type domainView struct {
	FQDN        string `json:"fqdn"`
	TLS         bool   `json:"tls"`
	TLSDaysLeft int    `json:"tls_days_left,omitempty"`
	NotAfter    string `json:"tls_expires_at,omitempty"`
	Health      string `json:"health"`
	Source      string `json:"source"`
}

func (s *Server) handleDomains(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no domains discovered yet")
		return
	}

	// Index TLS certificates by common name for enrichment.
	certByName := map[string]agentResource{}
	for _, c := range s.resourcesOfType(snap, "tls_certificate") {
		certByName[c.Name] = c
	}

	var out []domainView
	seen := map[string]bool{}
	for _, vh := range s.resourcesOfType(snap, "nginx_vhost", "caddy_site", "apache_vhost", "traefik_router") {
		for _, fqdn := range splitServerNames(vh.Name) {
			if fqdn == "" || fqdn == "_" || seen[fqdn] {
				continue
			}
			seen[fqdn] = true
			dv := domainView{FQDN: fqdn, Health: "UNKNOWN", Source: vh.Type}
			if cert, ok := certByName[fqdn]; ok {
				dv.TLS = true
				dv.Health = cert.Health
				if v, ok := cert.Attributes["days_left"].(float64); ok {
					dv.TLSDaysLeft = int(v)
				}
				if v, ok := cert.Attributes["not_after"].(string); ok {
					dv.NotAfter = v
				}
			}
			out = append(out, dv)
		}
	}
	JSON(w, http.StatusOK, Page{Data: out})
}

// securityFinding is a read-only risk observation. Pulse never auto-remediates.
type securityFinding struct {
	Severity       string `json:"severity"`
	Title          string `json:"title"`
	Detail         string `json:"detail"`
	Recommendation string `json:"recommendation"`
}

func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no security data yet")
		return
	}

	var findings []securityFinding

	// Publicly-exposed databases.
	for _, db := range s.resourcesOfType(snap, "database") {
		if expo, _ := db.Attributes["exposure"].(string); expo == "public" {
			findings = append(findings, securityFinding{
				Severity:       "CRITICAL",
				Title:          db.Name + " is publicly reachable",
				Detail:         db.Name + " is listening on all interfaces.",
				Recommendation: "Restrict access to trusted networks or bind to loopback.",
			})
		}
	}

	// Docker daemon exposed on a TCP port.
	for _, port := range s.resourcesOfType(snap, "listening_port") {
		if len(port.Ports) == 0 {
			continue
		}
		pp := port.Ports[0]
		if expo, _ := port.Attributes["exposure"].(string); expo == "public" {
			if pp.Host == 2375 || pp.Host == 2376 {
				findings = append(findings, securityFinding{
					Severity:       "CRITICAL",
					Title:          "Docker daemon exposed",
					Detail:         "The Docker API is listening publicly — this is root-equivalent access.",
					Recommendation: "Never expose the Docker socket/port publicly.",
				})
			}
		}
	}

	// Expiring / expired TLS.
	for _, cert := range s.resourcesOfType(snap, "tls_certificate") {
		days, _ := cert.Attributes["days_left"].(float64)
		if days < 0 {
			findings = append(findings, securityFinding{
				Severity: "CRITICAL", Title: "Expired TLS certificate: " + cert.Name,
				Detail: "The certificate has expired.", Recommendation: "Renew the certificate.",
			})
		} else if days < 30 {
			findings = append(findings, securityFinding{
				Severity: "WARNING", Title: "TLS certificate expiring soon: " + cert.Name,
				Detail: "Fewer than 30 days remain.", Recommendation: "Renew the certificate before expiry.",
			})
		}
	}

	if findings == nil {
		findings = []securityFinding{}
	}
	JSON(w, http.StatusOK, Page{Data: findings})
}

func splitServerNames(s string) []string {
	var out []string
	cur := ""
	for _, c := range s {
		if c == ' ' || c == ',' || c == '\t' {
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
			continue
		}
		cur += string(c)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}
