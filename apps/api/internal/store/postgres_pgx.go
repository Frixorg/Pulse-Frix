//go:build pgx

// PostgreSQL-backed Store. Compiled only with `-tags pgx`. Requires:
//
//	go get github.com/jackc/pgx/v5
//
// Apply migrations first (see ./migrations): psql "$DATABASE_URL" -f migrations/0001_init.sql
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/model"
)

// Postgres implements Store against PostgreSQL.
type Postgres struct{ db *sql.DB }

// NewPostgres opens a connection pool and verifies connectivity.
func NewPostgres(url string) (*Postgres, error) {
	if url == "" {
		return nil, errors.New("DATABASE_URL is empty")
	}
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)
	// Retry so a Postgres that is still starting doesn't crash-loop the API.
	// A persistent failure (e.g. wrong password) still surfaces a clear error.
	var lastErr error
	for i := 0; i < 15; i++ {
		if lastErr = db.Ping(); lastErr == nil {
			return &Postgres{db: db}, nil
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("postgres unreachable after retries: %w", lastErr)
}

func (p *Postgres) FindLoginByEmail(email string) (*model.User, *model.Membership, error) {
	u := &model.User{}
	err := p.db.QueryRow(`SELECT id,email,password_hash,created_at FROM users WHERE email=$1`, email).
		Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	m := &model.Membership{}
	err = p.db.QueryRow(`SELECT org_id,user_id,role FROM memberships WHERE user_id=$1 LIMIT 1`, u.ID).
		Scan(&m.OrgID, &m.UserID, &m.Role)
	if err != nil {
		return u, nil, nil
	}
	return u, m, nil
}

func (p *Postgres) SeedOrgOwner(orgName, email, passwordHash string) (*model.Organization, *model.User, error) {
	tx, err := p.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	org := &model.Organization{ID: auth.NewID("org"), Name: orgName, CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(`INSERT INTO organizations(id,name,created_at) VALUES($1,$2,$3)`, org.ID, org.Name, org.CreatedAt); err != nil {
		return nil, nil, err
	}
	u := &model.User{ID: auth.NewID("usr"), Email: email, PasswordHash: passwordHash, CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(`INSERT INTO users(id,email,password_hash,created_at) VALUES($1,$2,$3,$4)`, u.ID, u.Email, u.PasswordHash, u.CreatedAt); err != nil {
		return nil, nil, ErrAlreadyExists
	}
	if _, err := tx.Exec(`INSERT INTO memberships(org_id,user_id,role) VALUES($1,$2,'owner')`, org.ID, u.ID); err != nil {
		return nil, nil, err
	}
	return org, u, tx.Commit()
}

func (p *Postgres) UpsertOIDCUser(provider, subject, email, name string) (*model.User, *model.Membership, error) {
	// Existing OIDC identity.
	var uid string
	err := p.db.QueryRow(`SELECT user_id FROM oidc_identities WHERE provider=$1 AND subject=$2`, provider, subject).Scan(&uid)
	if err == nil {
		return p.userWithMembership(uid)
	}
	// Existing account by email → link identity.
	if email != "" {
		if e := p.db.QueryRow(`SELECT id FROM users WHERE email=$1`, strings.ToLower(email)).Scan(&uid); e == nil {
			_, _ = p.db.Exec(`INSERT INTO oidc_identities(provider,subject,user_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, provider, subject, uid)
			return p.userWithMembership(uid)
		}
	}
	// New tenant.
	tx, err := p.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	orgName := name
	if orgName == "" {
		orgName = email
	}
	if orgName == "" {
		orgName = "Personal"
	}
	org := &model.Organization{ID: auth.NewID("org"), Name: orgName, CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(`INSERT INTO organizations(id,name,created_at) VALUES($1,$2,$3)`, org.ID, org.Name, org.CreatedAt); err != nil {
		return nil, nil, err
	}
	u := &model.User{ID: auth.NewID("usr"), Email: strings.ToLower(email), Name: name, CreatedAt: time.Now().UTC()}
	if _, err := tx.Exec(`INSERT INTO users(id,email,name,password_hash,created_at) VALUES($1,NULLIF($2,''),$3,'',$4)`, u.ID, u.Email, u.Name, u.CreatedAt); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(`INSERT INTO memberships(org_id,user_id,role) VALUES($1,$2,'owner')`, org.ID, u.ID); err != nil {
		return nil, nil, err
	}
	if _, err := tx.Exec(`INSERT INTO oidc_identities(provider,subject,user_id) VALUES($1,$2,$3)`, provider, subject, u.ID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return u, &model.Membership{OrgID: org.ID, UserID: u.ID, Role: model.RoleOwner}, nil
}

// userWithMembership loads a user + its (first) membership by user id.
func (p *Postgres) userWithMembership(userID string) (*model.User, *model.Membership, error) {
	u := &model.User{}
	var email sql.NullString
	if err := p.db.QueryRow(`SELECT id,COALESCE(email,''),COALESCE(name,''),created_at FROM users WHERE id=$1`, userID).
		Scan(&u.ID, &email, &u.Name, &u.CreatedAt); err != nil {
		return nil, nil, err
	}
	u.Email = email.String
	m := &model.Membership{}
	if err := p.db.QueryRow(`SELECT org_id,user_id,role FROM memberships WHERE user_id=$1 LIMIT 1`, userID).
		Scan(&m.OrgID, &m.UserID, &m.Role); err != nil {
		return nil, nil, err
	}
	return u, m, nil
}

func (p *Postgres) GetMembership(orgID, userID string) (*model.Membership, error) {
	m := &model.Membership{}
	err := p.db.QueryRow(`SELECT org_id,user_id,role FROM memberships WHERE org_id=$1 AND user_id=$2`, orgID, userID).
		Scan(&m.OrgID, &m.UserID, &m.Role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (p *Postgres) GetUser(orgID, userID string) (*model.User, error) {
	u := &model.User{}
	// COALESCE guards against NULL email/name for identity-provider users.
	err := p.db.QueryRow(`
		SELECT u.id,COALESCE(u.email,''),COALESCE(u.name,''),u.password_hash,u.created_at
		FROM users u JOIN memberships m ON m.user_id=u.id
		WHERE u.id=$1 AND m.org_id=$2`, userID, orgID).
		Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (p *Postgres) CreateSession(s *model.Session) error {
	_, err := p.db.Exec(`INSERT INTO sessions(id,user_id,org_id,csrf_secret,expires_at) VALUES($1,$2,$3,$4,$5)`,
		s.ID, s.UserID, s.OrgID, s.CSRFSecret, s.ExpiresAt)
	return err
}

func (p *Postgres) GetSession(id string) (*model.Session, error) {
	s := &model.Session{}
	err := p.db.QueryRow(`SELECT id,user_id,org_id,csrf_secret,expires_at FROM sessions WHERE id=$1 AND expires_at>now()`, id).
		Scan(&s.ID, &s.UserID, &s.OrgID, &s.CSRFSecret, &s.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (p *Postgres) DeleteSession(id string) error {
	_, err := p.db.Exec(`DELETE FROM sessions WHERE id=$1`, id)
	return err
}

func (p *Postgres) ListServers(orgID string) ([]model.Server, error) {
	rows, err := p.db.Query(`SELECT id,org_id,server_id,hostname,mode,status,COALESCE(last_seen_at,to_timestamp(0)),created_at FROM servers WHERE org_id=$1 ORDER BY hostname`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Server
	for rows.Next() {
		var s model.Server
		if err := rows.Scan(&s.ID, &s.OrgID, &s.ServerID, &s.Hostname, &s.Mode, &s.Status, &s.LastSeenAt, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) GetServer(orgID, id string) (*model.Server, error) {
	s := &model.Server{}
	err := p.db.QueryRow(`SELECT id,org_id,server_id,hostname,mode,status,COALESCE(last_seen_at,to_timestamp(0)),created_at FROM servers WHERE org_id=$1 AND id=$2`, orgID, id).
		Scan(&s.ID, &s.OrgID, &s.ServerID, &s.Hostname, &s.Mode, &s.Status, &s.LastSeenAt, &s.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return s, err
}

func (p *Postgres) UpsertServer(orgID string, s *model.Server) error {
	if s.ID == "" {
		s.ID = auth.NewID("srv")
	}
	s.OrgID = orgID
	_, err := p.db.Exec(`
		INSERT INTO servers(id,org_id,server_id,hostname,mode,status,last_seen_at,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,now())
		ON CONFLICT (org_id,server_id) DO UPDATE SET hostname=EXCLUDED.hostname,mode=EXCLUDED.mode,status=EXCLUDED.status,last_seen_at=EXCLUDED.last_seen_at`,
		s.ID, orgID, s.ServerID, s.Hostname, s.Mode, s.Status, s.LastSeenAt)
	return err
}

func (p *Postgres) DeleteServer(orgID, id string) error {
	res, err := p.db.Exec(`DELETE FROM servers WHERE org_id=$1 AND id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) TouchServerSeen(orgID, serverID string, now time.Time, status model.Health) error {
	_, err := p.db.Exec(`UPDATE servers SET last_seen_at=$1,status=$2 WHERE org_id=$3 AND server_id=$4`, now, status, orgID, serverID)
	return err
}

func (p *Postgres) EnsureServer(orgID, serverID, hostname string, now time.Time, status model.Health) error {
	_, err := p.db.Exec(`
		INSERT INTO servers(id,org_id,server_id,hostname,mode,status,last_seen_at,created_at)
		VALUES($1,$2,$3,NULLIF($4,''),'cloud',$5,$6,now())
		ON CONFLICT (org_id,server_id) DO UPDATE SET
		  hostname=COALESCE(NULLIF(EXCLUDED.hostname,''), servers.hostname),
		  status=EXCLUDED.status,
		  last_seen_at=EXCLUDED.last_seen_at`,
		auth.NewID("srv"), orgID, serverID, hostname, status, now)
	return err
}

func (p *Postgres) CreateEnrollmentToken(t *model.EnrollmentToken) error {
	_, err := p.db.Exec(`INSERT INTO enrollment_tokens(org_id,token_hash,expires_at,fingerprint,created_at) VALUES($1,$2,$3,$4,now())`,
		t.OrgID, t.TokenHash, t.ExpiresAt, t.Fingerprint)
	return err
}

func (p *Postgres) ConsumeEnrollmentToken(hash string, now time.Time) (*model.EnrollmentToken, error) {
	t := &model.EnrollmentToken{}
	err := p.db.QueryRow(`
		UPDATE enrollment_tokens SET used_at=$1
		WHERE token_hash=$2 AND used_at IS NULL AND expires_at>$1
		RETURNING id,org_id,token_hash,expires_at`, now, hash).
		Scan(&t.ID, &t.OrgID, &t.TokenHash, &t.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (p *Postgres) CreateAgent(a *model.Agent) error {
	if a.ID == "" {
		a.ID = auth.NewID("agt")
	}
	_, err := p.db.Exec(`INSERT INTO agents(id,org_id,server_id,agent_id,public_key,protocol_version,last_seen_at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		a.ID, a.OrgID, a.ServerID, a.AgentID, a.PublicKey, a.ProtocolVersion, a.LastSeenAt)
	return err
}

func (p *Postgres) GetAgentByAgentID(agentID string) (*model.Agent, error) {
	a := &model.Agent{}
	var revoked sql.NullTime
	err := p.db.QueryRow(`SELECT id,org_id,server_id,agent_id,public_key,protocol_version,revoked_at,COALESCE(last_seen_at,to_timestamp(0)) FROM agents WHERE agent_id=$1`, agentID).
		Scan(&a.ID, &a.OrgID, &a.ServerID, &a.AgentID, &a.PublicKey, &a.ProtocolVersion, &revoked, &a.LastSeenAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if revoked.Valid {
		return nil, ErrNotFound
	}
	return a, nil
}

func (p *Postgres) RevokeAgent(orgID, id string, now time.Time) error {
	res, err := p.db.Exec(`UPDATE agents SET revoked_at=$1 WHERE org_id=$2 AND id=$3`, now, orgID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) SaveDiscovery(orgID, serverID string, snapshot json.RawMessage) error {
	_, err := p.db.Exec(`
		INSERT INTO discovery_snapshots(org_id,server_id,snapshot,updated_at) VALUES($1,$2,$3,now())
		ON CONFLICT (org_id,server_id) DO UPDATE SET snapshot=EXCLUDED.snapshot,updated_at=now()`,
		orgID, serverID, []byte(snapshot))
	return err
}

func (p *Postgres) GetDiscovery(orgID, serverID string) (json.RawMessage, error) {
	var b []byte
	err := p.db.QueryRow(`SELECT snapshot FROM discovery_snapshots WHERE org_id=$1 AND server_id=$2`, orgID, serverID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return json.RawMessage(b), err
}

func (p *Postgres) SaveMetrics(orgID, serverID string, sample json.RawMessage) error {
	_, err := p.db.Exec(`
		INSERT INTO metric_samples(org_id,server_id,sample,updated_at) VALUES($1,$2,$3,now())
		ON CONFLICT (org_id,server_id) DO UPDATE SET sample=EXCLUDED.sample,updated_at=now()`,
		orgID, serverID, []byte(sample))
	return err
}

func (p *Postgres) GetMetrics(orgID, serverID string) (json.RawMessage, error) {
	var b []byte
	err := p.db.QueryRow(`SELECT sample FROM metric_samples WHERE org_id=$1 AND server_id=$2`, orgID, serverID).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return json.RawMessage(b), err
}

func (p *Postgres) AppendMetricSample(orgID, serverID string, ts time.Time, sample json.RawMessage) error {
	if _, err := p.db.Exec(`
		INSERT INTO metric_history(org_id,server_id,ts,sample) VALUES($1,$2,$3,$4)
		ON CONFLICT (org_id,server_id,ts) DO NOTHING`,
		orgID, serverID, ts, []byte(sample)); err != nil {
		return err
	}
	// Opportunistic retention: keep ~7 days of history per server.
	_, _ = p.db.Exec(`DELETE FROM metric_history WHERE org_id=$1 AND server_id=$2 AND ts < $3`,
		orgID, serverID, ts.Add(-7*24*time.Hour))
	return nil
}

func (p *Postgres) QueryMetricHistory(orgID, serverID string, since time.Time) ([]MetricSample, error) {
	rows, err := p.db.Query(`SELECT ts,sample FROM metric_history WHERE org_id=$1 AND server_id=$2 AND ts>=$3 ORDER BY ts`,
		orgID, serverID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MetricSample
	for rows.Next() {
		var ms MetricSample
		var b []byte
		if err := rows.Scan(&ms.TS, &b); err != nil {
			return nil, err
		}
		ms.Sample = json.RawMessage(b)
		out = append(out, ms)
	}
	return out, rows.Err()
}

func (p *Postgres) ListAlerts(orgID string) ([]model.Alert, error) {
	rows, err := p.db.Query(`SELECT id,org_id,name,expr,severity,for_seconds,cooldown_seconds,enabled FROM alerts WHERE org_id=$1 ORDER BY name`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Alert
	for rows.Next() {
		var a model.Alert
		if err := rows.Scan(&a.ID, &a.OrgID, &a.Name, &a.Expr, &a.Severity, &a.ForSeconds, &a.CooldownSeconds, &a.Enabled); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (p *Postgres) CreateAlert(a *model.Alert) error {
	if a.ID == "" {
		a.ID = auth.NewID("alr")
	}
	_, err := p.db.Exec(`INSERT INTO alerts(id,org_id,name,expr,severity,for_seconds,cooldown_seconds,enabled) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		a.ID, a.OrgID, a.Name, a.Expr, a.Severity, a.ForSeconds, a.CooldownSeconds, a.Enabled)
	return err
}

func (p *Postgres) UpdateAlert(orgID string, a *model.Alert) error {
	res, err := p.db.Exec(`UPDATE alerts SET name=$1,expr=$2,severity=$3,for_seconds=$4,cooldown_seconds=$5,enabled=$6 WHERE org_id=$7 AND id=$8`,
		a.Name, a.Expr, a.Severity, a.ForSeconds, a.CooldownSeconds, a.Enabled, orgID, a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) DeleteAlert(orgID, id string) error {
	res, err := p.db.Exec(`DELETE FROM alerts WHERE org_id=$1 AND id=$2`, orgID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (p *Postgres) ListAlertInstances(orgID string) ([]model.AlertInstance, error) {
	rows, err := p.db.Query(`SELECT id,org_id,alert_id,name,severity,server_id,state,dedup_key,COALESCE(root_cause,''),COALESCE(affected,'[]'),started_at,resolved_at FROM alert_instances WHERE org_id=$1 ORDER BY started_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AlertInstance
	for rows.Next() {
		var i model.AlertInstance
		var affected []byte
		var resolved sql.NullTime
		if err := rows.Scan(&i.ID, &i.OrgID, &i.AlertID, &i.Name, &i.Severity, &i.ServerID, &i.State, &i.DedupKey, &i.RootCause, &affected, &i.StartedAt, &resolved); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(affected, &i.Affected)
		if resolved.Valid {
			i.ResolvedAt = &resolved.Time
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (p *Postgres) UpsertAlertInstance(i *model.AlertInstance) error {
	if i.ID == "" {
		i.ID = auth.NewID("ali")
	}
	affected, _ := json.Marshal(i.Affected)
	_, err := p.db.Exec(`
		INSERT INTO alert_instances(id,org_id,alert_id,name,severity,server_id,state,dedup_key,root_cause,affected,started_at,resolved_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (id) DO UPDATE SET state=EXCLUDED.state,resolved_at=EXCLUDED.resolved_at,root_cause=EXCLUDED.root_cause,affected=EXCLUDED.affected`,
		i.ID, i.OrgID, i.AlertID, i.Name, i.Severity, i.ServerID, i.State, i.DedupKey, i.RootCause, affected, i.StartedAt, i.ResolvedAt)
	return err
}

func (p *Postgres) AppendEvent(e *model.Event) error {
	if e.ID == "" {
		e.ID = auth.NewID("evt")
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	_, err := p.db.Exec(`INSERT INTO events(id,org_id,server_id,severity,source,event,prev_state,new_state,ts) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		e.ID, e.OrgID, e.ServerID, e.Severity, e.Source, e.Event, e.PrevState, e.NewState, e.TS)
	return err
}

func (p *Postgres) ListEvents(orgID string, limit int) ([]model.Event, error) {
	rows, err := p.db.Query(`SELECT id,org_id,server_id,severity,source,event,COALESCE(prev_state,''),COALESCE(new_state,''),ts FROM events WHERE org_id=$1 ORDER BY ts DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.ID, &e.OrgID, &e.ServerID, &e.Severity, &e.Source, &e.Event, &e.PrevState, &e.NewState, &e.TS); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *Postgres) AppendAudit(e *model.AuditEntry) error {
	if e.ID == "" {
		e.ID = auth.NewID("aud")
	}
	if e.TS.IsZero() {
		e.TS = time.Now().UTC()
	}
	meta, _ := json.Marshal(e.Metadata)
	_, err := p.db.Exec(`INSERT INTO audit_log(id,org_id,actor,action,result,ip,metadata,ts) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		e.ID, e.OrgID, e.Actor, e.Action, e.Result, e.IP, meta, e.TS)
	return err
}

func (p *Postgres) ListAudit(orgID string, limit int) ([]model.AuditEntry, error) {
	rows, err := p.db.Query(`SELECT id,org_id,actor,action,result,COALESCE(ip,''),COALESCE(metadata,'{}'),ts FROM audit_log WHERE org_id=$1 ORDER BY ts DESC LIMIT $2`, orgID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.AuditEntry
	for rows.Next() {
		var e model.AuditEntry
		var meta []byte
		if err := rows.Scan(&e.ID, &e.OrgID, &e.Actor, &e.Action, &e.Result, &e.IP, &meta, &e.TS); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(meta, &e.Metadata)
		out = append(out, e)
	}
	return out, rows.Err()
}
