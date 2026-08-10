package httpx

import (
	"fmt"
	"net/http"
	"strings"
	"time"
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

// securityFinding is a read-only risk observation. Pulse never auto-remediates.
type securityFinding struct {
	ID             string `json:"id"`
	Category       string `json:"category"`
	Severity       string `json:"severity"` // CRITICAL | WARNING | INFO
	Title          string `json:"title"`
	Resource       string `json:"resource,omitempty"`
	Detail         string `json:"detail"`
	Recommendation string `json:"recommendation"`
}

// securityCheck is one category in the audit catalogue with its outcome.
type securityCheck struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"` // pass | issues | not_assessed
	Count  int    `json:"count"`
	Note   string `json:"note,omitempty"`
}

func (s *Server) handleSecurity(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	snap, err := s.loadSnapshot(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "no security data yet")
		return
	}

	var findings []securityFinding
	issues := map[string]int{}
	flag := func(cat string, f securityFinding) {
		f.Category = cat
		findings = append(findings, f)
		issues[cat]++
	}

	// Localhost isolation — databases reachable on all interfaces.
	for _, db := range s.resourcesOfType(snap, "database") {
		if expo, _ := db.Attributes["exposure"].(string); expo == "public" {
			flag("localhost-isolation", securityFinding{
				ID: "db-public-" + db.Name, Severity: "CRITICAL",
				Title:          db.Name + " is reachable from outside the host",
				Resource:       db.Name,
				Detail:         "This database listens on all interfaces (0.0.0.0), so anything that can reach the host can attempt to connect.",
				Recommendation: "Bind it to 127.0.0.1 or an internal Docker network and reach it through your app — never publish the DB port.",
			})
		}
	}

	// Port exposure + Docker daemon exposure.
	for _, port := range s.resourcesOfType(snap, "listening_port") {
		if len(port.Ports) == 0 {
			continue
		}
		if expo, _ := port.Attributes["exposure"].(string); expo != "public" {
			continue
		}
		host := port.Ports[0].Host
		switch host {
		case 2375, 2376:
			flag("docker-exposure", securityFinding{
				ID: "docker-daemon", Severity: "CRITICAL",
				Title:          "Docker daemon exposed on the network",
				Resource:       fmt.Sprintf("tcp/%d", host),
				Detail:         "The Docker API is listening publicly — this is root-equivalent access to the whole host.",
				Recommendation: "Never expose the Docker socket/port. Use SSH or a read-only socket proxy.",
			})
		default:
			if host != 80 && host != 443 && host != 22 && host != 0 {
				flag("port-exposure", securityFinding{
					ID: fmt.Sprintf("port-%d", host), Severity: "WARNING",
					Title:          fmt.Sprintf("Port %d is exposed publicly", host),
					Resource:       fmt.Sprintf("%s · tcp/%d", port.Name, host),
					Detail:         "This port is listening on all interfaces. If it isn't meant to be public, it widens your attack surface.",
					Recommendation: "Bind it to loopback, or restrict it with a firewall / security group.",
				})
			}
		}
	}

	// TLS validity.
	for _, cert := range s.resourcesOfType(snap, "tls_certificate") {
		days, _ := cert.Attributes["days_left"].(float64)
		if days < 0 {
			flag("tls-validity", securityFinding{
				ID: "tls-expired-" + cert.Name, Severity: "CRITICAL",
				Title: "Expired TLS certificate: " + cert.Name, Resource: cert.Name,
				Detail: "The certificate has expired; clients will reject the connection.",
				Recommendation: "Renew it (e.g. certbot renew) and reload the reverse proxy.",
			})
		} else if days < 30 {
			flag("tls-validity", securityFinding{
				ID: "tls-expiring-" + cert.Name, Severity: "WARNING",
				Title: "TLS certificate expiring soon: " + cert.Name, Resource: cert.Name,
				Detail:         fmt.Sprintf("Fewer than 30 days remain (%d).", int(days)),
				Recommendation: "Confirm auto-renewal is working, or renew now.",
			})
		}
	}

	// Base image hygiene — unpinned :latest / untagged images.
	unpinned := 0
	for _, c := range s.resourcesOfType(snap, "docker_container") {
		img, _ := c.Attributes["image"].(string)
		if img == "" {
			continue
		}
		if strings.HasSuffix(img, ":latest") || !strings.Contains(img, ":") {
			unpinned++
			if unpinned <= 6 {
				flag("base-image", securityFinding{
					ID: "img-" + c.Name, Severity: "INFO",
					Title: "Unpinned image: " + img, Resource: c.Name,
					Detail:         "Using :latest (or no tag) means the image can change silently and may carry unpatched CVEs.",
					Recommendation: "Pin a specific version tag and scan images (e.g. trivy) in CI.",
				})
			}
		}
	}
	if unpinned > 6 {
		flag("base-image", securityFinding{
			ID: "img-more", Severity: "INFO",
			Title:          fmt.Sprintf("%d containers run unpinned images", unpinned),
			Detail:         "Several containers use :latest or untagged images.",
			Recommendation: "Pin versions and add image scanning to your build.",
		})
	}

	// Resource boundaries — running containers with no memory limit.
	noLimit := 0
	for _, c := range s.resourcesOfType(snap, "docker_container") {
		if c.Status != "running" {
			continue
		}
		if lim, _ := c.Attributes["memory_limit"].(float64); lim == 0 {
			noLimit++
		}
	}
	if noLimit > 0 {
		flag("resource-boundaries", securityFinding{
			ID: "no-mem-limit", Severity: "INFO",
			Title:          fmt.Sprintf("%d running containers have no memory limit", noLimit),
			Detail:         "Without a limit, one container can exhaust host memory (a noisy-neighbour / DoS risk).",
			Recommendation: "Set a memory limit per service (mem_limit / deploy.resources.limits).",
		})
	}

	// --- SSH hardening / ciphers / blank passwords (from ssh_config) ---
	for _, cfg := range s.resourcesOfType(snap, "ssh_config") {
		if v, _ := cfg.Attributes["permit_root_login"].(string); v == "yes" {
			flag("ssh-hardening", securityFinding{
				ID: "ssh-root", Severity: "WARNING",
				Title: "Root SSH login is permitted", Resource: "sshd_config",
				Detail:         "PermitRootLogin is 'yes' — remote root logins widen the blast radius of a compromised key or password.",
				Recommendation: "Set PermitRootLogin prohibit-password (or no) and use a sudo user.",
			})
		}
		if v, _ := cfg.Attributes["password_authentication"].(string); v == "yes" {
			flag("ssh-hardening", securityFinding{
				ID: "ssh-passauth", Severity: "INFO",
				Title: "SSH password authentication is enabled", Resource: "sshd_config",
				Detail:         "PasswordAuthentication 'yes' allows brute-forceable password logins.",
				Recommendation: "Prefer key-based auth: set PasswordAuthentication no.",
			})
		}
		if v, _ := cfg.Attributes["permit_empty_passwords"].(string); v == "yes" {
			flag("blank-passwords", securityFinding{
				ID: "ssh-emptypass", Severity: "CRITICAL",
				Title: "SSH permits empty passwords", Resource: "sshd_config",
				Detail:         "PermitEmptyPasswords 'yes' lets accounts with no password log in.",
				Recommendation: "Set PermitEmptyPasswords no immediately.",
			})
		}
		weak := append(append(toStringSlice(cfg.Attributes["weak_ciphers"]), toStringSlice(cfg.Attributes["weak_macs"])...), toStringSlice(cfg.Attributes["weak_kex"])...)
		if len(weak) > 0 {
			flag("cipher-suite", securityFinding{
				ID: "ssh-weakcrypto", Severity: "WARNING",
				Title: "Weak SSH ciphers/MACs/KEX enabled", Resource: "sshd_config",
				Detail:         "The SSH daemon offers weak algorithms: " + strings.Join(weak, ", ") + ".",
				Recommendation: "Restrict Ciphers/MACs/KexAlgorithms to modern suites (chacha20-poly1305, aes-gcm, curve25519).",
			})
		}
	}

	// --- Shared SSH keys ---
	for _, k := range s.resourcesOfType(snap, "ssh_keys") {
		if shared, _ := k.Attributes["shared"].(bool); shared {
			flag("shared-ssh-keys", securityFinding{
				ID: "shared-keys", Severity: "WARNING",
				Title: "One SSH key is authorised for multiple users", Resource: "authorized_keys",
				Detail:         "A shared key means access can't be attributed or revoked per-person (" + strings.Join(toStringSlice(k.Attributes["shared_keys"]), "; ") + ").",
				Recommendation: "Give each person their own key and remove shared entries.",
			})
		}
	}

	// --- Container privileges / IPC / credentials ---
	for _, c := range s.resourcesOfType(snap, "docker_container") {
		if priv, _ := c.Attributes["privileged"].(bool); priv {
			flag("privileged-flag", securityFinding{
				ID: "priv-" + c.Name, Severity: "CRITICAL",
				Title: c.Name + " runs in privileged mode", Resource: c.Name,
				Detail:         "A privileged container can access all host devices and effectively escape isolation.",
				Recommendation: "Drop --privileged; grant only the specific capabilities the container needs.",
			})
		}
		if ipc, _ := c.Attributes["ipc_mode"].(string); ipc == "host" {
			flag("shared-memory", securityFinding{
				ID: "ipc-" + c.Name, Severity: "WARNING",
				Title: c.Name + " shares the host IPC namespace", Resource: c.Name,
				Detail:         "--ipc=host exposes host shared memory to the container (and vice-versa).",
				Recommendation: "Remove --ipc=host unless the workload genuinely needs host shared memory.",
			})
		}
		if blank, _ := c.Attributes["blank_password"].(bool); blank {
			flag("blank-passwords", securityFinding{
				ID: "blank-" + c.Name, Severity: "CRITICAL",
				Title: c.Name + " has a blank password in its environment", Resource: c.Name,
				Detail:         "A password environment variable is empty — the service may accept no password.",
				Recommendation: "Set a strong password (and prefer secrets over env vars).",
			})
		}
		if creds := toStringSlice(c.Attributes["weak_credentials"]); len(creds) > 0 {
			flag("default-credentials", securityFinding{
				ID: "weakcred-" + c.Name, Severity: "WARNING",
				Title: c.Name + " uses a weak/default credential", Resource: c.Name,
				Detail:         "These password variables are set to a well-known default value: " + strings.Join(creds, ", ") + ".",
				Recommendation: "Rotate to strong, unique secrets.",
			})
		}
	}

	// --- Nginx security headers / info leakage / rate limiting (TLS vhosts) ---
	missingHeaders, noRateLimit, tokensOn := 0, 0, 0
	for _, vh := range s.resourcesOfType(snap, "nginx_vhost") {
		if ssl, _ := vh.Attributes["ssl"].(bool); !ssl {
			continue
		}
		hsts, _ := vh.Attributes["has_hsts"].(bool)
		xframe, _ := vh.Attributes["has_xframe"].(bool)
		csp, _ := vh.Attributes["has_csp"].(bool)
		if !hsts || !xframe || !csp {
			missingHeaders++
			if missingHeaders <= 6 {
				var missing []string
				if !hsts {
					missing = append(missing, "HSTS")
				}
				if !xframe {
					missing = append(missing, "X-Frame-Options")
				}
				if !csp {
					missing = append(missing, "Content-Security-Policy")
				}
				flag("security-headers", securityFinding{
					ID: "hdr-" + vh.Name, Severity: "INFO",
					Title: "Missing security headers on " + vh.Name, Resource: vh.Name,
					Detail:         vh.Name + " does not set: " + strings.Join(missing, ", ") + ".",
					Recommendation: "Add the missing add_header directives (HSTS, X-Frame-Options, CSP).",
				})
			}
		}
		if tokensOff, _ := vh.Attributes["server_tokens_off"].(bool); !tokensOff {
			tokensOn++
		}
		if rl, _ := vh.Attributes["has_rate_limit"].(bool); !rl {
			noRateLimit++
		}
	}
	if tokensOn > 0 {
		flag("information-leakage", securityFinding{
			ID: "server-tokens", Severity: "INFO",
			Title:          "Nginx exposes its version",
			Detail:         fmt.Sprintf("%d vhost(s) don't set 'server_tokens off', so responses leak the nginx version.", tokensOn),
			Recommendation: "Add 'server_tokens off;' in the http{} block.",
		})
	}
	if noRateLimit > 0 {
		flag("rate-limiting", securityFinding{
			ID: "no-ratelimit", Severity: "INFO",
			Title:          "No rate limiting on public vhosts",
			Detail:         fmt.Sprintf("%d TLS vhost(s) have no limit_req — login/API endpoints are open to brute-force and abuse.", noRateLimit),
			Recommendation: "Define a limit_req zone and apply it to sensitive locations.",
		})
	}

	if findings == nil {
		findings = []securityFinding{}
	}
	allChecks := buildSecurityChecks(issues)

	// Re-run a single check: return just that check and its findings.
	if only := r.URL.Query().Get("check"); only != "" {
		fs := []securityFinding{}
		for _, f := range findings {
			if f.Category == only {
				fs = append(fs, f)
			}
		}
		cks := []securityCheck{}
		for _, c := range allChecks {
			if c.ID == only {
				cks = append(cks, c)
			}
		}
		JSON(w, http.StatusOK, map[string]any{
			"generated_at": time.Now().UTC(),
			"checks":       cks,
			"findings":     fs,
		})
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"generated_at": time.Now().UTC(),
		"checks":       allChecks,
		"findings":     findings,
	})
}

// buildSecurityChecks returns the full audit catalogue. Assessed categories carry
// a pass/issues status; the rest are marked not-assessed until deeper host
// inspection lands in the agent.
func buildSecurityChecks(issues map[string]int) []securityCheck {
	assessed := [][2]string{
		{"localhost-isolation", "Localhost Isolation"},
		{"port-exposure", "Port Exposure"},
		{"docker-exposure", "Docker Daemon Exposure"},
		{"tls-validity", "TLS / Certificate Validity"},
		{"base-image", "Base Image Vulnerabilities"},
		{"resource-boundaries", "Resource Boundaries"},
		{"ssh-hardening", "SSH Hardening"},
		{"privileged-flag", "Privileged Flag"},
		{"blank-passwords", "Blank Passwords"},
		{"default-credentials", "Default Credentials"},
		{"shared-ssh-keys", "Shared SSH Keys"},
		{"shared-memory", "Shared Memory Restrictions"},
		{"cipher-suite", "Cipher Suite Hardening"},
		{"security-headers", "Security Header Injection"},
		{"information-leakage", "Information Leakage"},
		{"rate-limiting", "Rate Limiting"},
	}
	out := make([]securityCheck, 0, len(assessed))
	for _, c := range assessed {
		st := "pass"
		if issues[c[0]] > 0 {
			st = "issues"
		}
		out = append(out, securityCheck{ID: c[0], Name: c[1], Status: st, Count: issues[c[0]]})
	}
	return out
}

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
