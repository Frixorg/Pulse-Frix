package scanner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/frix-me/pulse/api/internal/ssrf"
)

const (
	overallTimeout  = 3 * time.Minute
	perCheckTimeout = 30 * time.Second
	perReqTimeout   = 6 * time.Second
	maxJobs         = 40 // retained job history before pruning oldest
)

// registry is the full ordered list of checks (passive first, then active).
func registry() []checkDef {
	return append(passiveChecks(), activeChecks()...)
}

// severityRank orders findings for display (most serious first).
func severityRank(s string) int {
	switch s {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 3
	default:
		return 4
	}
}

// job is a running/finished scan plus its lock.
type job struct {
	mu sync.Mutex
	st ScanState
}

func (j *job) log(level, check, msg string) {
	j.mu.Lock()
	j.st.Logs = append(j.st.Logs, LogEntry{T: time.Now().UTC(), Level: level, Check: check, Msg: msg})
	j.mu.Unlock()
}

func (j *job) snapshot() ScanState {
	j.mu.Lock()
	defer j.mu.Unlock()
	cp := j.st
	cp.Checks = append([]Check(nil), j.st.Checks...)
	cp.Findings = append([]Finding(nil), j.st.Findings...)
	cp.Logs = append([]LogEntry(nil), j.st.Logs...)
	cp.Categories = append([]string(nil), j.st.Categories...)
	cp.Targets = append([]string(nil), j.st.Targets...)
	return cp
}

// Manager owns scan jobs and the latest result per server. In-memory by design:
// scans are cheap to re-run and results are not security-of-record.
type Manager struct {
	mu     sync.Mutex
	jobs   map[string]*job
	latest map[string]string // serverID -> scanID
	order  []string          // scanID insertion order, for pruning
	logger *slog.Logger
}

// NewManager constructs a Manager.
func NewManager(logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		jobs:   map[string]*job{},
		latest: map[string]string{},
		logger: logger,
	}
}

func newScanID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "scan_" + hex.EncodeToString(b)
}

// selectChecks returns the checks to run for a mode + optional category filter.
func selectChecks(mode string, cats []string) []checkDef {
	catSet := map[string]bool{}
	for _, c := range cats {
		catSet[c] = true
	}
	var out []checkDef
	for _, d := range registry() {
		if mode == ModePassive && d.meta.Kind != KindPassive {
			continue
		}
		if mode == ModeActive && d.meta.Kind != KindActive {
			continue
		}
		if len(catSet) > 0 && !catSet[d.meta.Category] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// Catalogue returns the full check catalogue with default (not_run) status.
func Catalogue() []Check {
	var out []Check
	for _, d := range registry() {
		c := d.meta
		c.Status = StatusNotRun
		out = append(out, c)
	}
	return out
}

// StartScan launches a scan in the background and returns its id immediately.
func (m *Manager) StartScan(in *Input, mode string, cats []string) string {
	if mode == "" {
		mode = ModeFull
	}
	defs := selectChecks(mode, cats)

	checks := make([]Check, 0, len(defs))
	for _, d := range defs {
		c := d.meta
		c.Status = StatusNotRun
		checks = append(checks, c)
	}
	var targetURLs []string
	for _, t := range in.Targets {
		targetURLs = append(targetURLs, t.URL)
	}

	id := newScanID()
	j := &job{st: ScanState{
		ID:         id,
		ServerID:   in.ServerID,
		Status:     ScanQueued,
		Mode:       mode,
		Categories: cats,
		Targets:    targetURLs,
		Total:      len(defs),
		StartedAt:  time.Now().UTC(),
		Checks:     checks,
		Findings:   []Finding{},
		Logs:       []LogEntry{},
	}}

	m.mu.Lock()
	m.jobs[id] = j
	m.latest[in.ServerID] = id
	m.order = append(m.order, id)
	m.prune()
	m.mu.Unlock()

	in.httpClient = ssrf.SafeClient(perReqTimeout)
	go m.run(j, in, defs)
	return id
}

// prune caps retained jobs (call with m.mu held).
func (m *Manager) prune() {
	for len(m.order) > maxJobs {
		old := m.order[0]
		m.order = m.order[1:]
		// Keep it if it's still some server's latest.
		stillLatest := false
		for _, id := range m.latest {
			if id == old {
				stillLatest = true
				break
			}
		}
		if !stillLatest {
			delete(m.jobs, old)
		}
	}
}

// run executes the selected checks sequentially, updating job state live.
func (m *Manager) run(j *job, in *Input, defs []checkDef) {
	ctx, cancel := context.WithTimeout(context.Background(), overallTimeout)
	defer cancel()

	j.mu.Lock()
	j.st.Status = ScanRunning
	j.mu.Unlock()
	j.log(LogInfo, "", fmt.Sprintf("Scan started · %d checks · %d target(s)", len(defs), len(in.Targets)))

	for i := range defs {
		d := defs[i]
		start := time.Now()

		j.mu.Lock()
		j.st.Current = d.meta.Name
		j.mu.Unlock()

		// Active checks need at least one reachable public URL.
		if d.meta.Kind == KindActive && len(in.Targets) == 0 {
			j.finishCheck(d.meta.ID, StatusSkipped, 0, time.Since(start), "no public targets")
			j.log(LogWarn, d.meta.ID, "Skipped "+d.meta.Name+" — no public URL discovered for this server")
			j.advance(i, len(defs))
			continue
		}

		j.log(LogInfo, d.meta.ID, "Running "+d.meta.Name)
		count := m.runOne(ctx, j, d, in)

		status := StatusPass
		if count > 0 {
			status = StatusIssues
			j.log(LogWarn, d.meta.ID, fmt.Sprintf("%s: %d finding(s)", d.meta.Name, count))
		} else {
			j.log(LogSuccess, d.meta.ID, d.meta.Name+": clean")
		}
		j.finishCheck(d.meta.ID, status, count, time.Since(start), "")
		j.advance(i, len(defs))
	}

	// Sort findings by severity for stable, useful display.
	j.mu.Lock()
	sort.SliceStable(j.st.Findings, func(a, b int) bool {
		return severityRank(j.st.Findings[a].Severity) < severityRank(j.st.Findings[b].Severity)
	})
	now := time.Now().UTC()
	j.st.FinishedAt = &now
	j.st.Status = ScanDone
	j.st.Current = ""
	j.st.Progress = 1
	total := len(j.st.Findings)
	j.mu.Unlock()
	j.log(LogSuccess, "", fmt.Sprintf("Scan complete · %d finding(s)", total))
}

// runOne runs a single check with panic isolation and a per-check deadline,
// returning the number of findings it produced.
func (m *Manager) runOne(parent context.Context, j *job, d checkDef, in *Input) (count int) {
	ctx, cancel := context.WithTimeout(parent, perCheckTimeout)
	defer cancel()

	emit := func(f Finding) {
		f.CheckID = d.meta.ID
		if f.Category == "" {
			f.Category = d.meta.Category
		}
		if f.OWASP == "" {
			f.OWASP = d.meta.OWASP
		}
		j.mu.Lock()
		j.st.Findings = append(j.st.Findings, f)
		j.mu.Unlock()
		count++
	}
	logf := func(level, msg string) { j.log(level, d.meta.ID, msg) }

	defer func() {
		if r := recover(); r != nil {
			m.logger.Error("security check panicked", "check", d.meta.ID, "recover", r)
			j.log(LogError, d.meta.ID, fmt.Sprintf("Check errored: %v", r))
			j.setCheckError(d.meta.ID)
		}
	}()

	d.run(ctx, in, emit, logf)
	return count
}

func (j *job) advance(i, total int) {
	j.mu.Lock()
	j.st.Completed = i + 1
	if total > 0 {
		j.st.Progress = float64(i+1) / float64(total)
	}
	j.mu.Unlock()
}

func (j *job) finishCheck(id, status string, count int, dur time.Duration, note string) {
	j.mu.Lock()
	for k := range j.st.Checks {
		if j.st.Checks[k].ID == id {
			j.st.Checks[k].Status = status
			j.st.Checks[k].Count = count
			j.st.Checks[k].DurationMS = dur.Milliseconds()
			if note != "" {
				j.st.Checks[k].Note = note
			}
			break
		}
	}
	j.mu.Unlock()
}

func (j *job) setCheckError(id string) {
	j.mu.Lock()
	for k := range j.st.Checks {
		if j.st.Checks[k].ID == id {
			j.st.Checks[k].Status = StatusError
			break
		}
	}
	j.mu.Unlock()
}

// Get returns a snapshot of a scan by id.
func (m *Manager) Get(scanID string) (ScanState, bool) {
	m.mu.Lock()
	j, ok := m.jobs[scanID]
	m.mu.Unlock()
	if !ok {
		return ScanState{}, false
	}
	return j.snapshot(), true
}

// Latest returns the most recent scan for a server.
func (m *Manager) Latest(serverID string) (ScanState, bool) {
	m.mu.Lock()
	id, ok := m.latest[serverID]
	var j *job
	if ok {
		j = m.jobs[id]
	}
	m.mu.Unlock()
	if j == nil {
		return ScanState{}, false
	}
	return j.snapshot(), true
}

// RunPassiveSync runs only the passive checks inline (fast, no network) and
// stores the result as the server's latest scan. Used to populate the page on
// first load without waiting for an active scan.
func (m *Manager) RunPassiveSync(in *Input) ScanState {
	defs := selectChecks(ModePassive, nil)
	checks := make([]Check, 0, len(defs))
	for _, d := range defs {
		c := d.meta
		c.Status = StatusNotRun
		checks = append(checks, c)
	}
	id := newScanID()
	j := &job{st: ScanState{
		ID: id, ServerID: in.ServerID, Status: ScanRunning, Mode: ModePassive,
		Total: len(defs), StartedAt: time.Now().UTC(), Checks: checks,
		Findings: []Finding{}, Logs: []LogEntry{},
	}}

	ctx := context.Background()
	for i := range defs {
		d := defs[i]
		start := time.Now()
		count := m.runOne(ctx, j, d, in)
		status := StatusPass
		if count > 0 {
			status = StatusIssues
		}
		j.finishCheck(d.meta.ID, status, count, time.Since(start), "")
		j.advance(i, len(defs))
	}
	j.mu.Lock()
	sort.SliceStable(j.st.Findings, func(a, b int) bool {
		return severityRank(j.st.Findings[a].Severity) < severityRank(j.st.Findings[b].Severity)
	})
	now := time.Now().UTC()
	j.st.FinishedAt = &now
	j.st.Status = ScanDone
	j.st.Progress = 1
	j.mu.Unlock()

	m.mu.Lock()
	m.jobs[id] = j
	m.latest[in.ServerID] = id
	m.order = append(m.order, id)
	m.prune()
	m.mu.Unlock()
	return j.snapshot()
}
