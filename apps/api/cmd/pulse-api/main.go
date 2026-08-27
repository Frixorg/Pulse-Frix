// Command pulse-api is the Pulse control-plane API. It serves the REST API and
// the agent ingest endpoint. The default build uses an in-memory store; build
// with -tags pgx for the PostgreSQL adapter.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/frix-me/pulse/api/internal/auth"
	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/httpx"
	"github.com/frix-me/pulse/api/internal/model"
	"github.com/frix-me/pulse/api/internal/store"
)

func main() {
	cfg := config.Load()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	st, err := buildStore(cfg, logger)
	if err != nil {
		logger.Error("failed to initialise store", "error", err)
		os.Exit(1)
	}

	bootstrapOwner(st, logger)
	bootstrapEnrollment(st, logger, cfg)

	srv := httpx.New(cfg, st, logger)
	httpServer := srv.HTTPServer()

	go func() {
		logger.Info("pulse-api listening",
			"addr", cfg.Addr, "mode", cfg.Mode, "tenancy", cfg.Tenancy)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("http server error", "error", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	// Hijacked connections (open SSH consoles) are not covered by
	// http.Server.Shutdown, so close them explicitly.
	srv.Shutdown()
}

// bootstrapOwner seeds the initial administrator from the environment when the
// store holds no account yet. This is the installer's provisioning path; when
// the variables are unset the API instead offers the first-boot wizard at
// /setup (see internal/httpx/handlers_setup.go).
//
// ADMIN_EMAIL / ADMIN_PASSWORD are the documented names; the older
// PULSE_BOOTSTRAP_* pair still works so existing .env files keep running. The
// password is always provided out-of-band and only its PBKDF2 hash is stored.
func bootstrapOwner(st store.Store, logger *slog.Logger) {
	email := auth.NormalizeEmail(firstEnv("ADMIN_EMAIL", "PULSE_BOOTSTRAP_EMAIL"))
	password := firstEnv("ADMIN_PASSWORD", "PULSE_BOOTSTRAP_PASSWORD")
	if email == "" || password == "" {
		return
	}
	if err := auth.ValidateEmail(email); err != nil {
		logger.Error("bootstrap: ADMIN_EMAIL is not valid", "error", err)
		return
	}
	if _, _, err := st.FindLoginByEmail(email); err == nil {
		return // already exists
	}
	// A weak seeded password is refused outright rather than silently baked
	// into a deployment that is about to be exposed to the internet.
	if err := auth.ValidatePasswordPolicy(password, email); err != nil {
		logger.Error("bootstrap: ADMIN_PASSWORD rejected", "error", err,
			"hint", "set a stronger value, or leave it unset and use the /setup wizard")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		logger.Error("bootstrap: could not hash password", "error", err)
		return
	}
	if _, _, err := st.SeedOrgOwner("Default", email, hash); err != nil {
		logger.Warn("bootstrap: could not seed owner", "error", err)
		return
	}
	logger.Info("bootstrap: seeded initial administrator", "email", email)
}

// firstEnv returns the first non-empty environment variable among keys.
func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

// bootstrapEnrollment seeds a single enrollment token (from the environment) so
// the local agent can enroll over the same secure enroll+ingest path used in
// cloud mode. Set by the installer for self-hosted deployments. The token value
// is provided out-of-band (env) and only its hash is stored.
func bootstrapEnrollment(st store.Store, logger *slog.Logger, cfg *config.Config) {
	plain := os.Getenv("PULSE_BOOTSTRAP_ENROLLMENT_TOKEN")
	ownerEmail := auth.NormalizeEmail(firstEnv("ADMIN_EMAIL", "PULSE_BOOTSTRAP_EMAIL"))
	if plain == "" || ownerEmail == "" {
		return
	}
	_, mem, err := st.FindLoginByEmail(ownerEmail)
	if err != nil || mem == nil {
		return
	}
	tok := &model.EnrollmentToken{
		OrgID:     mem.OrgID,
		TokenHash: auth.HashToken(plain),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	if err := st.CreateEnrollmentToken(tok); err != nil {
		logger.Warn("bootstrap: could not seed enrollment token", "error", err)
		return
	}
	_ = cfg
	logger.Info("bootstrap: seeded enrollment token for self-hosted agent")
}
