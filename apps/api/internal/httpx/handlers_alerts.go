package httpx

import (
	"net/http"
	"strconv"

	"github.com/frix-me/pulse/api/internal/model"
)

func (s *Server) handleListAlerts(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	alerts, err := s.store.ListAlerts(p.OrgID)
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not list alerts")
		return
	}
	if alerts == nil {
		alerts = []model.Alert{}
	}
	JSON(w, http.StatusOK, Page{Data: alerts})
}

type createAlertRequest struct {
	Name            string `json:"name"`
	Expr            string `json:"expr"`
	Severity        string `json:"severity"`
	ForSeconds      int    `json:"for_seconds"`
	CooldownSeconds int    `json:"cooldown_seconds"`
	Enabled         bool   `json:"enabled"`
}

func (s *Server) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	var req createAlertRequest
	if err := decodeJSON(r, &req, 8192); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	if req.Name == "" || req.Expr == "" {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "name and expr are required")
		return
	}
	sev := model.Severity(req.Severity)
	if sev != model.SevInfo && sev != model.SevWarning && sev != model.SevCritical {
		sev = model.SevWarning
	}
	alert := &model.Alert{
		OrgID:           p.OrgID,
		Name:            req.Name,
		Expr:            req.Expr,
		Severity:        sev,
		ForSeconds:      req.ForSeconds,
		CooldownSeconds: req.CooldownSeconds,
		Enabled:         req.Enabled,
	}
	if err := s.store.CreateAlert(alert); err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not create alert")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "alert.create", "success", clientIP(r),
		map[string]any{"name": req.Name})
	JSON(w, http.StatusCreated, alert)
}

func (s *Server) handleUpdateAlert(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	var req createAlertRequest
	if err := decodeJSON(r, &req, 8192); err != nil {
		Fail(w, r, http.StatusBadRequest, CodeValidation, "invalid request body")
		return
	}
	sev := model.Severity(req.Severity)
	if sev != model.SevInfo && sev != model.SevWarning && sev != model.SevCritical {
		sev = model.SevWarning
	}
	alert := &model.Alert{
		ID: r.PathValue("id"), OrgID: p.OrgID, Name: req.Name, Expr: req.Expr,
		Severity: sev, ForSeconds: req.ForSeconds, CooldownSeconds: req.CooldownSeconds, Enabled: req.Enabled,
	}
	if err := s.store.UpdateAlert(p.OrgID, alert); err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "alert not found")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "alert.update", "success", clientIP(r), map[string]any{"id": alert.ID})
	JSON(w, http.StatusOK, alert)
}

func (s *Server) handleDeleteAlert(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	if err := s.store.DeleteAlert(p.OrgID, r.PathValue("id")); err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "alert not found")
		return
	}
	s.audit.Record(p.OrgID, p.Email, "alert.delete", "success", clientIP(r), map[string]any{"id": r.PathValue("id")})
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleAlertInstances(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	// Evaluate rules against the latest data, then return what's firing now.
	s.evaluateAlerts(p.OrgID)
	insts := s.firingInstances(p.OrgID)
	if insts == nil {
		insts = []model.AlertInstance{}
	}
	JSON(w, http.StatusOK, Page{Data: insts})
}

func (s *Server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	limit := limitInt(atoiDefault(r.URL.Query().Get("limit"), 50), 1, 200)
	events, err := s.store.ListEvents(p.OrgID, limit)
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not list events")
		return
	}
	if events == nil {
		events = []model.Event{}
	}
	JSON(w, http.StatusOK, Page{Data: events})
}

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	limit := limitInt(atoiDefault(r.URL.Query().Get("limit"), 100), 1, 200)
	entries, err := s.store.ListAudit(p.OrgID, limit)
	if err != nil {
		Fail(w, r, http.StatusInternalServerError, CodeInternal, "could not list audit log")
		return
	}
	if entries == nil {
		entries = []model.AuditEntry{}
	}
	JSON(w, http.StatusOK, Page{Data: entries})
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
