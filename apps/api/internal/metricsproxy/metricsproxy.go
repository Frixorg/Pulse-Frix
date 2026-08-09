// Package metricsproxy abstracts the Prometheus-compatible metrics backend so
// the dashboard never speaks PromQL and the storage engine can evolve later
// (spec section 12). It returns already-shaped series and degrades gracefully
// when the backend is unavailable.
package metricsproxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client queries a Prometheus-compatible backend.
type Client struct {
	baseURL string
	http    *http.Client
}

// New creates a metrics client.
func New(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: 10 * time.Second}}
}

// Point is a single time-series sample.
type Point struct {
	T float64 `json:"t"` // unix seconds
	V float64 `json:"v"`
}

// Series is a named list of points.
type Series struct {
	Name   string  `json:"name"`
	Unit   string  `json:"unit,omitempty"`
	Points []Point `json:"points"`
}

// promResponse is the subset of the Prometheus query_range response we need.
type promResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Metric map[string]string `json:"metric"`
			Values [][]any           `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

// QueryRange runs a PromQL range query and returns shaped series. If the backend
// is unreachable it returns an error the handler can degrade on.
func (c *Client) QueryRange(ctx context.Context, promQL string, start, end time.Time, step time.Duration) ([]Series, error) {
	q := url.Values{}
	q.Set("query", promQL)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", strconv.Itoa(int(step.Seconds())))

	reqURL := c.baseURL + "/api/v1/query_range?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics backend status %d", resp.StatusCode)
	}
	var pr promResponse
	if err := json.NewDecoder(resp.Body).Decode(&pr); err != nil {
		return nil, err
	}
	if pr.Status != "success" {
		return nil, fmt.Errorf("metrics query failed")
	}
	var series []Series
	for _, r := range pr.Data.Result {
		name := r.Metric["__name__"]
		if name == "" {
			name = promQL
		}
		s := Series{Name: name}
		for _, v := range r.Values {
			if len(v) != 2 {
				continue
			}
			t, _ := v[0].(float64)
			valStr, _ := v[1].(string)
			val, _ := strconv.ParseFloat(valStr, 64)
			s.Points = append(s.Points, Point{T: t, V: val})
		}
		series = append(series, s)
	}
	return series, nil
}

// RangeFor maps a friendly range token to a (duration, step).
func RangeFor(token string) (time.Duration, time.Duration) {
	switch token {
	case "1h":
		return time.Hour, 30 * time.Second
	case "6h":
		return 6 * time.Hour, 2 * time.Minute
	case "24h":
		return 24 * time.Hour, 5 * time.Minute
	case "7d":
		return 7 * 24 * time.Hour, 30 * time.Minute
	case "30d":
		return 30 * 24 * time.Hour, 2 * time.Hour
	default:
		return time.Hour, 30 * time.Second
	}
}
