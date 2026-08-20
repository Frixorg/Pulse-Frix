package httpx

import (
	"net/http"
	"strings"
)

// domainView is the shape returned for /servers/{id}/domains.
type domainView struct {
	FQDN        string   `json:"fqdn"`
	URL         string   `json:"url"`
	TLS         bool     `json:"tls"` // a matching certificate was found
	SSL         bool     `json:"ssl"` // TLS is configured on the vhost
	TLSDaysLeft int      `json:"tls_days_left,omitempty"`
	NotAfter    string   `json:"tls_expires_at,omitempty"`
	Upstream    string   `json:"upstream,omitempty"`
	Ports       []string `json:"ports,omitempty"`
	Health      string   `json:"health"`
	Source      string   `json:"source"`
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
			// A discovered vhost is actively served by the reverse proxy.
			dv := domainView{FQDN: fqdn, Health: "HEALTHY", Source: vh.Type}
			if ssl, _ := vh.Attributes["ssl"].(bool); ssl {
				dv.SSL = true
			}
			if ups, ok := vh.Attributes["upstreams"].([]any); ok {
				for _, u := range ups {
					if str, ok := u.(string); ok && str != "" {
						dv.Upstream = str
						break
					}
				}
			}
			for _, ln := range toStringSlice(vh.Attributes["listen"]) {
				dv.Ports = append(dv.Ports, ln)
			}
			scheme := "http"
			if dv.SSL {
				scheme = "https"
			}
			dv.URL = scheme + "://" + fqdn
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

// splitServerNames tokenises an nginx server_name value into FQDNs.
func splitServerNames(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '\t'
	})
}

// toStringSlice coerces a JSON array attribute ([]any of strings) to []string.
func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}
