//go:build pgx

package main

import (
	"log/slog"

	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/store"
)

// buildStore returns the PostgreSQL-backed store. Requires:
//
//	go get github.com/jackc/pgx/v5
//	go build -tags pgx ./...
//
// and a reachable DATABASE_URL. Migrations live in ./migrations.
func buildStore(cfg *config.Config, logger *slog.Logger) (store.Store, error) {
	logger.Info("using PostgreSQL store", "url_present", cfg.DatabaseURL != "")
	return store.NewPostgres(cfg.DatabaseURL)
}
