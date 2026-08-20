package scanner

import (
	"context"
	"fmt"
	"strings"
)

// emitFunc records a finding; logFunc writes a line to the live console.
type (
	emitFunc func(Finding)
	logFunc  func(level, msg string)
	runFunc  func(ctx context.Context, in *Input, emit emitFunc, log logFunc)
)

// checkDef is catalogue metadata plus the runner that produces findings.
type checkDef struct {
	meta Check
	run  runFunc
}

// passiveChecks reason purely over the discovery snapshot — no network, always
// safe to run. They cover the host/config surface the agent already sees.
func passiveChecks() []checkDef {
	return []checkDef{
		{
			meta: Check{ID: "localhost-isolation", Category: "infra-config", Kind: KindPassive, Name: "Localhost Isolation", OWASP: owaspA05,
				Description: "Datastores should never listen on public interfaces."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, db := range in.resourcesOfType("database") {
					if attrString(db.Attributes, "exposure") != "public" {
						continue
					}
					log(LogWarn, db.Name+" is reachable from outside the host")
					emit(Finding{
						ID: "db-public-" + db.Name, Severity: SeverityCritical, CVSS: 9.1, CWE: "CWE-668",
						Title:          db.Name + " is reachable from outside the host",
						Resource:       db.Name,
						Detail:         "This database listens on all interfaces (0.0.0.0), so anything that can reach the host can attempt to connect.",
						Recommendation: "Bind it to 127.0.0.1 or an internal Docker network and reach it through your app — never publish the DB port.",
						References:     []string{"https://owasp.org/Top10/A05_2021-Security_Misconfiguration/"},
					})
				}
			},
		},
		{
			meta: Check{ID: "docker-exposure", Category: "infra-config", Kind: KindPassive, Name: "Docker Daemon Exposure", OWASP: owaspA05,
				Description: "A network-exposed Docker API is root-equivalent access to the host."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, port := range in.resourcesOfType("listening_port") {
					if len(port.Ports) == 0 || attrString(port.Attributes, "exposure") != "public" {
						continue
					}
					if h := port.Ports[0].Host; h == 2375 || h == 2376 {
						log(LogError, "Docker daemon exposed on the network")
						emit(Finding{
							ID: "docker-daemon", Severity: SeverityCritical, CVSS: 9.8, CWE: "CWE-284",
							Title:          "Docker daemon exposed on the network",
							Resource:       fmt.Sprintf("tcp/%d", h),
							Detail:         "The Docker API is listening publicly — this is root-equivalent access to the whole host.",
							Recommendation: "Never expose the Docker socket/port. Use SSH or a read-only socket proxy.",
							References:     []string{"https://docs.docker.com/engine/security/protect-access/"},
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "port-exposure", Category: "infra-config", Kind: KindPassive, Name: "Port Exposure", OWASP: owaspA05,
				Description: "Non-web ports on public interfaces widen the attack surface."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, port := range in.resourcesOfType("listening_port") {
					if len(port.Ports) == 0 || attrString(port.Attributes, "exposure") != "public" {
						continue
					}
					h := port.Ports[0].Host
					if h == 80 || h == 443 || h == 22 || h == 0 || h == 2375 || h == 2376 {
						continue
					}
					emit(Finding{
						ID: fmt.Sprintf("port-%d", h), Severity: SeverityMedium, CVSS: 5.3, CWE: "CWE-668",
						Title:          fmt.Sprintf("Port %d is exposed publicly", h),
						Resource:       fmt.Sprintf("%s · tcp/%d", port.Name, h),
						Detail:         "This port is listening on all interfaces. If it isn't meant to be public, it widens your attack surface.",
						Recommendation: "Bind it to loopback, or restrict it with a firewall / security group.",
					})
				}
			},
		},
		{
			meta: Check{ID: "tls-validity", Category: "crypto-transport", Kind: KindPassive, Name: "TLS / Certificate Validity", OWASP: owaspA02,
				Description: "Expired or near-expiry certificates break trust and availability."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, cert := range in.resourcesOfType("tls_certificate") {
					days, ok := attrFloat(cert.Attributes, "days_left")
					if !ok {
						continue
					}
					switch {
					case days < 0:
						emit(Finding{
							ID: "tls-expired-" + cert.Name, Severity: SeverityHigh, CVSS: 7.4, CWE: "CWE-298",
							Title: "Expired TLS certificate: " + cert.Name, Resource: cert.Name,
							Detail:         "The certificate has expired; clients will reject the connection.",
							Recommendation: "Renew it (e.g. certbot renew) and reload the reverse proxy.",
						})
					case days < 30:
						emit(Finding{
							ID: "tls-expiring-" + cert.Name, Severity: SeverityMedium, CVSS: 4.0, CWE: "CWE-298",
							Title: "TLS certificate expiring soon: " + cert.Name, Resource: cert.Name,
							Detail:         fmt.Sprintf("Fewer than 30 days remain (%d).", int(days)),
							Recommendation: "Confirm auto-renewal is working, or renew now.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "base-image", Category: "infra-config", Kind: KindPassive, Name: "Base Image Hygiene", OWASP: owaspA06,
				Description: "Unpinned :latest images can change silently and carry unpatched CVEs."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				unpinned := 0
				for _, c := range in.resourcesOfType("docker_container") {
					img := attrString(c.Attributes, "image")
					if img == "" {
						continue
					}
					if strings.HasSuffix(img, ":latest") || !strings.Contains(img, ":") {
						unpinned++
						if unpinned <= 6 {
							emit(Finding{
								ID: "img-" + c.Name, Severity: SeverityInfo, CVSS: 3.7, CWE: "CWE-1104",
								Title: "Unpinned image: " + img, Resource: c.Name,
								Detail:         "Using :latest (or no tag) means the image can change silently and may carry unpatched CVEs.",
								Recommendation: "Pin a specific version tag and scan images (e.g. trivy) in CI.",
							})
						}
					}
				}
				if unpinned > 6 {
					emit(Finding{
						ID: "img-more", Severity: SeverityInfo, CVSS: 3.7, CWE: "CWE-1104",
						Title:          fmt.Sprintf("%d containers run unpinned images", unpinned),
						Detail:         "Several containers use :latest or untagged images.",
						Recommendation: "Pin versions and add image scanning to your build.",
					})
				}
			},
		},
		{
			meta: Check{ID: "resource-boundaries", Category: "infra-config", Kind: KindPassive, Name: "Resource Boundaries", OWASP: owaspA04,
				Description: "Containers without limits let one workload starve the host (DoS)."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				noLimit := 0
				for _, c := range in.resourcesOfType("docker_container") {
					if c.Status != "running" {
						continue
					}
					if lim, _ := attrFloat(c.Attributes, "memory_limit"); lim == 0 {
						noLimit++
					}
				}
				if noLimit > 0 {
					emit(Finding{
						ID: "no-mem-limit", Severity: SeverityInfo, CVSS: 3.5, CWE: "CWE-770",
						Title:          fmt.Sprintf("%d running containers have no memory limit", noLimit),
						Detail:         "Without a limit, one container can exhaust host memory (a noisy-neighbour / DoS risk).",
						Recommendation: "Set a memory limit per service (mem_limit / deploy.resources.limits).",
					})
				}
			},
		},
		{
			meta: Check{ID: "ssh-hardening", Category: "infra-config", Kind: KindPassive, Name: "SSH Hardening", OWASP: owaspA05,
				Description: "sshd policy — root login and password authentication."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, cfg := range in.resourcesOfType("ssh_config") {
					if attrString(cfg.Attributes, "permit_root_login") == "yes" {
						emit(Finding{
							ID: "ssh-root", Severity: SeverityMedium, CVSS: 6.5, CWE: "CWE-284",
							Title: "Root SSH login is permitted", Resource: "sshd_config",
							Detail:         "PermitRootLogin is 'yes' — remote root logins widen the blast radius of a compromised key or password.",
							Recommendation: "Set PermitRootLogin prohibit-password (or no) and use a sudo user.",
						})
					}
					if attrString(cfg.Attributes, "password_authentication") == "yes" {
						emit(Finding{
							ID: "ssh-passauth", Severity: SeverityLow, CVSS: 3.7, CWE: "CWE-307",
							Title: "SSH password authentication is enabled", Resource: "sshd_config",
							Detail:         "PasswordAuthentication 'yes' allows brute-forceable password logins.",
							Recommendation: "Prefer key-based auth: set PasswordAuthentication no.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "cipher-suite", Category: "crypto-transport", Kind: KindPassive, Name: "SSH Cipher Suite", OWASP: owaspA02,
				Description: "sshd should offer only modern ciphers, MACs and key exchanges."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, cfg := range in.resourcesOfType("ssh_config") {
					weak := append(append(attrStrings(cfg.Attributes, "weak_ciphers"), attrStrings(cfg.Attributes, "weak_macs")...), attrStrings(cfg.Attributes, "weak_kex")...)
					if len(weak) > 0 {
						emit(Finding{
							ID: "ssh-weakcrypto", Severity: SeverityMedium, CVSS: 5.9, CWE: "CWE-327",
							Title: "Weak SSH ciphers/MACs/KEX enabled", Resource: "sshd_config",
							Detail:         "The SSH daemon offers weak algorithms: " + strings.Join(weak, ", ") + ".",
							Evidence:       strings.Join(weak, ", "),
							Recommendation: "Restrict Ciphers/MACs/KexAlgorithms to modern suites (chacha20-poly1305, aes-gcm, curve25519).",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "shared-ssh-keys", Category: "secrets-creds", Kind: KindPassive, Name: "Shared SSH Keys", OWASP: owaspA07,
				Description: "A key authorized for multiple users can't be attributed or revoked cleanly."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, k := range in.resourcesOfType("ssh_keys") {
					if attrBool(k.Attributes, "shared") {
						emit(Finding{
							ID: "shared-keys", Severity: SeverityMedium, CVSS: 5.0, CWE: "CWE-522",
							Title: "One SSH key is authorised for multiple users", Resource: "authorized_keys",
							Detail:         "A shared key means access can't be attributed or revoked per-person (" + strings.Join(attrStrings(k.Attributes, "shared_keys"), "; ") + ").",
							Recommendation: "Give each person their own key and remove shared entries.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "privileged-flag", Category: "infra-config", Kind: KindPassive, Name: "Privileged Containers", OWASP: owaspA05,
				Description: "Privileged containers can access all host devices and escape isolation."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, c := range in.resourcesOfType("docker_container") {
					if attrBool(c.Attributes, "privileged") {
						emit(Finding{
							ID: "priv-" + c.Name, Severity: SeverityCritical, CVSS: 8.8, CWE: "CWE-250",
							Title: c.Name + " runs in privileged mode", Resource: c.Name,
							Detail:         "A privileged container can access all host devices and effectively escape isolation.",
							Recommendation: "Drop --privileged; grant only the specific capabilities the container needs.",
						})
					}
					if attrString(c.Attributes, "ipc_mode") == "host" {
						emit(Finding{
							ID: "ipc-" + c.Name, Severity: SeverityMedium, CVSS: 5.0, CWE: "CWE-668", Category: "infra-config",
							Title: c.Name + " shares the host IPC namespace", Resource: c.Name,
							Detail:         "--ipc=host exposes host shared memory to the container (and vice-versa).",
							Recommendation: "Remove --ipc=host unless the workload genuinely needs host shared memory.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "blank-passwords", Category: "secrets-creds", Kind: KindPassive, Name: "Blank Passwords", OWASP: owaspA07,
				Description: "Empty passwords let anyone authenticate."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, cfg := range in.resourcesOfType("ssh_config") {
					if attrString(cfg.Attributes, "permit_empty_passwords") == "yes" {
						emit(Finding{
							ID: "ssh-emptypass", Severity: SeverityCritical, CVSS: 9.8, CWE: "CWE-258",
							Title: "SSH permits empty passwords", Resource: "sshd_config",
							Detail:         "PermitEmptyPasswords 'yes' lets accounts with no password log in.",
							Recommendation: "Set PermitEmptyPasswords no immediately.",
						})
					}
				}
				for _, c := range in.resourcesOfType("docker_container") {
					if attrBool(c.Attributes, "blank_password") {
						emit(Finding{
							ID: "blank-" + c.Name, Severity: SeverityCritical, CVSS: 9.1, CWE: "CWE-258",
							Title: c.Name + " has a blank password in its environment", Resource: c.Name,
							Detail:         "A password environment variable is empty — the service may accept no password.",
							Recommendation: "Set a strong password (and prefer secrets over env vars).",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "default-credentials", Category: "secrets-creds", Kind: KindPassive, Name: "Default Credentials", OWASP: owaspA07,
				Description: "Well-known default passwords are trivially guessed."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				for _, c := range in.resourcesOfType("docker_container") {
					if creds := attrStrings(c.Attributes, "weak_credentials"); len(creds) > 0 {
						emit(Finding{
							ID: "weakcred-" + c.Name, Severity: SeverityHigh, CVSS: 8.0, CWE: "CWE-1392",
							Title: c.Name + " uses a weak/default credential", Resource: c.Name,
							Detail:         "These password variables are set to a well-known default value: " + strings.Join(creds, ", ") + ".",
							Evidence:       strings.Join(creds, ", "),
							Recommendation: "Rotate to strong, unique secrets.",
						})
					}
				}
			},
		},
		{
			meta: Check{ID: "nginx-headers", Category: "client-side", Kind: KindPassive, Name: "Reverse-Proxy Headers (config)", OWASP: owaspA05,
				Description: "Security headers declared in the nginx vhost configuration."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				missing := 0
				for _, vh := range in.resourcesOfType("nginx_vhost") {
					if !attrBool(vh.Attributes, "ssl") {
						continue
					}
					var lack []string
					if !attrBool(vh.Attributes, "has_hsts") {
						lack = append(lack, "HSTS")
					}
					if !attrBool(vh.Attributes, "has_xframe") {
						lack = append(lack, "X-Frame-Options")
					}
					if !attrBool(vh.Attributes, "has_csp") {
						lack = append(lack, "Content-Security-Policy")
					}
					if len(lack) > 0 {
						missing++
						if missing <= 6 {
							emit(Finding{
								ID: "hdr-cfg-" + vh.Name, Severity: SeverityLow, CVSS: 3.1, CWE: "CWE-693",
								Title: "Missing security headers on " + vh.Name, Resource: vh.Name,
								Detail:         vh.Name + " does not set: " + strings.Join(lack, ", ") + " in its nginx config.",
								Recommendation: "Add the missing add_header directives (HSTS, X-Frame-Options, CSP).",
							})
						}
					}
				}
			},
		},
		{
			meta: Check{ID: "server-tokens", Category: "info-disclosure", Kind: KindPassive, Name: "Version Disclosure (config)", OWASP: owaspA05,
				Description: "server_tokens should be off so responses don't leak the nginx version."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				on := 0
				for _, vh := range in.resourcesOfType("nginx_vhost") {
					if attrBool(vh.Attributes, "ssl") && !attrBool(vh.Attributes, "server_tokens_off") {
						on++
					}
				}
				if on > 0 {
					emit(Finding{
						ID: "server-tokens", Severity: SeverityInfo, CVSS: 2.5, CWE: "CWE-200",
						Title:          "Nginx exposes its version",
						Detail:         fmt.Sprintf("%d vhost(s) don't set 'server_tokens off', so responses leak the nginx version.", on),
						Recommendation: "Add 'server_tokens off;' in the http{} block.",
					})
				}
			},
		},
		{
			meta: Check{ID: "rate-limiting-cfg", Category: "api-security", Kind: KindPassive, Name: "Rate Limiting (config)", OWASP: owaspA04,
				Description: "Public vhosts without limit_req leave login/API endpoints open to brute-force."},
			run: func(ctx context.Context, in *Input, emit emitFunc, log logFunc) {
				none := 0
				for _, vh := range in.resourcesOfType("nginx_vhost") {
					if attrBool(vh.Attributes, "ssl") && !attrBool(vh.Attributes, "has_rate_limit") {
						none++
					}
				}
				if none > 0 {
					emit(Finding{
						ID: "no-ratelimit", Severity: SeverityLow, CVSS: 3.7, CWE: "CWE-770",
						Title:          "No rate limiting on public vhosts",
						Detail:         fmt.Sprintf("%d TLS vhost(s) have no limit_req — login/API endpoints are open to brute-force and abuse.", none),
						Recommendation: "Define a limit_req zone and apply it to sensitive locations.",
					})
				}
			},
		},
	}
}
