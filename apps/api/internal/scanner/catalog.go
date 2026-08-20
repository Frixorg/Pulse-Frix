package scanner

// Categories is the OWASP-aligned grouping the security page renders. Order is
// meaningful — it's the display order.
func Categories() []Category {
	return []Category{
		{
			ID:          "access-control",
			Name:        "Broken Access Control",
			Description: "IDOR, privilege escalation and authorization bypass — exposed admin surfaces and unauthenticated management endpoints.",
			Icon:        "lock-open",
		},
		{
			ID:          "injection",
			Name:        "Injection",
			Description: "SQL / NoSQL / OS-command / template injection — input that reaches an interpreter unsanitised.",
			Icon:        "syringe",
		},
		{
			ID:          "server-side",
			Name:        "Server-Side Vulnerabilities",
			Description: "SSRF, XXE, insecure deserialization and RCE — the server acting on attacker-controlled data.",
			Icon:        "server",
		},
		{
			ID:          "client-side",
			Name:        "Client-Side Attacks",
			Description: "XSS, CSRF, clickjacking and prototype pollution — attacks that execute in your users' browsers.",
			Icon:        "browser",
		},
		{
			ID:          "auth-session",
			Name:        "Authentication & Session",
			Description: "JWT weaknesses, session fixation and cookie handling — how identity is proven and kept.",
			Icon:        "key",
		},
		{
			ID:          "crypto-transport",
			Name:        "Cryptography & Transport",
			Description: "TLS validity, protocol/cipher strength, HSTS and HTTPS enforcement — data protected in transit.",
			Icon:        "shield",
		},
		{
			ID:          "infra-config",
			Name:        "Infrastructure & Configuration",
			Description: "Exposed services, container privileges, resource limits and SSH hardening — the host's attack surface.",
			Icon:        "cog",
		},
		{
			ID:          "secrets-creds",
			Name:        "Secrets & Credentials",
			Description: "Blank/default passwords, shared keys and exposed secret files (.env, .git) — credentials at rest.",
			Icon:        "vault",
		},
		{
			ID:          "info-disclosure",
			Name:        "Information Disclosure",
			Description: "Version banners, verbose errors and directory listing — details that help an attacker target you.",
			Icon:        "eye",
		},
		{
			ID:          "api-security",
			Name:        "API Security",
			Description: "CORS posture, dangerous HTTP methods, missing headers and rate limiting — the API perimeter.",
			Icon:        "plug",
		},
	}
}

// OWASP Top 10 (2021) short references, reused across findings.
const (
	owaspA01 = "A01:2021 Broken Access Control"
	owaspA02 = "A02:2021 Cryptographic Failures"
	owaspA03 = "A03:2021 Injection"
	owaspA04 = "A04:2021 Insecure Design"
	owaspA05 = "A05:2021 Security Misconfiguration"
	owaspA06 = "A06:2021 Vulnerable & Outdated Components"
	owaspA07 = "A07:2021 Identification & Authentication Failures"
	owaspA08 = "A08:2021 Software & Data Integrity Failures"
	owaspA09 = "A09:2021 Security Logging & Monitoring Failures"
	owaspA10 = "A10:2021 Server-Side Request Forgery"
)
