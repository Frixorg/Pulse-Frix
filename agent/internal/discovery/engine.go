// Package discovery is the read-only inspection engine at the heart of Pulse.
//
// It is a registry of independent Detectors. The engine runs them concurrently,
// bounds each with a timeout, isolates panics, and merges the results into a
// single redacted Snapshot. One failing detector never fails the whole run
// (graceful degradation). No detector ever writes to the system it inspects.
package discovery

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
	"github.com/frix-me/pulse/agent/internal/redact"
)

// Detector is the common interface every detector implements. See
// docs/DISCOVERY.md#detector-interface.
type Detector interface {
	ID() string
	Name() string
	Version() string
	Available(ctx context.Context) model.Availability
	Detect(ctx context.Context) ([]model.Resource, error)
	Health(ctx context.Context) model.HealthReport
}

// Engine runs a set of detectors.
type Engine struct {
	detectors      []Detector
	perDetectorTTL time.Duration
	installationID string
	serverID       string
}

// Option configures the engine.
type Option func(*Engine)

// WithTimeout sets the per-detector timeout (default 10s).
func WithTimeout(d time.Duration) Option { return func(e *Engine) { e.perDetectorTTL = d } }

// WithIdentity sets the installation/server identifiers stamped on the snapshot.
func WithIdentity(installationID, serverID string) Option {
	return func(e *Engine) { e.installationID = installationID; e.serverID = serverID }
}

// New creates an engine with the given detectors.
func New(detectors []Detector, opts ...Option) *Engine {
	e := &Engine{detectors: detectors, perDetectorTTL: 10 * time.Second}
	for _, o := range opts {
		o(e)
	}
	return e
}

// Run executes all detectors and returns a fully redacted snapshot.
func (e *Engine) Run(ctx context.Context) *model.Snapshot {
	start := time.Now()
	hostname, _ := os.Hostname()

	results := make([]detectorOutcome, len(e.detectors))
	var wg sync.WaitGroup
	for i, d := range e.detectors {
		wg.Add(1)
		go func(i int, d Detector) {
			defer wg.Done()
			results[i] = runDetector(ctx, d, e.perDetectorTTL)
		}(i, d)
	}
	wg.Wait()

	snap := &model.Snapshot{
		InstallationID: e.installationID,
		ServerID:       e.serverID,
		Hostname:       hostname,
		GeneratedAt:    start.UTC(),
	}
	for _, o := range results {
		snap.Detectors = append(snap.Detectors, o.result)
		snap.Resources = append(snap.Resources, o.resources...)
	}
	// Deterministic ordering for stable diffs.
	sort.SliceStable(snap.Detectors, func(i, j int) bool { return snap.Detectors[i].ID < snap.Detectors[j].ID })
	sort.SliceStable(snap.Resources, func(i, j int) bool {
		if snap.Resources[i].Type != snap.Resources[j].Type {
			return snap.Resources[i].Type < snap.Resources[j].Type
		}
		return snap.Resources[i].Name < snap.Resources[j].Name
	})

	snap.Topology = BuildTopology(snap.Resources)
	snap.DurationMS = time.Since(start).Milliseconds()

	// Single choke point: everything is redacted before it can leave the agent.
	return redact.Snapshot(snap)
}

// detectorOutcome is the internal result of running one detector.
type detectorOutcome struct {
	result    model.DetectorResult
	resources []model.Resource
}

// runDetector runs one detector with panic isolation and a timeout.
func runDetector(ctx context.Context, d Detector, ttl time.Duration) (out detectorOutcome) {
	start := time.Now()
	res := model.DetectorResult{ID: d.ID(), Name: d.Name(), Version: d.Version()}

	defer func() {
		if r := recover(); r != nil {
			res.Error = fmt.Sprintf("panic: %v", r)
			res.Available = false
			res.Health = model.HealthReport{Status: model.StatusUnknown}
			res.DurationMS = time.Since(start).Milliseconds()
			out.result = res
		}
	}()

	dctx, cancel := context.WithTimeout(ctx, ttl)
	defer cancel()

	avail := d.Available(dctx)
	res.Available = avail.Available
	res.Reason = avail.Reason
	if !avail.Available {
		res.Health = model.HealthReport{Status: model.StatusUnknown}
		res.DurationMS = time.Since(start).Milliseconds()
		out.result = res
		return out
	}

	resources, err := d.Detect(dctx)
	if err != nil {
		res.Error = err.Error()
	}
	res.Count = len(resources)
	res.Health = d.Health(dctx)
	res.DurationMS = time.Since(start).Milliseconds()

	out.result = res
	out.resources = resources
	return out
}
