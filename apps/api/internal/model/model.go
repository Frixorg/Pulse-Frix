// Package model holds the control-plane domain entities. See docs/DATA_MODEL.md.
package model

import "time"

type Health string

const (
	HealthHealthy  Health = "HEALTHY"
	HealthDegraded Health = "DEGRADED"
	HealthDown     Health = "DOWN"
	HealthUnknown  Health = "UNKNOWN"
)

type Severity string

const (
	SevInfo     Severity = "INFO"
	SevWarning  Severity = "WARNING"
	SevCritical Severity = "CRITICAL"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleViewer Role = "viewer"
)

// Organization is the tenant root.
type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// User is an account. PasswordHash and MFASecret are never serialised to JSON.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	PasswordHash string    `json:"-"`
	MFASecret    string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// Membership binds a user to an org with a role.
type Membership struct {
	OrgID  string `json:"org_id"`
	UserID string `json:"user_id"`
	Role   Role   `json:"role"`
}

// Server is a monitored VPS.
type Server struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"org_id"`
	ServerID   string    `json:"server_id"` // public opaque id
	Hostname   string    `json:"hostname"`
	Mode       string    `json:"mode"`
	Status     Health    `json:"status"`
	LastSeenAt time.Time `json:"last_seen_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// Agent is the identity of the binary running on a server.
type Agent struct {
	ID              string     `json:"id"`
	OrgID           string     `json:"org_id"`
	ServerID        string     `json:"server_id"`
	AgentID         string     `json:"agent_id"`
	PublicKey       string     `json:"-"` // ed25519 public key (base64)
	ProtocolVersion string     `json:"protocol_version"`
	RevokedAt       *time.Time `json:"revoked_at,omitempty"`
	LastSeenAt      time.Time  `json:"last_seen_at"`
}

// EnrollmentToken is a short-lived, single-use enrollment credential. Only the
// hash is stored.
type EnrollmentToken struct {
	ID          string     `json:"id"`
	OrgID       string     `json:"org_id"`
	TokenHash   string     `json:"-"`
	ExpiresAt   time.Time  `json:"expires_at"`
	UsedAt      *time.Time `json:"used_at,omitempty"`
	Fingerprint string     `json:"fingerprint,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Service is a discovered service (already redacted).
type Service struct {
	ID       string         `json:"id"`
	OrgID    string         `json:"org_id"`
	ServerID string         `json:"server_id"`
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Status   Health         `json:"status"`
	Metadata map[string]any `json:"metadata,omitempty"`
	SeenAt   time.Time      `json:"seen_at"`
}

// Domain is a discovered domain + TLS state.
type Domain struct {
	ID           string     `json:"id"`
	OrgID        string     `json:"org_id"`
	ServerID     string     `json:"server_id"`
	FQDN         string     `json:"fqdn"`
	HTTPStatus   int        `json:"http_status,omitempty"`
	LatencyMS    int        `json:"latency_ms,omitempty"`
	TLSValid     bool       `json:"tls_valid"`
	TLSExpiresAt *time.Time `json:"tls_expires_at,omitempty"`
	TLSDaysLeft  int        `json:"tls_days_left,omitempty"`
	Health       Health     `json:"health"`
}

// Alert is an alert rule.
type Alert struct {
	ID              string   `json:"id"`
	OrgID           string   `json:"org_id"`
	Name            string   `json:"name"`
	Expr            string   `json:"expr"`
	Severity        Severity `json:"severity"`
	ForSeconds      int      `json:"for_seconds"`
	CooldownSeconds int      `json:"cooldown_seconds"`
	Enabled         bool     `json:"enabled"`
}

// AlertInstance is a firing/resolved occurrence of an alert.
type AlertInstance struct {
	ID         string     `json:"id"`
	OrgID      string     `json:"org_id"`
	AlertID    string     `json:"alert_id"`
	Name       string     `json:"name"`
	Severity   Severity   `json:"severity"`
	ServerID   string     `json:"server_id"`
	State      string     `json:"state"` // firing | resolved
	DedupKey   string     `json:"dedup_key"`
	RootCause  string     `json:"root_cause,omitempty"`
	Affected   []string   `json:"affected,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
}

// Event is a recorded state change.
type Event struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	ServerID  string    `json:"server_id"`
	Severity  Severity  `json:"severity"`
	Source    string    `json:"source"`
	Event     string    `json:"event"`
	PrevState string    `json:"prev_state,omitempty"`
	NewState  string    `json:"new_state,omitempty"`
	TS        time.Time `json:"ts"`
}

// AuditEntry records a sensitive operation.
type AuditEntry struct {
	ID       string         `json:"id"`
	OrgID    string         `json:"org_id"`
	Actor    string         `json:"actor"`
	Action   string         `json:"action"`
	Result   string         `json:"result"`
	IP       string         `json:"ip,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	TS       time.Time      `json:"ts"`
}

// Session is a web session.
type Session struct {
	ID         string    `json:"-"`
	UserID     string    `json:"user_id"`
	OrgID      string    `json:"org_id"`
	CSRFSecret string    `json:"-"`
	ExpiresAt  time.Time `json:"expires_at"`
}
