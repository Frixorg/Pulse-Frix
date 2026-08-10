package discovery

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// DockerDetector inspects Docker read-only: containers, their stats, networks,
// volumes and health. It NEVER modifies a user's containers to monitor them and
// prefers Docker's read-only APIs. If the socket is absent/unreadable the
// detector reports unavailable and the platform degrades gracefully.
type DockerDetector struct {
	SocketPath string
}

func (DockerDetector) ID() string      { return "docker" }
func (DockerDetector) Name() string    { return "Docker Detector" }
func (DockerDetector) Version() string { return "1.0" }

func (d DockerDetector) socket() string {
	if d.SocketPath != "" {
		return d.SocketPath
	}
	return "/var/run/docker.sock"
}

func (d DockerDetector) Available(ctx context.Context) model.Availability {
	sock := d.socket()
	if _, err := os.Stat(sock); err != nil {
		return model.Availability{Available: false, Reason: "docker socket not found at " + sock}
	}
	c := newDockerClient(sock)
	if _, err := c.version(ctx); err != nil {
		return model.Availability{Available: false, Reason: "docker api unreachable: " + err.Error()}
	}
	return model.Availability{Available: true}
}

func (d DockerDetector) Detect(ctx context.Context) ([]model.Resource, error) {
	c := newDockerClient(d.socket())
	containers, err := c.containers(ctx)
	if err != nil {
		return nil, err
	}

	var out []model.Resource
	for _, ct := range containers {
		name := containerName(ct.Names)

		var ports []model.Port
		for _, p := range ct.Ports {
			ports = append(ports, model.Port{
				Host:      p.PublicPort,
				Container: p.PrivatePort,
				Protocol:  p.Type,
				Address:   p.IP,
			})
		}

		var networks []string
		for netName := range ct.NetworkSettings.Networks {
			networks = append(networks, netName)
		}

		var volumes []string
		for _, m := range ct.Mounts {
			if m.Source != "" {
				volumes = append(volumes, m.Source+":"+m.Destination)
			}
		}

		attrs := map[string]any{
			"image":  ct.Image,
			"state":  ct.State,
			"status": ct.Status,
		}
		// compose project/service for topology
		if ct.Labels != nil {
			if proj := ct.Labels["com.docker.compose.project"]; proj != "" {
				attrs["compose_project"] = proj
			}
			if svc := ct.Labels["com.docker.compose.service"]; svc != "" {
				attrs["compose_service"] = svc
			}
		}

		// Best-effort live stats (only for running containers).
		if ct.State == "running" {
			if st, err := c.stats(ctx, ct.ID); err == nil {
				attrs["cpu_percent"] = round2(st.cpuPercent())
				attrs["memory_bytes"] = st.MemoryStats.Usage
				attrs["memory_limit"] = st.MemoryStats.Limit
				var rx, tx uint64
				for _, n := range st.Networks {
					rx += n.RxBytes
					tx += n.TxBytes
				}
				attrs["net_rx_bytes"] = rx
				attrs["net_tx_bytes"] = tx
			}
		}

		// Security posture (privileged flag, IPC mode, credential hygiene).
		if di, err := c.inspect(ctx, ct.ID); err == nil {
			attrs["privileged"] = di.HostConfig.Privileged
			if di.HostConfig.IpcMode != "" {
				attrs["ipc_mode"] = di.HostConfig.IpcMode
			}
			if weak, blank := scanEnvCreds(di.Config.Env); len(weak) > 0 || blank {
				if len(weak) > 0 {
					attrs["weak_credentials"] = weak
				}
				if blank {
					attrs["blank_password"] = true
				}
			}
		}

		out = append(out, model.Resource{
			Type:       "docker_container",
			ID:         "container:" + shortID(ct.ID),
			Name:       name,
			Status:     ct.State,
			Health:     dockerHealth(ct.State, ct.Status),
			Labels:     ct.Labels,
			Attributes: attrs,
			Ports:      ports,
			Networks:   networks,
			Volumes:    volumes,
			DetectedBy: "docker",
			DetectedAt: time.Now().UTC(),
		})
	}
	return out, nil
}

func (d DockerDetector) Health(ctx context.Context) model.HealthReport {
	c := newDockerClient(d.socket())
	if _, err := c.version(ctx); err != nil {
		return model.HealthReport{Status: model.StatusUnknown, Checks: []model.Check{{
			Name: "docker_api", Status: model.StatusUnknown, Detail: err.Error(),
		}}}
	}
	return model.HealthReport{Status: model.StatusHealthy, Checks: []model.Check{{
		Name: "docker_api", Status: model.StatusHealthy,
	}}}
}

func containerName(names []string) string {
	if len(names) == 0 {
		return "unknown"
	}
	return strings.TrimPrefix(names[0], "/")
}

func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func dockerHealth(state, status string) model.Status {
	if strings.Contains(status, "unhealthy") {
		return model.StatusDown
	}
	if strings.Contains(status, "health: starting") {
		return model.StatusDegraded
	}
	switch state {
	case "running":
		return model.StatusHealthy
	case "restarting", "paused":
		return model.StatusDegraded
	case "exited", "dead":
		return model.StatusDown
	default:
		return model.StatusUnknown
	}
}

func round2(f float64) float64 {
	return float64(int64(f*100+0.5)) / 100
}

// scanEnvCreds inspects a container's environment for password variables that
// are blank or set to a well-known weak/default value. It returns the offending
// variable NAMES only (never the values) plus whether any password is blank.
func scanEnvCreds(env []string) (weak []string, blank bool) {
	weakVals := map[string]bool{
		"postgres": true, "root": true, "password": true, "admin": true, "changeme": true,
		"secret": true, "123456": true, "toor": true, "mysql": true, "test": true,
		"guest": true, "default": true, "pass": true, "12345678": true,
	}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			continue
		}
		ku := strings.ToUpper(k)
		if !strings.Contains(ku, "PASSWORD") && !strings.Contains(ku, "PASSWD") && !strings.Contains(ku, "PWD") {
			continue
		}
		if strings.TrimSpace(v) == "" {
			blank = true
			continue
		}
		if weakVals[strings.ToLower(strings.TrimSpace(v))] {
			weak = append(weak, k)
		}
	}
	return weak, blank
}
