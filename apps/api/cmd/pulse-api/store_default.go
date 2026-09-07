//go:build !pgx

package main

import (
	"errors"
	"log/slog"
	"os"
	"strings"

	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/store"
)

// buildStore returns the in-memory store, which is for development and tests.
// Build with -tags pgx to use PostgreSQL (see store_pgx.go).
//
// Refusing to serve production traffic from it is deliberate. Everything this
// control plane knows — accounts, servers, and every agent binding — lives in
// process memory, so an ordinary restart is indistinguishable from total data
// loss: the dashboard comes back empty and every agent is refused at ingest
// with no way to tell that anything is wrong. Nobody should reach that state
// by accident, and until now the default build was the way they did.
func buildStore(cfg *config.Config, logger *slog.Logger) (store.Store, error) {
	if cfg.Env == "production" && !allowEphemeralStore() {
		return nil, errors.New(
			"this API was built without the pgx tag, so it can only keep data in memory — " +
				"a restart would lose every account and every agent registration. " +
				"Rebuild with API_TAGS=pgx and set DATABASE_URL, or set " +
				"PULSE_ALLOW_EPHEMERAL_STORE=true if this really is a throwaway deployment")
	}
	logger.Warn("using the in-memory store: nothing here survives a restart of this process")
	return store.NewMemory(), nil
}

// allowEphemeralStore lets someone opt into a deliberately disposable
// deployment. It has to be explicit, because the failure it permits is silent.
func allowEphemeralStore() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PULSE_ALLOW_EPHEMERAL_STORE"))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
