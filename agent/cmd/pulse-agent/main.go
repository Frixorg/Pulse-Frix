// Command pulse-agent is the long-running Pulse agent. It performs periodic
// read-only discovery, collects system metrics, and (in cloud mode) ships them
// outbound to the control plane over a signed connection. It is offline-first:
// if the control plane is unreachable it keeps monitoring locally and buffers a
// bounded amount of data. It never opens an inbound port and never executes
// commands received from the control plane.
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
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

// How often an agent without a working credential tries to get one back.
const (
	credentialRetryEvery = 60 * time.Second
	// After the control plane says outright that it has never heard of this
	// key, retries slow right down. Only a human can fix that — but a restored
	// backup or a repaired database can too, so they never stop entirely.
	credentialRefusedRetryEvery = 15 * time.Minute
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

	var enrol *enrollment
	if cfg.Mode == config.ModeCloud {
		signer, err := cfg.Identity.Signer()
		if err != nil {
			logger.Error("invalid agent identity", "error", err)
			os.Exit(1)
		}
		enrol = &enrollment{
			cfg:    cfg,
			logger: logger,
			signer: signer,
			client: protocol.New(cfg.APIURL, cfg.Identity.AgentID, signer),
		}
		// Get a credential before the first send. A control plane that is
		// briefly busy right after start-up must not strand the agent, so this
		// retries a few times; after that the ticker below keeps trying for as
		// long as the agent runs.
		//
		// An already-enrolled agent skips this entirely — it has a credential
		// and the send path is what discovers whether it still works.
		if !enrol.ready() && cfg.EnrollmentToken == "" {
			logger.Warn("no enrollment token configured; monitoring continues locally only")
		}
		for attempt := 1; attempt <= 5 && !enrol.ready() && cfg.EnrollmentToken != ""; attempt++ {
			if enrol.attempt(ctx) {
				break
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
	}

	stateDir := filepath.Join(cfg.DataDir, "state")
	_ = os.MkdirAll(stateDir, 0o700)

	// Run an initial discovery immediately.
	runDiscovery(ctx, logger, engine, enrol, stateDir)

	discoveryTicker := time.NewTicker(cfg.DiscoveryEvery)
	metricsTicker := time.NewTicker(cfg.MetricsEvery)
	logsTicker := time.NewTicker(20 * time.Second)
	// Ticks on its own schedule so an agent that has no usable credential keeps
	// trying to get one even while every send is failing.
	credentialTicker := time.NewTicker(credentialRetryEvery)
	defer discoveryTicker.Stop()
	defer metricsTicker.Stop()
	defer logsTicker.Stop()
	defer credentialTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("shutting down; existing services are unaffected")
			return

		case <-credentialTicker.C:
			if enrol != nil && !enrol.ready() && enrol.due() {
				enrol.attempt(ctx)
			}

		case <-discoveryTicker.C:
			runDiscovery(ctx, logger, engine, enrol, stateDir)

		case <-logsTicker.C:
			if enrol != nil {
				entries := discovery.CollectContainerLogs(ctx, cfg.DockerSocket, 40)
				if len(entries) > 0 {
					sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
					if err := enrol.send(sendCtx, "logs", map[string]any{"entries": entries}); err != nil {
						logger.Warn("logs send failed", "error", err)
					}
					cancel()
				}
			}

		case <-metricsTicker.C:
			sample := collector.Sample()
			writeJSON(filepath.Join(stateDir, "metrics.json"), sample)
			if enrol != nil {
				sendCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
				if err := enrol.send(sendCtx, "metrics", sample); err != nil {
					// Offline-first: buffer and keep going. Never stop monitoring.
					payload, _ := json.Marshal(sample)
					buffer.Push(cache.Item{Priority: cache.PriorityHistorical, Payload: payload})
					logger.Warn("metrics send failed; buffered locally",
						"error", err, "buffered", buffer.Len())
				} else {
					flushBuffer(sendCtx, logger, enrol, buffer)
				}
				cancel()
			}
		}
	}
}

// enrollment owns the agent's credential: obtaining it, persisting it, and
// getting it back when the control plane stops accepting it.
//
// It exists because of one specific failure. identity.json records "enrolled"
// permanently, so an agent whose registration the control plane has lost — a
// restarted in-memory store, a rebuilt database, a revoked agent — carries on
// signing with an agent id that no longer resolves. Every send is refused, the
// buffer fills, the dashboard shows no servers at all, and nothing in the agent
// ever tries to put it right.
type enrollment struct {
	cfg    *config.Config
	logger *slog.Logger
	signer ed25519.PrivateKey
	client *protocol.Client

	// rejected is set the moment the control plane refuses this credential and
	// cleared when a new one is issued.
	rejected bool
	// refusedLogged keeps the "a human has to act" message to once per run.
	refusedLogged bool
	nextAttempt   time.Time
}

// ready reports whether the agent believes it holds a credential the control
// plane will accept. It is belief, not proof — the send path is what finds out.
func (e *enrollment) ready() bool { return e.cfg.Identity.Enrolled && !e.rejected }

// due rate-limits recovery, so neither a control plane that is down nor one
// that has refused this agent outright gets hammered.
func (e *enrollment) due() bool { return !time.Now().Before(e.nextAttempt) }

// attempt gets the agent a usable credential by whichever route is still open.
//
// An agent that has been registered before still holds the keypair the control
// plane knows, so the SIGNED re-bind is the route that can work: the enrollment
// token it was installed with was single-use and is long spent. A brand-new
// agent has nothing to re-bind and uses the token.
func (e *enrollment) attempt(ctx context.Context) bool {
	e.nextAttempt = time.Now().Add(credentialRetryEvery)
	if e.cfg.Identity.Enrolled {
		return e.recover(ctx)
	}
	return e.enroll(ctx)
}

// enroll registers a brand-new agent with a short-lived token.
func (e *enrollment) enroll(ctx context.Context) bool {
	// Nothing to try. Startup has already said so once; repeating it every
	// minute for the life of the process would say nothing new.
	if e.cfg.EnrollmentToken == "" {
		return false
	}
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := protocol.Enroll(callCtx, e.cfg.APIURL, protocol.EnrollRequest{
		EnrollmentToken: e.cfg.EnrollmentToken,
		InstallationID:  e.cfg.Identity.InstallationID,
		PublicKey:       e.cfg.Identity.PublicKey,
		ProtocolVersion: version.Protocol,
		Fingerprint:     hostFingerprint(),
	})
	if err != nil {
		e.logger.Warn("enrollment failed; monitoring continues locally and will retry", "error", err)
		return false
	}
	e.adopt(resp, "enrolled with cloud")
	return true
}

// recover re-binds an agent the control plane has stopped accepting. The
// request is signed with the agent's own key, so possession of that key is the
// proof. The enrollment token rides along only if the agent still has one,
// which is what lets a control plane that lost everything adopt the key again.
func (e *enrollment) recover(ctx context.Context) bool {
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	resp, err := protocol.Reenroll(callCtx, e.cfg.APIURL, e.signer, protocol.ReenrollRequest{
		InstallationID:  e.cfg.Identity.InstallationID,
		PublicKey:       e.cfg.Identity.PublicKey,
		PreviousAgentID: e.cfg.Identity.AgentID,
		ProtocolVersion: version.Protocol,
		Fingerprint:     hostFingerprint(),
		EnrollmentToken: e.cfg.EnrollmentToken,
	})
	switch {
	case err == nil:
		e.adopt(resp, "re-enrolled; the control plane had stopped accepting this agent")
		return true

	case errors.Is(err, protocol.ErrReenrollUnsupported):
		// An older control plane. The token is the only route it offers, and it
		// works whenever the operator has issued a fresh one.
		return e.enroll(ctx)

	case errors.Is(err, protocol.ErrUnauthorized):
		// Definitive: this control plane holds no record of the key and no
		// token it will accept. Say so once, with the operator's actual next
		// step, then back off — a restored database can still fix it later.
		e.nextAttempt = time.Now().Add(credentialRefusedRetryEvery)
		if !e.refusedLogged {
			e.refusedLogged = true
			e.logger.Error("the control plane no longer recognises this agent and refused re-enrolment",
				"reason", err,
				"next_step", "generate a fresh tracking key in the dashboard and re-run the install command on this host",
				"meanwhile", "monitoring continues locally and metrics are buffered")
		}
		return false

	default:
		e.logger.Warn("re-enrolment failed; monitoring continues locally and will retry", "error", err)
		return false
	}
}

// adopt records a freshly issued identity and points the live client at it.
func (e *enrollment) adopt(resp *protocol.EnrollResponse, msg string) {
	e.cfg.Identity.ServerID = resp.ServerID
	e.cfg.Identity.AgentID = resp.AgentID
	e.cfg.Identity.Enrolled = true
	e.rejected = false
	e.refusedLogged = false
	if err := config.SaveIdentity(e.cfg.DataDir, &e.cfg.Identity); err != nil {
		// An identity that cannot be written is an agent that has to enroll
		// again after every restart, so this is worth saying out loud.
		e.logger.Warn("could not persist identity; this agent will have to enroll again after a restart",
			"error", err, "data_dir", e.cfg.DataDir)
	}
	e.client.Rebind(resp.AgentID)
	e.logger.Info(msg, "server_id", resp.ServerID, "agent_id", resp.AgentID)
}

// send ships one message, repairing the credential if the control plane refuses
// it. One retry is enough: either the re-bind worked and the second attempt
// goes through, or it did not and the caller buffers exactly as it always has.
func (e *enrollment) send(ctx context.Context, msgType string, body any) error {
	err := e.client.Send(ctx, msgType, body)
	if !errors.Is(err, protocol.ErrUnauthorized) {
		return err
	}
	e.rejected = true
	if !e.due() || !e.attempt(ctx) {
		return err
	}
	return e.client.Send(ctx, msgType, body)
}

// hostFingerprint is the small, non-identifying description the control plane
// shows beside a server until the first discovery snapshot lands.
func hostFingerprint() map[string]string {
	hostname, _ := os.Hostname()
	return map[string]string{"hostname": hostname, "os": runtime.GOOS, "arch": runtime.GOARCH}
}

func runDiscovery(ctx context.Context, logger *slog.Logger, engine *discovery.Engine, enrol *enrollment, stateDir string) {
	start := time.Now()
	snap := engine.Run(ctx) // already redacted
	writeJSON(filepath.Join(stateDir, "discovery.json"), snap)
	logger.Info("discovery_completed",
		"duration_ms", time.Since(start).Milliseconds(),
		"resources", len(snap.Resources))

	if enrol != nil {
		sendCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		defer cancel()
		if err := enrol.send(sendCtx, "discovery", snap); err != nil {
			logger.Warn("discovery send failed; will retry next cycle", "error", err)
		}
	}
}

func flushBuffer(ctx context.Context, logger *slog.Logger, enrol *enrollment, buffer *cache.RingBuffer) {
	items := buffer.Drain()
	for _, it := range items {
		var raw json.RawMessage = it.Payload
		if err := enrol.send(ctx, "metrics", raw); err != nil {
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
