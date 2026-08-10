package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// dockerClient is a minimal read-only Docker Engine API client that talks over
// the unix socket using only the standard library (no Docker SDK). It calls a
// small allowlist of INSPECTION endpoints and never mutating ones. See
// SECURITY.md (Docker socket).
type dockerClient struct {
	socket string
	http   *http.Client
	apiVer string
}

// DockerAllowedPaths documents (and, in the socket proxy, enforces) the exact
// set of endpoints Pulse uses. All are read-only. See SECURITY.md.
var DockerAllowedPaths = []string{
	"/version", "/info",
	"/containers/json", "/containers/*/json", "/containers/*/stats", "/containers/*/logs",
	"/networks", "/volumes", "/images/json", "/system/df",
}

func newDockerClient(socket string) *dockerClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		DisableCompression: true,
	}
	return &dockerClient{
		socket: socket,
		apiVer: "v1.41",
		http:   &http.Client{Transport: tr, Timeout: 8 * time.Second},
	}
}

func (c *dockerClient) get(ctx context.Context, path string, out any) error {
	// The host is a placeholder; the transport dials the unix socket regardless.
	url := "http://unix/" + c.apiVer + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("docker api %s: status %d", path, resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// dockerVersion is a subset of GET /version.
type dockerVersion struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
	Os         string `json:"Os"`
	Arch       string `json:"Arch"`
}

func (c *dockerClient) version(ctx context.Context) (dockerVersion, error) {
	var v dockerVersion
	err := c.get(ctx, "/version", &v)
	return v, err
}

// dockerContainer is a subset of GET /containers/json?all=1.
type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
	Ports  []struct {
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
		IP          string `json:"IP"`
	} `json:"Ports"`
	Labels          map[string]string `json:"Labels"`
	NetworkSettings struct {
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
	Mounts []struct {
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		Mode        string `json:"Mode"`
	} `json:"Mounts"`
}

func (c *dockerClient) containers(ctx context.Context) ([]dockerContainer, error) {
	var cs []dockerContainer
	err := c.get(ctx, "/containers/json?all=1", &cs)
	return cs, err
}

// dockerStats is a subset of GET /containers/{id}/stats?stream=false.
type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint64 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
}

func (c *dockerClient) stats(ctx context.Context, id string) (dockerStats, error) {
	var s dockerStats
	err := c.get(ctx, "/containers/"+id+"/stats?stream=false", &s)
	return s, err
}

// dockerInspect is a subset of GET /containers/{id}/json used for security
// posture (privileged flag, IPC mode, environment credential hygiene).
type dockerInspect struct {
	Config struct {
		Env []string `json:"Env"`
	} `json:"Config"`
	HostConfig struct {
		Privileged  bool     `json:"Privileged"`
		IpcMode     string   `json:"IpcMode"`
		SecurityOpt []string `json:"SecurityOpt"`
	} `json:"HostConfig"`
}

func (c *dockerClient) inspect(ctx context.Context, id string) (dockerInspect, error) {
	var di dockerInspect
	err := c.get(ctx, "/containers/"+id+"/json", &di)
	return di, err
}

// getRaw fetches a non-JSON body (used for the multiplexed logs stream).
func (c *dockerClient) getRaw(ctx context.Context, path string) ([]byte, error) {
	url := "http://unix/" + c.apiVer + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("docker api %s: status %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512*1024))
}

func (c *dockerClient) logs(ctx context.Context, id string, tail int) ([]byte, error) {
	return c.getRaw(ctx, fmt.Sprintf("/containers/%s/logs?stdout=1&stderr=1&timestamps=1&tail=%d", id, tail))
}

// cpuPercent computes CPU usage percentage from two stat samples (as Docker CLI does).
func (s dockerStats) cpuPercent() float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	if sysDelta <= 0 || cpuDelta < 0 {
		return 0
	}
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = 1
	}
	return (cpuDelta / sysDelta) * cpus * 100.0
}
