module github.com/frix-me/pulse/api

go 1.22

// The default build uses only the Go standard library (net/http with the 1.22
// enhanced ServeMux). This keeps the control plane buildable and testable with
// zero external dependencies and a minimal supply chain.
//
// A production PostgreSQL store adapter lives in internal/store/postgres_pgx.go
// behind the `pgx` build tag. To use it:
//
//	go get github.com/jackc/pgx/v5
//	go build -tags pgx ./...
//
// The default (untagged) build ships an in-memory store used for development
// and tests. Schema lives in ./migrations.
