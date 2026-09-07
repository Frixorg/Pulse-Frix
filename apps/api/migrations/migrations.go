// Package migrations carries the control-plane schema as embedded SQL so the
// API can apply it itself at start-up.
//
// The files stay plain .sql and can still be applied by hand with psql; the
// embedding only removes the separate step that a fresh deployment could skip.
// Skipping it produced a control plane serving an empty schema, which looks
// exactly like one that has lost every account and agent — see
// store.Postgres.Migrate.
package migrations

import "embed"

// FS holds every migration, applied in lexical filename order. Each file must
// be idempotent, because they are re-applied on every start.
//
//go:embed *.sql
var FS embed.FS
