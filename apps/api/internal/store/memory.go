package store

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/model"
)

// Memory is a thread-safe in-memory Store for development and tests. It is NOT
// durable. Production uses the PostgreSQL adapter (build tag `pgx`).
type Memory struct {
	mu sync.RWMutex

	orgs        map[string]*model.Organization
	users       map[string]*model.User            // userID -> user
	usersByMail map[string]string                 // email -> userID
	oidcByKey   map[string]string                 // "provider:subject" -> userID
	memberships map[string]*model.Membership      // userID -> membership (one org in v1)
	sessions    map[string]*model.Session         // sessionID -> session
	servers     map[string]*model.Server          // id -> server
	agents      map[string]*model.Agent           // id -> agent
	agentByAID  map[string]string                 // agentID -> id
	enrollments map[string]*model.EnrollmentToken // tokenHash -> token
	discovery   map[string]json.RawMessage        // orgID/serverID -> snapshot
	metrics     map[string]json.RawMessage        // orgID/serverID -> sample
	metricHist  map[string][]MetricSample         // orgID/serverID -> history
	alerts      map[string]*model.Alert           // id -> alert
	instances   map[string]*model.AlertInstance   // id -> instance
	events      []*model.Event
	audit       []*model.AuditEntry
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{
		orgs:        map[string]*model.Organization{},
		users:       map[string]*model.User{},
		usersByMail: map[string]string{},
		oidcByKey:   map[string]string{},
		memberships: map[string]*model.Membership{},
		sessions:    map[string]*model.Session{},
		servers:     map[string]*model.Server{},
		agents:      map[string]*model.Agent{},
		agentByAID:  map[string]string{},
		enrollments: map[string]*model.EnrollmentToken{},
		discovery:   map[string]json.RawMessage{},
		metrics:     map[string]json.RawMessage{},
		metricHist:  map[string][]MetricSample{},
		alerts:      map[string]*model.Alert{},
		instances:   map[string]*model.AlertInstance{},
	}
}

func key(orgID, serverID string) string { return orgID + "/" + serverID }

// --- auth ---

func (m *Memory) FindLoginByEmail(email string) (*model.User, *model.Membership, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	uid, ok := m.usersByMail[strings.ToLower(email)]
	if !ok {
		return nil, nil, ErrNotFound
	}
	return m.users[uid], m.memberships[uid], nil
}

func (m *Memory) SeedOrgOwner(orgName, email, passwordHash string) (*model.Organization, *model.User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	email = strings.ToLower(email)
	if _, exists := m.usersByMail[email]; exists {
		return nil, nil, ErrAlreadyExists
	}
	org := &model.Organization{ID: auth.NewID("org"), Name: orgName, CreatedAt: time.Now().UTC()}
	user := &model.User{ID: auth.NewID("usr"), Email: email, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	m.orgs[org.ID] = org
	m.users[user.ID] = user
	m.usersByMail[email] = user.ID
	m.memberships[user.ID] = &model.Membership{OrgID: org.ID, UserID: user.ID, Role: model.RoleOwner}
	return org, user, nil
}

func (m *Memory) UpsertOIDCUser(provider, subject, email, name string) (*model.User, *model.Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := provider + ":" + subject

	// Existing OIDC identity → return it.
	if uid, ok := m.oidcByKey[key]; ok {
		return m.users[uid], m.memberships[uid], nil
	}
	// Existing account by email → link this identity to it.
	if email != "" {
		if uid, ok := m.usersByMail[strings.ToLower(email)]; ok {
			m.oidcByKey[key] = uid
			return m.users[uid], m.memberships[uid], nil
		}
	}

	// New tenant: org + owner user.
	orgName := firstNonEmpty(name, email, "Personal")
	org := &model.Organization{ID: auth.NewID("org"), Name: orgName, CreatedAt: time.Now().UTC()}
	user := &model.User{ID: auth.NewID("usr"), Email: strings.ToLower(email), Name: name, CreatedAt: time.Now().UTC()}
	m.orgs[org.ID] = org
	m.users[user.ID] = user
	if email != "" {
		m.usersByMail[strings.ToLower(email)] = user.ID
	}
	m.oidcByKey[key] = user.ID
	mem := &model.Membership{OrgID: org.ID, UserID: user.ID, Role: model.RoleOwner}
	m.memberships[user.ID] = mem
	return user, mem, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func (m *Memory) GetMembership(orgID, userID string) (*model.Membership, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	mem, ok := m.memberships[userID]
	if !ok || mem.OrgID != orgID {
		return nil, ErrNotFound
	}
	cp := *mem
	return &cp, nil
}

func (m *Memory) GetUser(orgID, userID string) (*model.User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	u, ok := m.users[userID]
	if !ok {
		return nil, ErrNotFound
	}
	mem := m.memberships[userID]
	if mem == nil || mem.OrgID != orgID {
		return nil, ErrNotFound
	}
	return u, nil
}

func (m *Memory) CreateSession(s *model.Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[s.ID] = s
	return nil
}

func (m *Memory) GetSession(id string) (*model.Session, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	if time.Now().After(s.ExpiresAt) {
		return nil, ErrNotFound
	}
	return s, nil
}

func (m *Memory) DeleteSession(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

// --- servers & agents ---

func (m *Memory) ListServers(orgID string) ([]model.Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Server
	for _, s := range m.servers {
		if s.OrgID == orgID {
			out = append(out, *s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out, nil
}

func (m *Memory) GetServer(orgID, id string) (*model.Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.servers[id]
	if !ok || s.OrgID != orgID {
		return nil, ErrNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *Memory) UpsertServer(orgID string, s *model.Server) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.OrgID = orgID
	if s.ID == "" {
		s.ID = auth.NewID("srv")
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now().UTC()
	}
	m.servers[s.ID] = s
	return nil
}

func (m *Memory) DeleteServer(orgID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.servers[id]
	if !ok || s.OrgID != orgID {
		return ErrNotFound
	}
	delete(m.servers, id)
	delete(m.discovery, key(orgID, s.ServerID))
	delete(m.metrics, key(orgID, s.ServerID))
	delete(m.metricHist, key(orgID, s.ServerID))
	return nil
}

func (m *Memory) TouchServerSeen(orgID, serverID string, now time.Time, status model.Health) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.OrgID == orgID && s.ServerID == serverID {
			s.LastSeenAt = now
			s.Status = status
			return nil
		}
	}
	return ErrNotFound
}

func (m *Memory) EnsureServer(orgID, serverID, hostname string, now time.Time, status model.Health) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.servers {
		if s.OrgID == orgID && s.ServerID == serverID {
			s.LastSeenAt = now
			s.Status = status
			if hostname != "" {
				s.Hostname = hostname
			}
			return nil
		}
	}
	srv := &model.Server{
		ID: auth.NewID("srv"), OrgID: orgID, ServerID: serverID, Hostname: hostname,
		Mode: "cloud", Status: status, LastSeenAt: now, CreatedAt: now,
	}
	m.servers[srv.ID] = srv
	return nil
}

func (m *Memory) CreateEnrollmentToken(t *model.EnrollmentToken) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.enrollments[t.TokenHash] = t
	return nil
}

func (m *Memory) ConsumeEnrollmentToken(hash string, now time.Time) (*model.EnrollmentToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.enrollments[hash]
	if !ok {
		return nil, ErrNotFound
	}
	if t.UsedAt != nil || now.After(t.ExpiresAt) {
		return nil, ErrNotFound // single-use + expiry
	}
	used := now
	t.UsedAt = &used
	return t, nil
}

func (m *Memory) CreateAgent(a *model.Agent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = auth.NewID("agt")
	}
	m.agents[a.ID] = a
	m.agentByAID[a.AgentID] = a.ID
	return nil
}

func (m *Memory) GetAgentByAgentID(agentID string) (*model.Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.agentByAID[agentID]
	if !ok {
		return nil, ErrNotFound
	}
	a := m.agents[id]
	if a.RevokedAt != nil {
		return nil, ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (m *Memory) RevokeAgent(orgID, id string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.agents[id]
	if !ok || a.OrgID != orgID {
		return ErrNotFound
	}
	a.RevokedAt = &now
	return nil
}

// --- discovery & metrics ---

func (m *Memory) SaveDiscovery(orgID, serverID string, snapshot json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.discovery[key(orgID, serverID)] = snapshot
	return nil
}

func (m *Memory) GetDiscovery(orgID, serverID string) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	raw, ok := m.discovery[key(orgID, serverID)]
	if !ok {
		return nil, ErrNotFound
	}
	return raw, nil
}

func (m *Memory) SaveMetrics(orgID, serverID string, sample json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.metrics[key(orgID, serverID)] = sample
	return nil
}

func (m *Memory) GetMetrics(orgID, serverID string) (json.RawMessage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	raw, ok := m.metrics[key(orgID, serverID)]
	if !ok {
		return nil, ErrNotFound
	}
	return raw, nil
}

func (m *Memory) AppendMetricSample(orgID, serverID string, ts time.Time, sample json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := key(orgID, serverID)
	cp := make(json.RawMessage, len(sample))
	copy(cp, sample)
	h := append(m.metricHist[k], MetricSample{TS: ts, Sample: cp})
	const maxPoints = 10000 // bound memory (dev store)
	if len(h) > maxPoints {
		h = h[len(h)-maxPoints:]
	}
	m.metricHist[k] = h
	return nil
}

func (m *Memory) QueryMetricHistory(orgID, serverID string, since time.Time) ([]MetricSample, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	h := m.metricHist[key(orgID, serverID)]
	out := make([]MetricSample, 0, len(h))
	for _, s := range h {
		if !s.TS.Before(since) {
			out = append(out, s)
		}
	}
	return out, nil
}

// --- alerts, events, audit ---

func (m *Memory) ListAlerts(orgID string) ([]model.Alert, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Alert
	for _, a := range m.alerts {
		if a.OrgID == orgID {
			out = append(out, *a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *Memory) CreateAlert(a *model.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a.ID == "" {
		a.ID = auth.NewID("alr")
	}
	m.alerts[a.ID] = a
	return nil
}

func (m *Memory) UpdateAlert(orgID string, a *model.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.alerts[a.ID]
	if !ok || existing.OrgID != orgID {
		return ErrNotFound
	}
	a.OrgID = orgID
	m.alerts[a.ID] = a
	return nil
}

func (m *Memory) ListAlertInstances(orgID string) ([]model.AlertInstance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.AlertInstance
	for _, i := range m.instances {
		if i.OrgID == orgID {
			out = append(out, *i)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (m *Memory) UpsertAlertInstance(i *model.AlertInstance) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i.ID == "" {
		i.ID = auth.NewID("ali")
	}
	m.instances[i.ID] = i
	return nil
}

func (m *Memory) AppendEvent(e *model.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = auth.NewID("evt")
	}
	m.events = append(m.events, e)
	return nil
}

func (m *Memory) ListEvents(orgID string, limit int) ([]model.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.Event
	for i := len(m.events) - 1; i >= 0 && len(out) < limit; i-- {
		if m.events[i].OrgID == orgID {
			out = append(out, *m.events[i])
		}
	}
	return out, nil
}

func (m *Memory) AppendAudit(e *model.AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e.ID == "" {
		e.ID = auth.NewID("aud")
	}
	m.audit = append(m.audit, e)
	return nil
}

func (m *Memory) ListAudit(orgID string, limit int) ([]model.AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []model.AuditEntry
	for i := len(m.audit) - 1; i >= 0 && len(out) < limit; i-- {
		if m.audit[i].OrgID == orgID {
			out = append(out, *m.audit[i])
		}
	}
	return out, nil
}
