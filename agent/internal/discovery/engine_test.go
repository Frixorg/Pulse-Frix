package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
	"github.com/frix-me/pulse/agent/internal/redact"
)

// fakeDetector lets us exercise the engine deterministically.
type fakeDetector struct {
	id        string
	available bool
	panics    bool
	err       error
	resources []model.Resource
}

func (f fakeDetector) ID() string      { return f.id }
func (f fakeDetector) Name() string    { return f.id }
func (f fakeDetector) Version() string { return "test" }
func (f fakeDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: f.available, Reason: "test"}
}
func (f fakeDetector) Detect(context.Context) ([]model.Resource, error) {
	if f.panics {
		panic("boom")
	}
	return f.resources, f.err
}
func (f fakeDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

func TestEngineGracefulDegradation(t *testing.T) {
	eng := New([]Detector{
		fakeDetector{id: "ok", available: true, resources: []model.Resource{{Type: "x", Name: "a", DetectedBy: "ok", DetectedAt: time.Now()}}},
		fakeDetector{id: "unavailable", available: false},
		fakeDetector{id: "panics", available: true, panics: true},
		fakeDetector{id: "errors", available: true, err: errors.New("nope")},
	}, WithTimeout(2*time.Second))

	snap := eng.Run(context.Background())

	if len(snap.Detectors) != 4 {
		t.Fatalf("expected 4 detector results, got %d", len(snap.Detectors))
	}
	// A panicking detector must not crash the run.
	var sawPanic, sawErr bool
	for _, d := range snap.Detectors {
		if d.ID == "panics" && d.Error == "" {
			t.Errorf("panic detector should record an error")
		}
		if d.ID == "panics" {
			sawPanic = true
		}
		if d.ID == "errors" && d.Error != "nope" {
			t.Errorf("error detector should surface its error, got %q", d.Error)
		}
		if d.ID == "errors" {
			sawErr = true
		}
	}
	if !sawPanic || !sawErr {
		t.Fatalf("missing expected detector results")
	}
	if len(snap.Resources) != 1 {
		t.Fatalf("expected 1 resource from the healthy detector, got %d", len(snap.Resources))
	}
}

func TestEngineRedactsOutput(t *testing.T) {
	eng := New([]Detector{
		fakeDetector{id: "d", available: true, resources: []model.Resource{{
			Type:       "docker_container",
			Name:       "api",
			DetectedBy: "d",
			DetectedAt: time.Now(),
			Attributes: map[string]any{"DATABASE_URL": "postgres://u:supersecret@h/db"},
			Labels:     map[string]string{"API_KEY": "abcdef123456"},
		}}},
	})
	snap := eng.Run(context.Background())
	r := snap.Resources[0]
	if got := r.Attributes["DATABASE_URL"].(string); got != "postgres://u:"+redact.Placeholder+"@h/db" {
		t.Errorf("attribute not redacted: %q", got)
	}
	if got := r.Labels["API_KEY"]; got != redact.Placeholder {
		t.Errorf("label not redacted: %q", got)
	}
}
