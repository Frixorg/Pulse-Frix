// Package redact implements the mandatory secret-redaction layer.
//
// Redaction runs in the AGENT, at the source, before any discovery output is
// logged, stored, transmitted, or displayed. The discovery engine MUST NEVER
// send raw secrets to the dashboard. See docs/PRIVACY.md and golden rule #6/#18.
package redact

import (
	"regexp"
	"strings"

	"github.com/frix-me/pulse/agent/internal/model"
)

// Placeholder is what replaces any redacted secret.
const Placeholder = "***REDACTED***"

// secretKey matches environment-variable / label keys whose *values* are
// sensitive and must be redacted entirely.
var secretKey = regexp.MustCompile(`(?i)(pass|password|passwd|secret|token|apikey|api_key|access_key|accesskey|private|credential|auth|session|salt|passphrase|dsn|connection_string|conn_str|client_secret|encryption|signing|jwt|bearer|cookie)`)

// nonSecretAllow prevents over-redaction of obviously non-sensitive keys that
// merely contain a matched substring (e.g. AUTHOR, AUTH_METHOD, TOKENIZER).
var nonSecretAllow = regexp.MustCompile(`(?i)^(author|authority|auth_method|auth_type|token_type|tokenizer|session_timeout|session_name|public_key|keycloak_realm)$`)

var (
	// URL credentials: scheme://user:password@host  ->  redact the password.
	urlCreds = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^:@/\s]+:)([^@/\s]+)(@)`)
	// JSON Web Tokens.
	jwt = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}\.[A-Za-z0-9_-]{5,}`)
	// PEM private-key blocks (any type).
	pemBlock = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	// Common high-signal token formats.
	knownTokens = regexp.MustCompile(`(?i)\b(sk-[A-Za-z0-9]{16,}|xox[baprs]-[A-Za-z0-9-]{10,}|ghp_[A-Za-z0-9]{20,}|gho_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|ASIA[0-9A-Z]{16}|AIza[0-9A-Za-z\-_]{20,}|glpat-[A-Za-z0-9_\-]{16,})\b`)
	// key=value / key: value where the key looks secret (single-line).
	inlineAssign = regexp.MustCompile(`(?i)\b([A-Z0-9_]*(?:pass|password|secret|token|apikey|api_key|access_key|private_key|credential|passphrase|client_secret)[A-Z0-9_]*)(\s*[:=]\s*)("?)([^\s"']+)("?)`)
)

// String redacts secrets that may appear anywhere inside a free-form string
// (connection strings, tokens, PEM blocks, JWTs, inline assignments).
func String(s string) string {
	if s == "" {
		return s
	}
	s = pemBlock.ReplaceAllString(s, Placeholder)
	s = urlCreds.ReplaceAllString(s, "${1}"+Placeholder+"${3}")
	s = jwt.ReplaceAllString(s, Placeholder)
	s = knownTokens.ReplaceAllString(s, Placeholder)
	s = inlineAssign.ReplaceAllString(s, "${1}${2}${3}"+Placeholder+"${5}")
	return s
}

// KeyValue redacts the value of a single key/value pair. If the key looks
// sensitive the whole value is replaced; otherwise the value is still scrubbed
// for embedded secrets (e.g. a URL with a password).
func KeyValue(key, value string) string {
	k := strings.TrimSpace(key)
	if secretKey.MatchString(k) && !nonSecretAllow.MatchString(k) {
		return Placeholder
	}
	return String(value)
}

// EnvSlice redacts a slice of "KEY=VALUE" environment entries.
func EnvSlice(env []string) []string {
	out := make([]string, len(env))
	for i, e := range env {
		if idx := strings.IndexByte(e, '='); idx >= 0 {
			key, val := e[:idx], e[idx+1:]
			out[i] = key + "=" + KeyValue(key, val)
		} else {
			out[i] = String(e)
		}
	}
	return out
}

// Map redacts a string map in place-safe manner (returns a new map).
func Map(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = KeyValue(k, v)
	}
	return out
}

// Any recursively redacts an arbitrary JSON-like value.
func Any(v any) any {
	switch t := v.(type) {
	case string:
		return String(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if s, ok := val.(string); ok {
				out[k] = KeyValue(k, s)
			} else {
				out[k] = Any(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Any(val)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, s := range t {
			out[i] = String(s)
		}
		return out
	default:
		return v
	}
}

// Resource returns a redacted copy of a discovered resource.
func Resource(r model.Resource) model.Resource {
	r.Name = String(r.Name)
	r.Labels = Map(r.Labels)
	if r.Attributes != nil {
		attrs := make(map[string]any, len(r.Attributes))
		for k, v := range r.Attributes {
			if s, ok := v.(string); ok {
				attrs[k] = KeyValue(k, s)
			} else {
				attrs[k] = Any(v)
			}
		}
		r.Attributes = attrs
	}
	for i, vol := range r.Volumes {
		r.Volumes[i] = String(vol)
	}
	return r
}

// Snapshot returns a redacted copy of a full discovery snapshot. This is the
// single choke point every snapshot passes through before leaving the agent.
func Snapshot(s *model.Snapshot) *model.Snapshot {
	if s == nil {
		return nil
	}
	for i := range s.Resources {
		s.Resources[i] = Resource(s.Resources[i])
	}
	return s
}
