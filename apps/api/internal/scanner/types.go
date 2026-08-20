// Package scanner runs PulseFrix's non-destructive security assessment.
//
// It stays inside the product's "observe, never change" model: every check is
// either passive (reasoning over the agent's read-only discovery snapshot) or a
// safe, read-shaped HTTP probe against the server's own public endpoints. It
// does not exploit, fuzz destructively, execute shells, or run arbitrary code —
// findings that need real exploitation are reported as "potential" for a human
// to validate. All outbound requests go through the SSRF guard, so only public
// targets are ever reached.
package scanner

import (
	"net/http"
	"sync"
	"time"
)

// Severity levels, ordered most to least serious.
const (
	SeverityCritical = "CRITICAL"
	SeverityHigh     = "HIGH"
	SeverityMedium   = "MEDIUM"
	SeverityLow      = "LOW"
	SeverityInfo     = "INFO"
)

// Check kinds.
const (
	KindPassive = "passive" // reasons over the discovery snapshot, no network
	KindActive  = "active"  // safe HTTP probe against the server's public URLs
)

// Check status after a run.
const (
	StatusPass    = "pass"     // ran, nothing flagged
	StatusIssues  = "issues"   // ran, produced findings
	StatusError   = "error"    // the check itself failed to run
	StatusSkipped = "skipped"  // not applicable (e.g. active check, no targets)
	StatusNotRun  = "not_run"  // catalogue default, before any scan
)

// Scan lifecycle.
const (
	ScanQueued  = "queued"
	ScanRunning = "running"
	ScanDone    = "done"
	ScanError   = "error"
)

// Scan modes.
const (
	ModePassive = "passive"
	ModeActive  = "active"
	ModeFull    = "full"
)

// Log levels for the live scan console.
const (
	LogInfo    = "info"
	LogWarn    = "warn"
	LogError   = "error"
	LogSuccess = "success"
)

// Category groups checks along the OWASP-aligned lines the security page renders.
type Category struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

// Finding is a single risk observation. PulseFrix reports; it never remediates.
type Finding struct {
	ID             string   `json:"id"`
	CheckID        string   `json:"check_id"`
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	Title          string   `json:"title"`
	Resource       string   `json:"resource,omitempty"`
	Detail         string   `json:"detail"`
	Evidence       string   `json:"evidence,omitempty"`
	Recommendation string   `json:"recommendation"`
	CVSS           float64  `json:"cvss,omitempty"`
	OWASP          string   `json:"owasp,omitempty"`
	CWE            string   `json:"cwe,omitempty"`
	References     []string `json:"references,omitempty"`
}

// Check is one probe in the catalogue plus its per-scan outcome.
type Check struct {
	ID          string `json:"id"`
	Category    string `json:"category"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Kind        string `json:"kind"`
	OWASP       string `json:"owasp,omitempty"`

	// Outcome fields, populated during a scan.
	Status     string `json:"status"`
	Count      int    `json:"count"`
	Note       string `json:"note,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// LogEntry is one line in the live scan console.
type LogEntry struct {
	T     time.Time `json:"t"`
	Level string    `json:"level"`
	Check string    `json:"check,omitempty"`
	Msg   string    `json:"msg"`
}

// ScanState is the full, pollable state of a scan (also used as the "latest
// result" payload on the security page).
type ScanState struct {
	ID         string     `json:"id"`
	ServerID   string     `json:"server_id"`
	Status     string     `json:"status"`
	Mode       string     `json:"mode"`
	Categories []string   `json:"categories,omitempty"`
	Targets    []string   `json:"targets,omitempty"`
	Progress   float64    `json:"progress"`
	Current    string     `json:"current,omitempty"`
	Total      int        `json:"total"`
	Completed  int        `json:"completed"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Checks     []Check    `json:"checks"`
	Findings   []Finding  `json:"findings"`
	Logs       []LogEntry `json:"logs"`
	Error      string     `json:"error,omitempty"`
}

// Audit is what GET /security returns: the static catalogue plus the most
// recent scan for the server (nil until one has run).
type Audit struct {
	Categories []Category `json:"categories"`
	Checks     []Check    `json:"checks"`
	Latest     *ScanState `json:"latest,omitempty"`
}

// --- inputs the engine reasons over (mapped from the agent snapshot) ---

// Port mirrors the agent's listening-port shape.
type Port struct {
	Host      int    `json:"host,omitempty"`
	Container int    `json:"container,omitempty"`
	Protocol  string `json:"protocol,omitempty"`
	Address   string `json:"address,omitempty"`
	State     string `json:"state,omitempty"`
}

// Resource mirrors the fields of the discovery snapshot the checks need.
type Resource struct {
	Type       string
	Name       string
	Status     string
	Health     string
	Attributes map[string]any
	Ports      []Port
}

// Target is one public URL the active checks probe.
type Target struct {
	URL  string // e.g. https://app.example.com
	FQDN string
	TLS  bool
}

// Input bundles everything a scan runs against.
type Input struct {
	ServerID  string
	Hostname  string
	Resources []Resource
	Targets   []Target

	// Set by the manager before a scan; not serialized.
	httpClient *http.Client
	mu         sync.Mutex
	rootCache  map[string]*httpResult
}

func (in *Input) resourcesOfType(types ...string) []Resource {
	want := map[string]bool{}
	for _, t := range types {
		want[t] = true
	}
	var out []Resource
	for _, r := range in.Resources {
		if want[r.Type] {
			out = append(out, r)
		}
	}
	return out
}

// attrString / attrBool / attrFloat / attrStrings are small, nil-safe helpers
// for reading loosely-typed snapshot attributes.
func attrString(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func attrBool(m map[string]any, k string) bool {
	if m == nil {
		return false
	}
	b, _ := m[k].(bool)
	return b
}

func attrFloat(m map[string]any, k string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	f, ok := m[k].(float64)
	return f, ok
}

func attrStrings(m map[string]any, k string) []string {
	if m == nil {
		return nil
	}
	arr, ok := m[k].([]any)
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
