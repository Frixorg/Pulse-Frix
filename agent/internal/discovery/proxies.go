package discovery

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// ProxyDetector reads and ANALYSES the reverse proxies Nginx does not cover:
// Apache httpd, Caddy, Traefik and HAProxy, plus the static name mappings in
// /etc/hosts. Like every detector it is strictly READ-ONLY — configuration is
// parsed, never rewritten — and every file is read under the host rootfs so a
// containerised agent sees the operator's real config.
//
// The parsers are deliberately small and forgiving: they extract the handful of
// directives that name a domain or an upstream and ignore everything else. They
// do not attempt to implement each product's full grammar.
//
// Emitted resource types (all consumed by the API's /domains view):
//
//	apache_vhost | caddy_site | traefik_router | haproxy_frontend | hosts_entry
//	reverse_proxy (one per engine actually found)
type ProxyDetector struct{}

func (ProxyDetector) ID() string      { return "proxies" }
func (ProxyDetector) Name() string    { return "Reverse Proxy Detector" }
func (ProxyDetector) Version() string { return "1.0" }

// proxyEngines maps an engine to the paths that prove it is installed.
var proxyEngines = map[string][]string{
	"apache":  {"/etc/apache2", "/etc/httpd"},
	"caddy":   {"/etc/caddy/Caddyfile", "/etc/caddy"},
	"traefik": {"/etc/traefik"},
	"haproxy": {"/etc/haproxy/haproxy.cfg", "/etc/haproxy"},
}

func (ProxyDetector) Available(context.Context) model.Availability {
	for _, paths := range proxyEngines {
		for _, p := range paths {
			if hostFileExists(p) {
				return model.Availability{Available: true}
			}
		}
	}
	if hostFileExists("/etc/hosts") {
		// /etc/hosts alone is still worth reporting: it is how many operators
		// pin an internal name to a local service.
		return model.Availability{Available: true}
	}
	return model.Availability{Available: false, Reason: "no apache/caddy/traefik/haproxy config found"}
}

func (ProxyDetector) Detect(context.Context) ([]model.Resource, error) {
	var out []model.Resource
	now := time.Now().UTC()

	if rs := detectApache(now); len(rs) > 0 {
		out = append(out, rs...)
		out = append(out, proxyRoot("apache", countOfType(rs, "apache_vhost"), now))
	}
	if rs := detectCaddy(now); len(rs) > 0 {
		out = append(out, rs...)
		out = append(out, proxyRoot("caddy", countOfType(rs, "caddy_site"), now))
	}
	if rs := detectTraefik(now); len(rs) > 0 {
		out = append(out, rs...)
		out = append(out, proxyRoot("traefik", countOfType(rs, "traefik_router"), now))
	} else if hostFileExists("/etc/traefik") {
		// Traefik is present but routed entirely from Docker labels; the API
		// derives those routers from the container inventory.
		out = append(out, proxyRoot("traefik", 0, now))
	}
	if rs := detectHAProxy(now); len(rs) > 0 {
		out = append(out, rs...)
		out = append(out, proxyRoot("haproxy", countOfType(rs, "haproxy_frontend"), now))
	}
	out = append(out, detectHostsFile(now)...)
	return out, nil
}

func (ProxyDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

// proxyRoot emits the single logical resource that anchors an engine in the
// topology graph, mirroring what the Nginx detector does.
func proxyRoot(engine string, vhosts int, now time.Time) model.Resource {
	return model.Resource{
		Type:       "reverse_proxy",
		ID:         "reverse_proxy:" + engine,
		Name:       engine,
		Health:     model.StatusHealthy,
		DetectedBy: "proxies",
		DetectedAt: now,
		Attributes: map[string]any{"engine": engine, "vhosts": vhosts},
	}
}

func countOfType(rs []model.Resource, typ string) int {
	n := 0
	for _, r := range rs {
		if r.Type == typ {
			n++
		}
	}
	return n
}

// --- Apache ----------------------------------------------------------------

var apacheDirs = []string{
	"/etc/apache2/sites-enabled",
	"/etc/apache2/sites-available",
	"/etc/apache2/conf.d",
	"/etc/httpd/conf.d",
	"/etc/httpd/sites-enabled",
	"/etc/httpd/vhosts.d",
}

var apacheRootConfigs = []string{
	"/etc/apache2/apache2.conf",
	"/etc/apache2/httpd.conf",
	"/etc/httpd/conf/httpd.conf",
}

func detectApache(now time.Time) []model.Resource {
	files := map[string]bool{}
	for _, dir := range apacheDirs {
		for _, m := range hostGlob(filepath.Join(dir, "*")) {
			files[m] = true
		}
	}
	for _, f := range apacheRootConfigs {
		if hostFileExists(f) {
			files[hostPath(f)] = true
		}
	}
	if len(files) == 0 {
		return nil
	}

	var out []model.Resource
	seen := map[string]bool{}
	for file := range files {
		for _, vh := range parseApacheVHosts(readTrim(file)) {
			name := vh.ServerName
			if name == "" && len(vh.Aliases) > 0 {
				name = vh.Aliases[0]
			}
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			attrs := map[string]any{
				"config_file": displayPath(file),
				"listen":      vh.Listen,
				"ssl":         vh.SSL,
				"engine":      "apache",
			}
			if len(vh.Aliases) > 0 {
				attrs["aliases"] = vh.Aliases
			}
			if vh.Certificate != "" {
				attrs["ssl_certificate"] = vh.Certificate
			}
			if len(vh.Upstreams) > 0 {
				attrs["upstreams"] = vh.Upstreams
			}
			if vh.DocumentRoot != "" {
				attrs["document_root"] = vh.DocumentRoot
			}
			out = append(out, model.Resource{
				Type:       "apache_vhost",
				ID:         "apache:" + name,
				Name:       name,
				Health:     model.StatusHealthy,
				DetectedBy: "proxies",
				DetectedAt: now,
				Attributes: attrs,
			})
		}
	}
	return out
}

type apacheVHost struct {
	ServerName   string
	Aliases      []string
	Listen       []string
	SSL          bool
	Certificate  string
	Upstreams    []string
	DocumentRoot string
}

// parseApacheVHosts extracts <VirtualHost> blocks. Apache directives are
// line-oriented, which keeps this parser far simpler than the Nginx one.
func parseApacheVHosts(content string) []apacheVHost {
	if content == "" {
		return nil
	}
	var out []apacheVHost
	var cur *apacheVHost
	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(stripApacheComment(raw))
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)

		if strings.HasPrefix(lower, "<virtualhost") {
			cur = &apacheVHost{}
			// "<VirtualHost *:443>" -> the ":443" part is the listen address.
			inner := strings.TrimSuffix(strings.TrimPrefix(line, "<"), ">")
			addrs := strings.Fields(inner)
			if len(addrs) > 0 {
				addrs = addrs[1:]
			}
			for _, f := range addrs {
				addr := strings.TrimSuffix(f, ">")
				cur.Listen = append(cur.Listen, addr)
				if strings.HasSuffix(addr, ":443") {
					cur.SSL = true
				}
			}
			continue
		}
		if strings.HasPrefix(lower, "</virtualhost") {
			if cur != nil {
				out = append(out, *cur)
				cur = nil
			}
			continue
		}
		if cur == nil {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "servername":
			cur.ServerName = strings.TrimSuffix(fields[1], ":443")
		case "serveralias":
			cur.Aliases = append(cur.Aliases, fields[1:]...)
		case "documentroot":
			cur.DocumentRoot = strings.Trim(fields[1], `"`)
		case "sslengine":
			if strings.EqualFold(fields[1], "on") {
				cur.SSL = true
			}
		case "sslcertificatefile":
			cur.Certificate = strings.Trim(fields[1], `"`)
			cur.SSL = true
		case "proxypass":
			// "ProxyPass /api http://127.0.0.1:8080/" — the target is the last
			// field that looks like a URL.
			if u := lastURLField(fields); u != "" {
				cur.Upstreams = append(cur.Upstreams, cleanUpstream(u))
			}
		}
	}
	// An unterminated block still describes a real vhost.
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func stripApacheComment(line string) string {
	if i := strings.IndexByte(line, '#'); i >= 0 {
		return line[:i]
	}
	return line
}

func lastURLField(fields []string) string {
	for i := len(fields) - 1; i >= 1; i-- {
		f := fields[i]
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") ||
			strings.HasPrefix(f, "unix:") || strings.HasPrefix(f, "fcgi://") {
			return f
		}
	}
	return ""
}

// --- Caddy -----------------------------------------------------------------

func detectCaddy(now time.Time) []model.Resource {
	var files []string
	if hostFileExists("/etc/caddy/Caddyfile") {
		files = append(files, hostPath("/etc/caddy/Caddyfile"))
	}
	files = append(files, hostGlob("/etc/caddy/conf.d/*")...)
	files = append(files, hostGlob("/etc/caddy/sites-enabled/*")...)
	if len(files) == 0 {
		return nil
	}

	var out []model.Resource
	seen := map[string]bool{}
	for _, file := range files {
		for _, site := range parseCaddyfile(readTrim(file)) {
			for _, addr := range site.Addresses {
				host, scheme := splitCaddyAddress(addr)
				if host == "" || seen[host] {
					continue
				}
				seen[host] = true
				attrs := map[string]any{
					"config_file": displayPath(file),
					"engine":      "caddy",
					// Caddy provisions TLS automatically for any public name,
					// so anything not explicitly http:// is served over TLS.
					"ssl": scheme != "http",
				}
				if len(site.Upstreams) > 0 {
					attrs["upstreams"] = site.Upstreams
				}
				if site.Root != "" {
					attrs["document_root"] = site.Root
				}
				out = append(out, model.Resource{
					Type:       "caddy_site",
					ID:         "caddy:" + host,
					Name:       host,
					Health:     model.StatusHealthy,
					DetectedBy: "proxies",
					DetectedAt: now,
					Attributes: attrs,
				})
			}
		}
	}
	return out
}

type caddySite struct {
	Addresses []string
	Upstreams []string
	Root      string
}

// parseCaddyfile walks top-level site blocks: one or more comma-separated
// addresses followed by "{ ... }". Global options and snippets (blocks whose
// address starts with "(" or is empty) are skipped.
func parseCaddyfile(content string) []caddySite {
	if content == "" {
		return nil
	}
	var out []caddySite
	var cur *caddySite
	depth := 0

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(stripApacheComment(raw))
		if line == "" {
			continue
		}

		if depth == 0 && strings.HasSuffix(line, "{") {
			header := strings.TrimSpace(strings.TrimSuffix(line, "{"))
			depth = 1
			if header == "" || strings.HasPrefix(header, "(") {
				cur = nil // global options / snippet
				continue
			}
			cur = &caddySite{Addresses: splitCaddyAddresses(header)}
			continue
		}

		if depth > 0 {
			depth += strings.Count(line, "{") - strings.Count(line, "}")
			if depth <= 0 {
				depth = 0
				if cur != nil {
					out = append(out, *cur)
					cur = nil
				}
				continue
			}
			if cur == nil {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			switch strings.ToLower(fields[0]) {
			case "reverse_proxy":
				for _, f := range fields[1:] {
					if strings.HasPrefix(f, "{") || f == "{" {
						break
					}
					cur.Upstreams = append(cur.Upstreams, cleanUpstream(f))
				}
			case "root":
				cur.Root = fields[len(fields)-1]
			}
			continue
		}

		// A one-line site with no block: "example.com reverse_proxy :8080".
		if fields := strings.Fields(line); len(fields) >= 2 && looksLikeCaddyAddress(fields[0]) {
			site := caddySite{Addresses: splitCaddyAddresses(fields[0])}
			if strings.EqualFold(fields[1], "reverse_proxy") && len(fields) > 2 {
				site.Upstreams = append(site.Upstreams, cleanUpstream(fields[2]))
			}
			out = append(out, site)
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	return out
}

func splitCaddyAddresses(header string) []string {
	var out []string
	for _, part := range strings.Split(header, ",") {
		for _, f := range strings.Fields(part) {
			if looksLikeCaddyAddress(f) {
				out = append(out, f)
			}
		}
	}
	return out
}

// looksLikeCaddyAddress rejects the directive keywords that can share a line
// with an address, keeping only things that name a host or a port.
func looksLikeCaddyAddress(s string) bool {
	if s == "" || strings.HasPrefix(s, "(") || strings.HasPrefix(s, "{") {
		return false
	}
	return strings.Contains(s, ".") || strings.HasPrefix(s, ":") ||
		strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") ||
		s == "localhost"
}

// splitCaddyAddress turns "https://example.com:8443" into ("example.com", "https").
func splitCaddyAddress(addr string) (host, scheme string) {
	scheme = ""
	if s, rest, ok := strings.Cut(addr, "://"); ok {
		scheme = s
		addr = rest
	}
	if i := strings.IndexByte(addr, '/'); i >= 0 {
		addr = addr[:i]
	}
	if strings.HasPrefix(addr, ":") {
		return "", scheme // a bare port block binds no name
	}
	if h, _, ok := strings.Cut(addr, ":"); ok {
		addr = h
	}
	return addr, scheme
}

// --- Traefik ---------------------------------------------------------------

// traefikHostRule matches the Host(`a.example.com`, `b.example.com`) matcher
// used in both file providers and Docker labels.
var traefikHostRule = regexp.MustCompile("Host\\(([^)]*)\\)")

// traefikQuoted pulls each name out of a matcher's argument list.
var traefikQuoted = regexp.MustCompile("[`\"']([^`\"']+)[`\"']")

func detectTraefik(now time.Time) []model.Resource {
	var files []string
	for _, pat := range []string{
		"/etc/traefik/*.yml", "/etc/traefik/*.yaml", "/etc/traefik/*.toml",
		"/etc/traefik/dynamic/*", "/etc/traefik/conf.d/*",
	} {
		files = append(files, hostGlob(pat)...)
	}
	if len(files) == 0 {
		return nil
	}

	var out []model.Resource
	seen := map[string]bool{}
	for _, file := range files {
		content := readTrim(file)
		if content == "" {
			continue
		}
		for _, host := range TraefikHostsFromRules(content) {
			if seen[host] {
				continue
			}
			seen[host] = true
			out = append(out, model.Resource{
				Type:       "traefik_router",
				ID:         "traefik:" + host,
				Name:       host,
				Health:     model.StatusHealthy,
				DetectedBy: "proxies",
				DetectedAt: now,
				Attributes: map[string]any{
					"config_file": displayPath(file),
					"engine":      "traefik",
					// Traefik terminates TLS through a certresolver; assume TLS
					// unless the file says otherwise. The SSL detector confirms.
					"ssl": strings.Contains(content, "certresolver") || strings.Contains(content, "tls"),
				},
			})
		}
	}
	return out
}

// TraefikHostsFromRules extracts every hostname named by a Host(...) matcher in
// a Traefik rule, a dynamic-config file, or a Docker label value. Exported so
// the same extraction can be reused wherever Traefik rules turn up.
func TraefikHostsFromRules(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range traefikHostRule.FindAllStringSubmatch(s, -1) {
		for _, q := range traefikQuoted.FindAllStringSubmatch(m[1], -1) {
			host := strings.TrimSpace(q[1])
			if host == "" || seen[host] {
				continue
			}
			seen[host] = true
			out = append(out, host)
		}
	}
	return out
}

// --- HAProxy ---------------------------------------------------------------

func detectHAProxy(now time.Time) []model.Resource {
	files := []string{}
	if hostFileExists("/etc/haproxy/haproxy.cfg") {
		files = append(files, hostPath("/etc/haproxy/haproxy.cfg"))
	}
	files = append(files, hostGlob("/etc/haproxy/conf.d/*")...)
	if len(files) == 0 {
		return nil
	}

	var out []model.Resource
	for _, file := range files {
		fronts, backends := parseHAProxy(readTrim(file))
		for _, f := range fronts {
			attrs := map[string]any{
				"config_file": displayPath(file),
				"engine":      "haproxy",
				"ssl":         f.SSL,
			}
			if len(f.Binds) > 0 {
				attrs["listen"] = f.Binds
			}
			if len(f.Hosts) > 0 {
				attrs["hosts"] = f.Hosts
			}
			if len(f.Backends) > 0 {
				attrs["backends"] = f.Backends
				var ups []string
				for _, b := range f.Backends {
					ups = append(ups, backends[b]...)
				}
				if len(ups) > 0 {
					attrs["upstreams"] = ups
				}
			}
			out = append(out, model.Resource{
				Type:       "haproxy_frontend",
				ID:         "haproxy:" + f.Name,
				Name:       f.Name,
				Health:     model.StatusHealthy,
				DetectedBy: "proxies",
				DetectedAt: now,
				Attributes: attrs,
			})
		}
	}
	return out
}

type haproxyFrontend struct {
	Name     string
	Binds    []string
	SSL      bool
	Hosts    []string
	Backends []string
}

// parseHAProxy returns the frontends and a backend-name -> server-addresses map.
// HAProxy config is section-based: a non-indented keyword opens a section and
// indented lines belong to it.
func parseHAProxy(content string) ([]haproxyFrontend, map[string][]string) {
	backends := map[string][]string{}
	if content == "" {
		return nil, backends
	}
	var fronts []haproxyFrontend
	var cur *haproxyFrontend
	curBackend := ""

	flush := func() {
		if cur != nil {
			fronts = append(fronts, *cur)
			cur = nil
		}
	}

	for _, raw := range strings.Split(content, "\n") {
		line := strings.TrimSpace(stripApacheComment(raw))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		switch strings.ToLower(fields[0]) {
		case "frontend", "listen":
			flush()
			curBackend = ""
			if len(fields) > 1 {
				cur = &haproxyFrontend{Name: fields[1]}
			}
			continue
		case "backend":
			flush()
			if len(fields) > 1 {
				curBackend = fields[1]
			}
			continue
		case "global", "defaults", "resolvers", "peers", "userlist":
			flush()
			curBackend = ""
			continue
		}

		if curBackend != "" && strings.EqualFold(fields[0], "server") && len(fields) > 2 {
			backends[curBackend] = append(backends[curBackend], fields[2])
			continue
		}
		if cur == nil {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "bind":
			if len(fields) > 1 {
				cur.Binds = append(cur.Binds, fields[1])
			}
			if strings.Contains(strings.ToLower(line), " ssl") {
				cur.SSL = true
			}
		case "acl":
			// "acl is_site hdr(host) -i example.com"
			if strings.Contains(strings.ToLower(line), "hdr(host)") && len(fields) > 3 {
				for _, f := range fields[3:] {
					if strings.Contains(f, ".") && !strings.HasPrefix(f, "-") {
						cur.Hosts = append(cur.Hosts, f)
					}
				}
			}
		case "use_backend", "default_backend":
			if len(fields) > 1 {
				cur.Backends = append(cur.Backends, fields[1])
			}
		}
	}
	flush()
	return fronts, backends
}

// --- /etc/hosts ------------------------------------------------------------

// hostsIgnore are the names every distro ships; reporting them is pure noise.
var hostsIgnore = map[string]bool{
	"localhost": true, "localhost.localdomain": true,
	"ip6-localhost": true, "ip6-loopback": true, "ip6-localnet": true,
	"ip6-mcastprefix": true, "ip6-allnodes": true, "ip6-allrouters": true,
}

func detectHostsFile(now time.Time) []model.Resource {
	lines := readLines(hostPath("/etc/hosts"))
	if len(lines) == 0 {
		return nil
	}
	var out []model.Resource
	seen := map[string]bool{}
	for _, raw := range lines {
		line := strings.TrimSpace(stripApacheComment(raw))
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		ip := fields[0]
		for _, name := range fields[1:] {
			if hostsIgnore[strings.ToLower(name)] || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, model.Resource{
				Type:       "hosts_entry",
				ID:         "hosts:" + name,
				Name:       name,
				Health:     model.StatusHealthy,
				DetectedBy: "proxies",
				DetectedAt: now,
				Attributes: map[string]any{"address": ip, "source": "/etc/hosts"},
			})
		}
	}
	return out
}
