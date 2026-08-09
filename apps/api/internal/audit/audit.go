// Package audit records sensitive operations. Every mutation the API performs
// on behalf of a user produces an audit record (spec section 92, golden rule
// #20). Sensitive credentials are never included.
package audit

import (
	"log/slog"
	"time"

	"github.com/frix-me/pulse/api/internal/model"
	"github.com/frix-me/pulse/api/internal/store"
)

// Recorder writes audit entries to the store and mirrors them to the log.
type Recorder struct {
	store  store.Store
	logger *slog.Logger
}

// New creates a Recorder.
func New(st store.Store, logger *slog.Logger) *Recorder {
	return &Recorder{store: st, logger: logger}
}

// Record persists an audit entry. Metadata must be pre-redacted by the caller.
func (r *Recorder) Record(orgID, actor, action, result, ip string, metadata map[string]any) {
	entry := &model.AuditEntry{
		OrgID:    orgID,
		Actor:    actor,
		Action:   action,
		Result:   result,
		IP:       ip,
		Metadata: metadata,
		TS:       time.Now().UTC(),
	}
	if err := r.store.AppendAudit(entry); err != nil {
		r.logger.Error("failed to write audit entry", "error", err, "action", action)
	}
	r.logger.Info("audit",
		"org", orgID, "actor", actor, "action", action, "result", result, "ip", ip)
}
