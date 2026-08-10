package httpx

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

type logEntry struct {
	Source  string `json:"source"`
	Stream  string `json:"stream"`
	Time    string `json:"time"`
	Message string `json:"message"`
}

// handleLogs returns the latest collected log lines for a server, with the set
// of available sources (containers) and optional source/text filters. Log text
// is JSON-encoded (the dashboard escapes it) — never rendered as raw HTML.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	p := s.principal(r)
	srv, err := s.store.GetServer(p.OrgID, r.PathValue("id"))
	if err != nil {
		Fail(w, r, http.StatusNotFound, CodeNotFound, "server not found")
		return
	}
	raw, err := s.store.GetLogs(p.OrgID, srv.ServerID)
	if err != nil {
		JSON(w, http.StatusOK, map[string]any{"sources": []string{}, "entries": []logEntry{}})
		return
	}
	var body struct {
		Entries []logEntry `json:"entries"`
	}
	_ = json.Unmarshal(raw, &body)

	set := map[string]bool{}
	for _, e := range body.Entries {
		set[e.Source] = true
	}
	sources := make([]string, 0, len(set))
	for src := range set {
		sources = append(sources, src)
	}
	sort.Strings(sources)

	source := r.URL.Query().Get("source")
	q := strings.ToLower(r.URL.Query().Get("q"))
	out := make([]logEntry, 0, len(body.Entries))
	for _, e := range body.Entries {
		if source != "" && e.Source != source {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Message), q) {
			continue
		}
		out = append(out, e)
	}
	// Chronological (RFC3339 timestamps sort lexically).
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time < out[j].Time })

	JSON(w, http.StatusOK, map[string]any{"sources": sources, "entries": out})
}
