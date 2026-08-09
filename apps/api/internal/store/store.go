// Package store defines the control-plane persistence interface and ships an
// in-memory implementation used for development and tests. A PostgreSQL adapter
// is provided under the `pgx` build tag (see postgres_pgx.go and ./migrations).
//
// Every tenant-scoped method takes an orgID; there is deliberately NO "get by
// id without scope" method, so cross-tenant access is impossible by
// construction. See docs/DATA_MODEL.md#multi-tenancy.
package store

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/frix-me/pulse/api/internal/model"
)

// ErrNotFound is returned when a scoped lookup misses.
var ErrNotFound = errors.New("not found")

// ErrAlreadyExists is returned when creating a duplicate (e.g. an email).
var ErrAlreadyExists = errors.New("already exists")

// Store is the control-plane persistence interface.
type Store interface {
	// --- auth ---
	FindLoginByEmail(email string) (*model.User, *model.Membership, error)
	SeedOrgOwner(orgName, email, passwordHash string) (*model.Organization, *model.User, error)
	GetUser(orgID, userID string) (*model.User, error)
	// GetMembership resolves a user's role in an org by user id (not email), so
	// it works for identity-provider users who share no email address.
	GetMembership(orgID, userID string) (*model.Membership, error)

	// UpsertOIDCUser finds the user for (provider, subject) or, on first login,
	// creates a new tenant (org + owner user). Email may be empty.
	UpsertOIDCUser(provider, subject, email, name string) (*model.User, *model.Membership, error)

	CreateSession(s *model.Session) error
	GetSession(id string) (*model.Session, error)
	DeleteSession(id string) error

	// --- servers & agents ---
	ListServers(orgID string) ([]model.Server, error)
	GetServer(orgID, id string) (*model.Server, error)
	UpsertServer(orgID string, s *model.Server) error
	DeleteServer(orgID, id string) error
	TouchServerSeen(orgID, serverID string, now time.Time, status model.Health) error

	CreateEnrollmentToken(t *model.EnrollmentToken) error
	ConsumeEnrollmentToken(hash string, now time.Time) (*model.EnrollmentToken, error)
	CreateAgent(a *model.Agent) error
	GetAgentByAgentID(agentID string) (*model.Agent, error)
	RevokeAgent(orgID, id string, now time.Time) error

	// --- discovery & metrics (latest snapshot per server) ---
	SaveDiscovery(orgID, serverID string, snapshot json.RawMessage) error
	GetDiscovery(orgID, serverID string) (json.RawMessage, error)
	SaveMetrics(orgID, serverID string, sample json.RawMessage) error
	GetMetrics(orgID, serverID string) (json.RawMessage, error)

	// --- alerts, events, audit ---
	ListAlerts(orgID string) ([]model.Alert, error)
	CreateAlert(a *model.Alert) error
	UpdateAlert(orgID string, a *model.Alert) error
	ListAlertInstances(orgID string) ([]model.AlertInstance, error)
	UpsertAlertInstance(i *model.AlertInstance) error

	AppendEvent(e *model.Event) error
	ListEvents(orgID string, limit int) ([]model.Event, error)

	AppendAudit(e *model.AuditEntry) error
	ListAudit(orgID string, limit int) ([]model.AuditEntry, error)
}
