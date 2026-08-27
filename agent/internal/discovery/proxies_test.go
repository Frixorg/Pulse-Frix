package discovery

import "testing"

// The proxy parsers are the only place discovery interprets someone else's
// configuration language, so they are exercised against realistic snippets.
// Every case here is parse-only — no file is written and no proxy is reloaded.

func TestParseApacheVHosts(t *testing.T) {
	cfg := `
# a comment
<VirtualHost *:443>
    ServerName app.example.com
    ServerAlias www.example.com legacy.example.com
    DocumentRoot /var/www/app
    SSLEngine on
    SSLCertificateFile /etc/letsencrypt/live/app.example.com/fullchain.pem
    ProxyPass /api http://127.0.0.1:8080/
</VirtualHost>

<VirtualHost *:80>
    ServerName plain.example.com
</VirtualHost>
`
	hosts := parseApacheVHosts(cfg)
	if len(hosts) != 2 {
		t.Fatalf("expected 2 vhosts, got %d", len(hosts))
	}
	first := hosts[0]
	if first.ServerName != "app.example.com" {
		t.Errorf("server name: got %q", first.ServerName)
	}
	if len(first.Aliases) != 2 || first.Aliases[0] != "www.example.com" {
		t.Errorf("aliases: got %v", first.Aliases)
	}
	if !first.SSL {
		t.Error("expected the :443 vhost to be marked TLS")
	}
	if first.Certificate == "" {
		t.Error("expected a certificate path")
	}
	if len(first.Upstreams) != 1 || first.Upstreams[0] != "127.0.0.1:8080/" {
		t.Errorf("upstreams: got %v", first.Upstreams)
	}
	if first.DocumentRoot != "/var/www/app" {
		t.Errorf("document root: got %q", first.DocumentRoot)
	}
	if hosts[1].SSL {
		t.Error("the :80 vhost must not be marked TLS")
	}
}

func TestParseApacheVHostsIgnoresGarbage(t *testing.T) {
	if got := parseApacheVHosts(""); got != nil {
		t.Errorf("empty config should parse to nil, got %v", got)
	}
	// A truncated block still describes a real vhost and must not panic.
	hosts := parseApacheVHosts("<VirtualHost>\n  ServerName a.example.com\n")
	if len(hosts) != 1 || hosts[0].ServerName != "a.example.com" {
		t.Errorf("got %v", hosts)
	}
}

func TestParseCaddyfile(t *testing.T) {
	cfg := `
{
	email admin@example.com
}

(snippet) {
	header X-Test 1
}

app.example.com, www.example.com {
	reverse_proxy 127.0.0.1:3000
	handle /static/* {
		root /var/www
	}
}

http://plain.example.com {
	root /srv/plain
}
`
	sites := parseCaddyfile(cfg)
	if len(sites) != 2 {
		t.Fatalf("expected 2 sites (global options and the snippet are skipped), got %d: %v", len(sites), sites)
	}
	if len(sites[0].Addresses) != 2 {
		t.Errorf("expected both addresses, got %v", sites[0].Addresses)
	}
	if len(sites[0].Upstreams) != 1 || sites[0].Upstreams[0] != "127.0.0.1:3000" {
		t.Errorf("upstreams: got %v", sites[0].Upstreams)
	}

	host, scheme := splitCaddyAddress(sites[1].Addresses[0])
	if host != "plain.example.com" || scheme != "http" {
		t.Errorf("address split: got %q / %q", host, scheme)
	}
}

func TestSplitCaddyAddress(t *testing.T) {
	cases := []struct{ in, host, scheme string }{
		{"example.com", "example.com", ""},
		{"https://example.com:8443", "example.com", "https"},
		{"http://example.com/path", "example.com", "http"},
		{":8080", "", ""}, // a bare port block binds no name
	}
	for _, tc := range cases {
		host, scheme := splitCaddyAddress(tc.in)
		if host != tc.host || scheme != tc.scheme {
			t.Errorf("%q: got %q/%q, want %q/%q", tc.in, host, scheme, tc.host, tc.scheme)
		}
	}
}

func TestParseHAProxy(t *testing.T) {
	cfg := `
global
    log /dev/log local0

defaults
    mode http

frontend www
    bind :80
    bind :443 ssl crt /etc/ssl/app.pem
    acl is_app hdr(host) -i app.example.com
    use_backend app_servers if is_app
    default_backend app_servers

backend app_servers
    server app1 127.0.0.1:8080 check
    server app2 127.0.0.1:8081 check
`
	fronts, backends := parseHAProxy(cfg)
	if len(fronts) != 1 {
		t.Fatalf("expected 1 frontend, got %d: %v", len(fronts), fronts)
	}
	f := fronts[0]
	if f.Name != "www" {
		t.Errorf("name: got %q", f.Name)
	}
	if !f.SSL {
		t.Error("expected the ssl bind to mark the frontend as TLS")
	}
	if len(f.Hosts) != 1 || f.Hosts[0] != "app.example.com" {
		t.Errorf("hosts: got %v", f.Hosts)
	}
	if len(f.Backends) != 2 {
		t.Errorf("backends: got %v", f.Backends)
	}
	if got := backends["app_servers"]; len(got) != 2 || got[0] != "127.0.0.1:8080" {
		t.Errorf("backend servers: got %v", got)
	}
}

func TestTraefikHostsFromRules(t *testing.T) {
	rule := "Host(`app.example.com`, `www.example.com`) && PathPrefix(`/api`)"
	hosts := TraefikHostsFromRules(rule)
	if len(hosts) != 2 || hosts[0] != "app.example.com" || hosts[1] != "www.example.com" {
		t.Errorf("got %v", hosts)
	}
	if got := TraefikHostsFromRules("PathPrefix(`/api`)"); len(got) != 0 {
		t.Errorf("a rule with no Host matcher should yield nothing, got %v", got)
	}
}
