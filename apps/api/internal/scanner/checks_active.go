package scanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxBody caps how much of a response body we read into memory per probe.
const maxBody = 128 << 10 // 128 KiB

// httpResult is a captured HTTP response (headers + a bounded body snippet).
// FinalURL is the URL after any redirects the safe client followed, which lets
// checks reason about redirect targets without an unguarded no-follow client.
type httpResult struct {
	Status   int
	Header   http.Header
	Body     string
	Cookies  []*http.Cookie
	FinalURL string
	Err      error
}

// doReq performs one safe HTTP request. The client is the SSRF-guarded client,
// so only public hosts are reachable; internal targets fail closed.
func (in *Input) doReq(ctx context.Context, method, rawURL string, headers map[string]string) *httpResult {
	if in.httpClient == nil {
		return &httpResult{Err: fmt.Errorf("no http client")}
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, nil)
	if err != nil {
		return &httpResult{Err: err}
	}
	req.Header.Set("User-Agent", "PulseFrix-SecurityScanner/1.0 (+non-destructive)")
	req.Header.Set("Accept", "*/*")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := in.httpClient.Do(req)
	if err != nil {
		return &httpResult{Err: err}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	final := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}
	return &httpResult{
		Status:   resp.StatusCode,
		Header:   resp.Header,
		Body:     string(body),
		Cookies:  resp.Cookies(),
		FinalURL: final,
	}
}

// getRoot fetches (and caches for the scan) a GET of the target's root URL, so
// the header-family checks don't each re-request the same page.
func (in *Input) getRoot(ctx context.Context, t Target) *httpResult {
	in.mu.Lock()
	if in.rootCache == nil {
		in.rootCache = map[string]*httpResult{}
	}
	if r, ok := in.rootCache[t.URL]; ok {
		in.mu.Unlock()
		return r
	}
	in.mu.Unlock()

	r := in.doReq(ctx, http.MethodGet, t.URL, nil)
	in.mu.Lock()
	in.rootCache[t.URL] = r
	in.mu.Unlock()
	return r
}

func marker() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "pulse" + hex.EncodeToString(b)
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return u.Host
}

// activeChecks are safe, read-shaped HTTP probes against the server's own public
// endpoints. None of them attempt destructive exploitation.
func activeChecks() []checkDef {
	return []checkDef{
		{
			meta: Check{ID: "http-security-headers", Category: "api-security", Kind: KindActive, Name: "HTTP Security Headers", OWASP: owaspA05,
				Description: "Live check of response headers: HSTS, CSP, X-Content-Type-Options, Referrer-Policy, Permissions-Policy."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					r := in.getRoot(ctx, t)
					if r.Err != nil {
						log(LogWarn, hostOf(t.URL)+" unreachable: "+r.Err.Error())
						continue
					}
					log(LogInfo, fmt.Sprintf("%s → %d, inspecting headers", hostOf(t.URL), r.Status))
					want := []struct{ header, label, rec string }{
						{"Strict-Transport-Security", "HSTS", "Add 'Strict-Transport-Security: max-age=63072000; includeSubDomains; preload'."},
						{"Content-Security-Policy", "Content-Security-Policy", "Define a CSP that restricts script/style/connect sources."},
						{"X-Content-Type-Options", "X-Content-Type-Options", "Add 'X-Content-Type-Options: nosniff'."},
						{"Referrer-Policy", "Referrer-Policy", "Add 'Referrer-Policy: strict-origin-when-cross-origin'."},
						{"Permissions-Policy", "Permissions-Policy", "Add a Permissions-Policy to disable unused browser features."},
					}
					var missing []string
					for _, w := range want {
						if r.Header.Get(w.header) == "" {
							missing = append(missing, w.label)
							emit(Finding{
								ID: "hdr-" + w.header + "-" + t.FQDN, Severity: SeverityLow, CVSS: 3.1, CWE: "CWE-693",
								Title: "Missing " + w.label + " header", Resource: t.URL,
								Detail:         t.FQDN + " responds without the " + w.label + " header.",
								Recommendation: w.rec,
								References:     []string{"https://owasp.org/www-project-secure-headers/"},
							})
						}
					}
					if len(missing) == 0 {
						log(LogSuccess, hostOf(t.URL)+" sets all core security headers")
					}
				}
			},
		},
		{
			meta: Check{ID: "https-enforcement", Category: "crypto-transport", Kind: KindActive, Name: "HTTPS Enforcement", OWASP: owaspA02,
				Description: "Confirms plain HTTP redirects to HTTPS and that HSTS is present."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					httpURL := "http://" + t.FQDN + "/"
					// The safe client follows redirects, so a well-configured
					// server lands on an https:// FinalURL; a bad one stays http.
					r := in.doReq(ctx, http.MethodGet, httpURL, nil)
					if r.Err != nil {
						log(LogInfo, t.FQDN+" has no reachable plain-HTTP listener: "+r.Err.Error())
						continue
					}
					if strings.HasPrefix(strings.ToLower(r.FinalURL), "https://") {
						log(LogSuccess, t.FQDN+" redirects HTTP → HTTPS")
						continue
					}
					if r.Status == 200 {
						emit(Finding{
							ID: "http-no-redirect-" + t.FQDN, Severity: SeverityMedium, CVSS: 5.9, CWE: "CWE-319",
							Title: "Plain HTTP is served without redirect to HTTPS", Resource: httpURL,
							Detail:         t.FQDN + " answers on http:// with 200 and never lands on https://. Traffic (and cookies) can be sent in cleartext.",
							Evidence:       fmt.Sprintf("GET %s → %d (final: %s)", httpURL, r.Status, r.FinalURL),
							Recommendation: "Return a 301 to the https:// URL for all plain-HTTP requests, then enable HSTS.",
							References:     []string{"https://owasp.org/Top10/A02_2021-Cryptographic_Failures/"},
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "cookie-security", Category: "auth-session", Kind: KindActive, Name: "Cookie Security Flags", OWASP: owaspA05,
				Description: "Checks Set-Cookie for Secure, HttpOnly and SameSite."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					r := in.getRoot(ctx, t)
					if r.Err != nil {
						continue
					}
					for _, c := range r.Cookies {
						var flags []string
						if !c.Secure {
							flags = append(flags, "Secure")
						}
						if !c.HttpOnly {
							flags = append(flags, "HttpOnly")
						}
						if c.SameSite == http.SameSiteNoneMode || c.SameSite == http.SameSiteDefaultMode {
							flags = append(flags, "SameSite")
						}
						if len(flags) > 0 {
							emit(Finding{
								ID: "cookie-" + t.FQDN + "-" + c.Name, Severity: SeverityMedium, CVSS: 5.4, CWE: "CWE-1004",
								Title: "Cookie '" + c.Name + "' missing " + strings.Join(flags, "/"), Resource: t.URL,
								Detail:         "The cookie '" + c.Name + "' is set without " + strings.Join(flags, ", ") + ". This exposes it to theft (no Secure/HttpOnly) or CSRF (weak SameSite).",
								Evidence:       "Set-Cookie: " + c.Name,
								Recommendation: "Set the Secure and HttpOnly flags and SameSite=Lax (or Strict) on session cookies.",
								References:     []string{"https://owasp.org/www-community/controls/SecureCookieAttribute"},
							})
						}
					}
				}
			},
		},
		{
			meta: Check{ID: "cors-policy", Category: "api-security", Kind: KindActive, Name: "CORS Misconfiguration", OWASP: owaspA05,
				Description: "Sends a foreign Origin and checks whether it is reflected with credentials."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				const evil = "https://pulse-scanner.example"
				for _, t := range in.Targets {
					r := in.doReq(ctx, http.MethodGet, t.URL, map[string]string{"Origin": evil})
					if r.Err != nil {
						continue
					}
					acao := r.Header.Get("Access-Control-Allow-Origin")
					acac := strings.EqualFold(r.Header.Get("Access-Control-Allow-Credentials"), "true")
					switch {
					case acao == evil && acac:
						emit(Finding{
							ID: "cors-reflect-" + t.FQDN, Severity: SeverityHigh, CVSS: 7.5, CWE: "CWE-942",
							Title: "CORS reflects arbitrary Origin with credentials", Resource: t.URL,
							Detail:         t.FQDN + " reflects any Origin in Access-Control-Allow-Origin while also allowing credentials — any site can read authenticated responses.",
							Evidence:       "Origin: " + evil + " → Access-Control-Allow-Origin: " + acao + "; Allow-Credentials: true",
							Recommendation: "Never reflect Origin blindly. Allow-list exact origins and only send Allow-Credentials to trusted ones.",
							References:     []string{"https://portswigger.net/web-security/cors"},
						})
					case acao == "*" && acac:
						emit(Finding{
							ID: "cors-wildcard-" + t.FQDN, Severity: SeverityMedium, CVSS: 5.3, CWE: "CWE-942",
							Title: "CORS wildcard with credentials", Resource: t.URL,
							Detail:         t.FQDN + " combines Access-Control-Allow-Origin: * with credentials (browsers block this, but it signals a misconfigured policy).",
							Recommendation: "Use an explicit origin allow-list instead of '*' when credentials are involved.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "clickjacking", Category: "client-side", Kind: KindActive, Name: "Clickjacking Protection", OWASP: owaspA05,
				Description: "Flags pages framable due to missing X-Frame-Options and CSP frame-ancestors."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					r := in.getRoot(ctx, t)
					if r.Err != nil {
						continue
					}
					xfo := r.Header.Get("X-Frame-Options")
					csp := strings.ToLower(r.Header.Get("Content-Security-Policy"))
					if xfo == "" && !strings.Contains(csp, "frame-ancestors") {
						emit(Finding{
							ID: "clickjack-" + t.FQDN, Severity: SeverityMedium, CVSS: 4.3, CWE: "CWE-1021",
							Title: "Page can be framed (clickjacking)", Resource: t.URL,
							Detail:         t.FQDN + " sets neither X-Frame-Options nor a CSP frame-ancestors directive, so it can be embedded in a hostile iframe.",
							Recommendation: "Add 'X-Frame-Options: DENY' (or SAMEORIGIN) and a CSP 'frame-ancestors' directive.",
							References:     []string{"https://owasp.org/www-community/attacks/Clickjacking"},
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "http-methods", Category: "api-security", Kind: KindActive, Name: "Dangerous HTTP Methods", OWASP: owaspA05,
				Description: "Uses OPTIONS to detect TRACE / PUT / DELETE being advertised."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				risky := map[string]bool{"TRACE": true, "TRACK": true, "PUT": true, "DELETE": true, "CONNECT": true}
				for _, t := range in.Targets {
					r := in.doReq(ctx, http.MethodOptions, t.URL, nil)
					if r.Err != nil {
						continue
					}
					allow := r.Header.Get("Allow")
					if allow == "" {
						continue
					}
					var found []string
					for _, m := range strings.Split(allow, ",") {
						m = strings.ToUpper(strings.TrimSpace(m))
						if risky[m] {
							found = append(found, m)
						}
					}
					if len(found) > 0 {
						emit(Finding{
							ID: "methods-" + t.FQDN, Severity: SeverityLow, CVSS: 4.0, CWE: "CWE-650",
							Title: "Risky HTTP methods enabled: " + strings.Join(found, ", "), Resource: t.URL,
							Detail:         t.FQDN + " advertises " + strings.Join(found, ", ") + " via OPTIONS. TRACE enables cross-site tracing; PUT/DELETE may allow unintended writes.",
							Evidence:       "Allow: " + allow,
							Recommendation: "Disable methods the app doesn't use (e.g. limit_except in nginx) — especially TRACE.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "server-banner", Category: "info-disclosure", Kind: KindActive, Name: "Version / Banner Disclosure", OWASP: owaspA05,
				Description: "Reads Server and X-Powered-By headers for leaked software versions."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					r := in.getRoot(ctx, t)
					if r.Err != nil {
						continue
					}
					server := r.Header.Get("Server")
					powered := r.Header.Get("X-Powered-By")
					if hasVersionDigits(server) || powered != "" {
						detail := t.FQDN + " leaks software details:"
						if server != "" {
							detail += " Server: " + server + "."
						}
						if powered != "" {
							detail += " X-Powered-By: " + powered + "."
						}
						emit(Finding{
							ID: "banner-" + t.FQDN, Severity: SeverityInfo, CVSS: 2.5, CWE: "CWE-200",
							Title: "Software version disclosed in headers", Resource: t.URL,
							Detail:         detail,
							Evidence:       strings.TrimSpace("Server: " + server + "  X-Powered-By: " + powered),
							Recommendation: "Suppress version banners (server_tokens off; remove X-Powered-By).",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "exposed-files", Category: "secrets-creds", Kind: KindActive, Name: "Exposed Sensitive Files", OWASP: owaspA05,
				Description: "Probes for public .env, .git, backups and admin/debug endpoints (GET only)."},
			run: runExposedFiles,
		},
		{
			meta: Check{ID: "directory-listing", Category: "info-disclosure", Kind: KindActive, Name: "Directory Listing", OWASP: owaspA05,
				Description: "Detects autoindex 'Index of /' pages that leak file structure."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				paths := []string{"/", "/uploads/", "/files/", "/backup/", "/static/", "/assets/"}
				for _, t := range in.Targets {
					for _, p := range paths {
						r := in.doReq(ctx, http.MethodGet, strings.TrimRight(t.URL, "/")+p, nil)
						if r.Err != nil || r.Status != 200 {
							continue
						}
						if strings.Contains(r.Body, "<title>Index of /") || strings.Contains(r.Body, "Directory listing for") {
							emit(Finding{
								ID: "dirlist-" + t.FQDN + p, Severity: SeverityMedium, CVSS: 5.3, CWE: "CWE-548",
								Title: "Directory listing enabled at " + p, Resource: t.URL + p,
								Detail:         "The server returns an auto-generated file index at " + p + ", exposing the directory's contents.",
								Recommendation: "Disable autoindex (nginx: 'autoindex off;') and serve an explicit index file.",
							})
						}
					}
				}
			},
		},
		{
			meta: Check{ID: "reflected-input", Category: "client-side", Kind: KindActive, Name: "Reflected Input (potential XSS)", OWASP: owaspA03,
				Description: "Sends a unique benign marker in a query parameter and checks for unescaped reflection."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					m := marker()
					probe := t.URL + "/?q=" + m + "&search=" + m
					r := in.doReq(ctx, http.MethodGet, probe, nil)
					if r.Err != nil {
						continue
					}
					if strings.Contains(r.Body, m) {
						ct := r.Header.Get("Content-Type")
						sev := SeverityLow
						if strings.Contains(ct, "text/html") {
							sev = SeverityMedium
						}
						emit(Finding{
							ID: "reflect-" + t.FQDN, Severity: sev, CVSS: 4.7, CWE: "CWE-79",
							Title: "Query parameter reflected in the response", Resource: probe,
							Detail:         "A unique marker sent in a query string was reflected back in an HTML response. This is a potential reflected-XSS sink — it needs manual confirmation of the output context and encoding.",
							Evidence:       "reflected marker: " + m + " (Content-Type: " + ct + ")",
							Recommendation: "Context-encode all user input on output and apply a strict CSP. Confirm whether the reflection is exploitable.",
							References:     []string{"https://owasp.org/www-community/attacks/xss/"},
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "sql-error", Category: "injection", Kind: KindActive, Name: "SQL Error Signature (potential SQLi)", OWASP: owaspA03,
				Description: "Appends a single quote to a parameter and looks for database error strings."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, t := range in.Targets {
					probe := t.URL + "/?id=1%27" // a single quote, non-destructive
					r := in.doReq(ctx, http.MethodGet, probe, nil)
					if r.Err != nil {
						continue
					}
					if sig := sqlErrorSignature(r.Body); sig != "" {
						emit(Finding{
							ID: "sqlerr-" + t.FQDN, Severity: SeverityMedium, CVSS: 6.5, CWE: "CWE-89",
							Title: "Database error triggered by a quote (potential SQL injection)", Resource: probe,
							Detail:         "Adding a single quote to a parameter produced a database error message (" + sig + "). This is a strong indicator of unsanitised SQL — it requires manual confirmation.",
							Evidence:       "signature: " + sig,
							Recommendation: "Use parameterised queries / prepared statements and never build SQL by string concatenation. Validate exploitability manually.",
							References:     []string{"https://owasp.org/Top10/A03_2021-Injection/"},
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "open-redirect", Category: "server-side", Kind: KindActive, Name: "Open Redirect", OWASP: owaspA01,
				Description: "Checks common redirect parameters for redirection to an external host."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				const dest = "https://example.com/"
				params := []string{"next", "url", "redirect", "return", "returnUrl", "r"}
				for _, t := range in.Targets {
					for _, p := range params {
						probe := t.URL + "/?" + p + "=" + url.QueryEscape(dest)
						r := in.doReq(ctx, http.MethodGet, probe, nil)
						if r.Err != nil {
							continue
						}
						// The safe client follows the redirect (example.com is
						// public); if we land on example.com the redirect is open.
						if hostOf(r.FinalURL) == "example.com" {
							emit(Finding{
								ID: "openredir-" + t.FQDN + "-" + p, Severity: SeverityMedium, CVSS: 6.1, CWE: "CWE-601",
								Title: "Open redirect via '" + p + "' parameter", Resource: probe,
								Detail:         t.FQDN + " redirects to an attacker-controlled external URL supplied in the '" + p + "' parameter. This aids phishing and can chain into SSRF/OAuth theft.",
								Evidence:       "final URL: " + r.FinalURL,
								Recommendation: "Only redirect to allow-listed relative paths; reject absolute external URLs.",
								References:     []string{"https://owasp.org/www-community/attacks/Open_redirect"},
							})
							break
						}
					}
				}
			},
		},
	}
}

// runExposedFiles probes a curated set of sensitive paths, first fingerprinting
// the server's soft-404 behaviour so we don't flag SPA catch-all 200s.
func runExposedFiles(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
	type probe struct {
		path, label, sev, detail string
	}
	probes := []probe{
		{"/.env", "environment file", SeverityCritical, "A .env file typically holds secrets, DB passwords and API keys."},
		{"/.git/config", "Git repository", SeverityHigh, "An exposed .git directory can leak your full source history."},
		{"/.git/HEAD", "Git repository", SeverityHigh, "An exposed .git directory can leak your full source history."},
		{"/.svn/entries", "SVN metadata", SeverityMedium, "Exposed version-control metadata can leak source and structure."},
		{"/.aws/credentials", "AWS credentials", SeverityCritical, "Cloud credentials grant direct access to your AWS account."},
		{"/.htpasswd", "htpasswd file", SeverityHigh, "Contains hashed HTTP basic-auth credentials."},
		{"/config.php.bak", "config backup", SeverityHigh, "Backup config files often expose credentials in plaintext."},
		{"/docker-compose.yml", "compose file", SeverityMedium, "May reveal service topology and embedded secrets."},
		{"/wp-config.php.bak", "WordPress config backup", SeverityCritical, "A WordPress config backup exposes database credentials."},
		{"/phpinfo.php", "phpinfo", SeverityMedium, "phpinfo() dumps environment, paths and module details."},
		{"/server-status", "Apache status", SeverityMedium, "mod_status exposes live request and client information."},
		{"/actuator/env", "Spring actuator", SeverityHigh, "Spring Boot actuator endpoints can leak env vars and secrets."},
		{"/.DS_Store", "macOS metadata", SeverityLow, "A .DS_Store file leaks directory/file names."},
		{"/backup.zip", "backup archive", SeverityHigh, "A public backup archive can expose source and data."},
		{"/.well-known/security.txt", "security.txt", SeverityInfo, "Present (good practice) — no action required beyond keeping it current."},
	}

	for _, t := range in.Targets {
		base := strings.TrimRight(t.URL, "/")
		// Fingerprint the not-found behaviour with a random path.
		fp := in.doReq(ctx, http.MethodGet, base+"/"+marker()+"-notfound", nil)
		soft404 := fp.Err == nil && fp.Status == 200
		fpLen := 0
		if fp.Err == nil {
			fpLen = len(fp.Body)
		}
		log(LogInfo, fmt.Sprintf("%s probing %d sensitive paths (soft-404=%v)", hostOf(t.URL), len(probes), soft404))

		for _, pr := range probes {
			r := in.doReq(ctx, http.MethodGet, base+pr.path, nil)
			if r.Err != nil || r.Status != 200 || len(r.Body) == 0 {
				continue
			}
			// Skip if the server 200s everything with the same page.
			if soft404 && abs(len(r.Body)-fpLen) < 32 {
				continue
			}
			if pr.sev == SeverityInfo { // security.txt: positive signal, log only
				log(LogSuccess, t.FQDN+" publishes security.txt")
				continue
			}
			emit(Finding{
				ID: "exposed-" + t.FQDN + pr.path, Severity: pr.sev, CVSS: severityCVSS(pr.sev), CWE: "CWE-538",
				Title: "Exposed " + pr.label + " at " + pr.path, Resource: base + pr.path,
				Detail:         pr.detail + " It responded 200 at " + pr.path + ".",
				Evidence:       fmt.Sprintf("GET %s → 200 (%d bytes)", base+pr.path, len(r.Body)),
				Recommendation: "Block this path at the reverse proxy and remove the file from the web root.",
				References:     []string{"https://owasp.org/Top10/A05_2021-Security_Misconfiguration/"},
			})
		}
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// hasVersionDigits reports whether a Server header looks like it leaks a version.
func hasVersionDigits(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// sqlErrorSignature returns a matched DB error string, or "" if none.
func sqlErrorSignature(body string) string {
	sigs := []string{
		"SQL syntax", "mysql_fetch", "MySQLSyntaxErrorException", "You have an error in your SQL syntax",
		"ORA-01756", "ORA-00933", "PostgreSQL query failed", "pg_query", "SQLite3::SQLException",
		"sqlite3.OperationalError", "Unclosed quotation mark", "Microsoft OLE DB Provider for SQL Server",
		"SQLServer JDBC Driver", "System.Data.SqlClient", "Npgsql.",
	}
	for _, s := range sigs {
		if strings.Contains(body, s) {
			return s
		}
	}
	return ""
}

// severityCVSS maps a severity label to a representative CVSS base score for
// findings whose score depends on which path matched.
func severityCVSS(sev string) float64 {
	switch sev {
	case SeverityCritical:
		return 9.1
	case SeverityHigh:
		return 7.5
	case SeverityMedium:
		return 5.3
	case SeverityLow:
		return 3.1
	default:
		return 0
	}
}
