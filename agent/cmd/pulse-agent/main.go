// Command pulse-agent is the long-running Pulse agent. It performs periodic
// read-only discovery, collects system metrics, and (in cloud mode) ships them
// outbound to the control plane over a signed connection. It is offline-first:
// if the control plane is unreachable it keeps monitoring locally and buffers a
// bounded amount of data. It never opens an inbound port and never executes
// commands received from the control plane.
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/frix-me/pulse/agent/internal/cache"
	"github.com/frix-me/pulse/agent/internal/config"
	"github.com/frix-me/pulse/agent/internal/discovery"
	"github.com/frix-me/pulse/agent/internal/metrics"
	"github.com/frix-me/pulse/agent/internal/protocol"
	"github.com/frix-me/pulse/agent/internal/version"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}
	logger := newLogger(cfg)
	slog.SetDefault(logger)

	logger.Info("pulse agent starting",
		"version", version.Version, "protocol", version.Protocol,
		"mode", cfg.Mode, "server_id", cfg.Identity.ServerID)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	engine := discovery.New(discovery.DefaultDetectors(),
		discovery.WithTimeout(10*time.Second),
		discovery.WithIdentity(cfg.Identity.InstallationID, cfg.Identity.ServerID),
	)
	collector := metrics.NewCollector()
	buffer := cache.New(4096)

	var client *protocol.Client
	if cfg.Mode == config.ModeCloud {
		signer, err := cfg.Identity.Signer()
		if err != nil {
			logger.Error("invalid agent identity", "error", err)
			os.Exit(1)
		}
		// Enroll on first cloud start (outbound; token-authenticated). Retry a
		// few times so a control plane that is briefly busy right after startup
		// doesn't strand the agent for the rest of its lifetime.
		if cfg.EnrollmentToken != "" && !cfg.Identity.Enrolled {
			hostname, _ := os.Hostname()
			var resp *protocol.EnrollResponse
			var err error
			for attempt := 1; attempt <= 5; attempt++ {
				enrollCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
				resp, err = protocol.Enroll(enrollCtx, cfg.APIURL, protocol.EnrollRequest{
					EnrollmentToken: cfg.EnrollmentToken,
					InstallationID:  cfg.Identity.InstallationID,
					PublicKey:       cfg.Identity.PublicKey,
					ProtocolVersion: version.Protocol,
					Fingerprint:     map[string]string{"hostname": hostname, "os": runtime.GOOS, "arch": runtime.GOARCH},
				})
				cancel()
				if err == nil {
					break
				}
				logger.Warn("enrollment attempt failed; will retry", "attempt", attempt, "error", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(3 * time.Second):
				}
			}
			if err != nil {
				logger.Warn("enrollment failed; monitoring continues locally", "error", err)
			} else {
				cfg.Identity.ServerID = resp.ServerID
				cfg.Identity.AgentID = resp.AgentID
				cfg.Identity.Enrolled = true
				if err := config.SaveIdentity(cfg.DataDir, &cfg.Identity); err != nil {
					logger.Warn("could not persist identity", "error", err)
				}
				logger.Info("enrolled with cloud", "server_id", resp.ServerID)
			}
		}
		client = protocol.New(cfg.APIURL, cfg.Identity.AgentID, signer)
	}

	stateDir := filepath.Join(cfg.DataDir, "state")
	_ = os.MkdirAll(stateDir, 0o700)

	// Run an initial discovery immediately.
	runDiscovery(ctx, logger, engine, client, stateDir)

	discoveryTicker := time.NewTicker(cfg.DiscoveryEvery)
	metricsTicker := time.NewTicker(cfg.MetricsEvery)
	defer discoveryTicker.Stop()
	defer metricsTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down; existing services are unaffected")
			return

		case <-discoveryTicker.C:
			runDiscovery(ctx, logger, engine, client, stateDir)

		case <-metricsTicker.C:
			sample := collector.Sample()
			writeJSON(filepath.Join(stateDir, "metrics.json"), sample)
			if client != nil {
				sendCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				if err := client.Send(sendCtx, "metrics", sample); err != nil {
					// Offline-first: buffer and keep going. Never stop monitoring.
					payload, _ := json.Marshal(sample)
					buffer.Push(cache.Item{Priority: cache.PriorityHistorical, Payload: payload})
					logger.Warn("metrics send failed; buffered locally",
						"error", err, "buffered", buffer.Len())
				} else {
					flushBuffer(sendCtx, logger, client, buffer)
				}
				cancel()
			}
		}
	}
}

func runDiscovery(ctx context.Context, logger *slog.Logger, engine *discovery.Engine, client *protocol.Client, stateDir string) {
	start := time.Now()
	snap := engine.Run(ctx) // already redacted
	writeJSON(filepath.Join(stateDir, "discovery.json"), snap)
	logger.Info("discovery_completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"resources", len(snap.Resources))

	if client != nil {
		sendCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		if err := client.Send(sendCtx, "discovery", snap); err != nil {
			logger.Warn("discovery send failed; will retry next cycle", "error", err)
		}
	}
}

func flushBuffer(ctx context.Context, logger *slog.Logger, client *protocol.Client, buffer *cache.RingBuffer) {
	items := buffer.Drain()
	for _, it := range items {
		var raw json.RawMessage = it.Payload
		if err := client.Send(ctx, "metrics", raw); err != nil {
			// Put it back and stop; try again next cycle.
			buffer.Push(it)
			logger.Warn("buffer flush interrupted", "error", err, "remaining", buffer.Len())
			return
		}
	}
	if len(items) > 0 {
		logger.Info("flushed buffered metrics", "count", len(items))
	}
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path) // atomic replace
}

func newLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
