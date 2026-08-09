// Package health provides reusable read-only service health checks. Checks never
// write to the target service; they establish a connection or issue a safe GET.
package health

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// CheckTCP reports whether a TCP endpoint accepts connections.
func CheckTCP(ctx context.Context, address string, timeout time.Duration) model.Check {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", address)
	if err != nil {
		return model.Check{Name: "tcp:" + address, Status: model.StatusDown, Detail: err.Error()}
	}
	_ = conn.Close()
	return model.Check{Name: "tcp:" + address, Status: model.StatusHealthy}
}

// CheckHTTP issues a GET and classifies the response. 2xx/3xx = healthy,
// 4xx = degraded, 5xx or transport error = down.
func CheckHTTP(ctx context.Context, url string, timeout time.Duration) model.Check {
	client := &http.Client{Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return model.Check{Name: "http:" + url, Status: model.StatusDown, Detail: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return model.Check{Name: "http:" + url, Status: model.StatusDown, Detail: err.Error()}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode < 400:
		return model.Check{Name: "http:" + url, Status: model.StatusHealthy}
	case resp.StatusCode < 500:
		return model.Check{Name: "http:" + url, Status: model.StatusDegraded, Detail: resp.Status}
	default:
		return model.Check{Name: "http:" + url, Status: model.StatusDown, Detail: resp.Status}
	}
}

// Aggregate reduces a set of checks to an overall status (worst-wins).
func Aggregate(checks []model.Check) model.HealthReport {
	overall := model.StatusHealthy
	rank := map[model.Status]int{
		model.StatusHealthy:  0,
		model.StatusDegraded: 1,
		model.StatusUnknown:  2,
		model.StatusDown:     3,
	}
	for _, c := range checks {
		if rank[c.Status] > rank[overall] {
			overall = c.Status
		}
	}
	return model.HealthReport{Status: overall, Checks: checks}
}
