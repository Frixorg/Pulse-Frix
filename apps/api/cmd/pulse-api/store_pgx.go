//go:build pgx

package main

import (
	"fmt"
	"log/slog"

	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/store"
)

// buildStore returns the PostgreSQL-backed store. Requires:
//
//	go get github.com/jackc/pgx/v5
//	go build -tags pgx ./...
//
// and a reachable DATABASE_URL. The schema is applied here from the embedded
// files in ./migrations, so a fresh deployment never starts against an empty
// database.
func buildStore(cfg *config.Config, logger *slog.Logger) (store.Store, error) {
	logger.Info("using PostgreSQL store", "url_present", cfg.DatabaseURL != "")
	st, err := store.NewPostgres(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	// Idempotent, and run on every start. A control plane serving an empty
	// schema is indistinguishable — to the dashboard and to every agent —
	// from one that has lost all of its data, so this must not be a step an
	// install path can skip.
	if err := st.Migrate(); err != nil {
		return nil, fmt.Errorf("could not apply the database schema: %w", err)
	}
	logger.Info("database schema is up to date")
	return st, nil
}
