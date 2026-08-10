package discovery

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// NginxDetector reads and ANALYSES Nginx configuration. It NEVER overwrites
// nginx.conf, sites-enabled/*, or sites-available/*. It extracts virtual hosts,
// server_names, listen ports, TLS certificate paths, and proxy upstreams to
// drive domain monitoring and the topology graph. See docs/DISCOVERY.md#nginx.
type NginxDetector struct{}

func (NginxDetector) ID() string      { return "nginx" }
func (NginxDetector) Name() string    { return "Nginx Detector" }
func (NginxDetector) Version() string { return "1.0" }

var nginxDirs = []string{
	"/etc/nginx/sites-enabled",
	"/etc/nginx/conf.d",
	"/etc/nginx/sites-available",
}

func (NginxDetector) Available(context.Context) model.Availability {
	if fileExists("/etc/nginx/nginx.conf") {
		return model.Availability{Available: true}
	}
	if _, ok := lookPath("nginx"); ok {
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "nginx not detected"}
}

func (NginxDetector) Detect(context.Context) ([]model.Resource, error) {
	files := map[string]bool{}
	for _, dir := range nginxDirs {
		matches, _ := filepath.Glob(filepath.Join(dir, "*"))
		for _, m := range matches {
			files[m] = true
		}
	}

	var out []model.Resource
	seen := map[string]bool{}
	for file := range files {
		for _, vh := range parseNginxServers(readTrim(file)) {
			if vh.ServerName == "" || seen[vh.ServerName] {
				continue
			}
			seen[vh.ServerName] = true
			attrs := map[string]any{
				"config_file": file,
				"listen":      vh.Listen,
				"ssl":         vh.SSL,
			}
			if vh.Certificate != "" {
				attrs["ssl_certificate"] = vh.Certificate
			}
			ups := vh.Upstreams
			if len(ups) > 0 {
				attrs["upstreams"] = ups
			}
			// Security posture for the Security view.
			if len(vh.Headers) > 0 {
				attrs["headers"] = vh.Headers
			}
			attrs["has_hsts"] = hasHeader(vh.Headers, "Strict-Transport-Security")
			attrs["has_xframe"] = hasHeader(vh.Headers, "X-Frame-Options")
			attrs["has_csp"] = hasHeader(vh.Headers, "Content-Security-Policy")
			attrs["has_xcto"] = hasHeader(vh.Headers, "X-Content-Type-Options")
			attrs["has_rate_limit"] = vh.HasRateLimit
			attrs["server_tokens_off"] = vh.ServerTokensOff
			out = append(out, model.Resource{
				Type:       "nginx_vhost",
				ID:         "nginx:" + vh.ServerName,
				Name:       vh.ServerName,
				Health:     model.StatusHealthy,
				DetectedBy: "nginx",
				DetectedAt: time.Now().UTC(),
				Attributes: attrs,
			})
		}
	}

	// A single logical reverse-proxy resource for the topology root.
	out = append(out, model.Resource{
		Type:       "reverse_proxy",
		ID:         "reverse_proxy:nginx",
		Name:       "nginx",
		Health:     model.StatusHealthy,
		DetectedBy: "nginx",
		DetectedAt: time.Now().UTC(),
		Attributes: map[string]any{"engine": "nginx", "vhosts": len(seen)},
	})
	return out, nil
}

func (NginxDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

type nginxVHost struct {
	ServerName      string
	Listen          []string
	SSL             bool
	Certificate     string
	Upstreams       []string
	Headers         []string // add_header directive names
	HasRateLimit    bool     // limit_req present
	ServerTokensOff bool     // server_tokens off
}

// parseNginxServers is a deliberately small, forgiving parser: it extracts
// server_name, listen, ssl_certificate and proxy_pass directives per server{}
// block. It does not attempt to be a full Nginx grammar and never writes.
func parseNginxServers(content string) []nginxVHost {
	if content == "" {
		return nil
	}
	var hosts []nginxVHost
	depth := 0
	inServer := false
	serverDepth := 0
	var cur nginxVHost

	tokens := tokenizeNginx(content)
	flush := func() {
		if inServer {
			hosts = append(hosts, cur)
			cur = nginxVHost{}
			inServer = false
		}
	}

	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		switch t {
		case "{":
			depth++
		case "}":
			if inServer && depth == serverDepth {
				flush()
			}
			depth--
		case "server":
			// Only a block-opening "server {" starts a vhost. This avoids
			// mistaking `server 127.0.0.1:3000;` inside an upstream{} block.
			if !inServer && i+1 < len(tokens) && tokens[i+1] == "{" {
				inServer = true
				serverDepth = depth + 1
			}
		case "server_name":
			if inServer && i+1 < len(tokens) {
				cur.ServerName = firstName(tokens, i+1)
			}
		case "listen":
			if inServer && i+1 < len(tokens) {
				val := tokens[i+1]
				cur.Listen = append(cur.Listen, val)
				if strings.Contains(strings.Join(collectUntilSemicolon(tokens, i+1), " "), "ssl") {
					cur.SSL = true
				}
			}
		case "ssl_certificate":
			if inServer && i+1 < len(tokens) {
				cur.Certificate = strings.TrimRight(tokens[i+1], ";")
				cur.SSL = true
			}
		case "proxy_pass":
			if inServer && i+1 < len(tokens) {
				cur.Upstreams = append(cur.Upstreams, cleanUpstream(tokens[i+1]))
			}
		case "add_header":
			if inServer && i+1 < len(tokens) {
				cur.Headers = append(cur.Headers, strings.TrimRight(tokens[i+1], ";"))
			}
		case "limit_req":
			if inServer {
				cur.HasRateLimit = true
			}
		case "server_tokens":
			if inServer && i+1 < len(tokens) && strings.TrimRight(tokens[i+1], ";") == "off" {
				cur.ServerTokensOff = true
			}
		}
	}
	flush()
	return hosts
}

// tokenizeNginx splits config into whitespace tokens, stripping comments and
// keeping braces/semicolons as separate tokens.
func tokenizeNginx(content string) []string {
	var toks []string
	for _, rawLine := range strings.Split(content, "\n") {
		line := rawLine
		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		line = strings.ReplaceAll(line, "{", " { ")
		line = strings.ReplaceAll(line, "}", " } ")
		line = strings.ReplaceAll(line, ";", " ; ")
		for _, f := range strings.Fields(line) {
			toks = append(toks, f)
		}
	}
	return toks
}

func firstName(tokens []string, idx int) string {
	if idx >= len(tokens) {
		return ""
	}
	return strings.TrimRight(tokens[idx], ";")
}

func collectUntilSemicolon(tokens []string, idx int) []string {
	var out []string
	for i := idx; i < len(tokens) && tokens[i] != ";"; i++ {
		out = append(out, tokens[i])
	}
	return out
}

func cleanUpstream(s string) string {
	s = strings.TrimRight(s, ";")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	return s
}

func hasHeader(headers []string, name string) bool {
	for _, h := range headers {
		if strings.EqualFold(strings.Trim(h, "\"'"), name) {
			return true
		}
	}
	return false
}
