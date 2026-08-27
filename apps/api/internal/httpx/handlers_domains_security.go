package httpx

import (
	"net/http"
	"regexp"
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

	// add records one FQDN once, enriched from the vhost that published it and
	// from any matching certificate.
	add := func(fqdn, source string, attrs map[string]any) {
		if fqdn == "" || fqdn == "_" || seen[fqdn] {
			return
		}
		seen[fqdn] = true
		// A discovered vhost is actively served by the reverse proxy.
		dv := domainView{FQDN: fqdn, Health: "HEALTHY", Source: source}
		if ssl, _ := attrs["ssl"].(bool); ssl {
			dv.SSL = true
		}
		for _, u := range toStringSlice(attrs["upstreams"]) {
			dv.Upstream = u
			break
		}
		dv.Ports = append(dv.Ports, toStringSlice(attrs["listen"])...)
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

	// Reverse proxies that name the domain in the vhost itself: Nginx, Apache,
	// Caddy, and Traefik's file provider.
	for _, vh := range s.resourcesOfType(snap, "nginx_vhost", "caddy_site", "apache_vhost", "traefik_router") {
		for _, fqdn := range splitServerNames(vh.Name) {
			add(fqdn, vh.Type, vh.Attributes)
		}
		// Apache ServerAlias names are domains in their own right.
		for _, alias := range toStringSlice(vh.Attributes["aliases"]) {
			add(alias, vh.Type, vh.Attributes)
		}
	}

	// HAProxy routes by header rather than by vhost name, so the domains live
	// in the frontend's host ACLs.
	for _, f := range s.resourcesOfType(snap, "haproxy_frontend") {
		for _, fqdn := range toStringSlice(f.Attributes["hosts"]) {
			add(fqdn, "haproxy_frontend", f.Attributes)
		}
	}

	// Traefik is most often configured entirely through Docker labels, which
	// never reach a config file for the agent to parse — so the routers are
	// derived from the container inventory here instead.
	for _, c := range s.resourcesOfType(snap, "docker_container") {
		for k, v := range c.Labels {
			if !strings.HasPrefix(k, "traefik.") || !strings.Contains(k, ".rule") {
				continue
			}
			for _, fqdn := range traefikHostsFromRule(v) {
				add(fqdn, "traefik_router", map[string]any{
					"ssl":       strings.Contains(k, "websecure") || containerHasTLSLabel(c.Labels),
					"upstreams": []any{c.Name},
				})
			}
		}
	}

	if out == nil {
		out = []domainView{}
	}
	JSON(w, http.StatusOK, Page{Data: out})
}

// traefikHostRule matches Host(`a.example.com`, `b.example.com`) in a Traefik
// router rule; traefikQuoted then pulls each name out of the argument list.
var (
	traefikHostRule = regexp.MustCompile("Host\\(([^)]*)\\)")
	traefikQuoted   = regexp.MustCompile("[`\"']([^`\"']+)[`\"']")
)

// traefikHostsFromRule extracts every hostname a Traefik rule matches on.
func traefikHostsFromRule(rule string) []string {
	var out []string
	for _, m := range traefikHostRule.FindAllStringSubmatch(rule, -1) {
		for _, q := range traefikQuoted.FindAllStringSubmatch(m[1], -1) {
			if host := strings.TrimSpace(q[1]); host != "" {
				out = append(out, host)
			}
		}
	}
	return out
}

// containerHasTLSLabel reports whether any Traefik label on the container turns
// TLS on for its router.
func containerHasTLSLabel(labels map[string]string) bool {
	for k, v := range labels {
		if strings.HasPrefix(k, "traefik.") &&
			(strings.Contains(k, ".tls") || strings.Contains(k, "certresolver")) &&
			v != "false" {
			return true
		}
	}
	return false
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
