//go:build !pgx

package main

import (
	"log/slog"

	"github.com/frix-me/pulse/api/internal/config"
	"github.com/frix-me/pulse/api/internal/store"
)

// buildStore returns the default in-memory store. Build with -tags pgx to use
// PostgreSQL instead (see store_pgx.go and internal/store/postgres_pgx.go).
func buildStore(cfg *config.Config, logger *slog.Logger) (store.Store, error) {
	logger.Info("using in-memory store (development default)")
	_ = cfg
	return store.NewMemory(), nil
}
