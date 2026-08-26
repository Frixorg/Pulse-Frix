module github.com/frix-me/pulse/api

go 1.22

// The control plane is standard-library only except for ONE dependency:
// golang.org/x/crypto, which provides the SSH client behind the browser
// console (internal/sshx). It is in the default build so the SSH tab works
// with no build flags. To drop it and get a pure-stdlib binary with the
// console compiled out:
//
//	go build -tags nosshconsole ./...
//
// A production PostgreSQL store adapter lives in internal/store/postgres_pgx.go
// behind the `pgx` build tag. It is deliberately NOT required here, so the
// default build never pulls it. To use it:
//
//	go get github.com/jackc/pgx/v5
//	go build -tags pgx ./...
//
// The default (untagged) build ships an in-memory store used for development
// and tests. Schema lives in ./migrations.

require golang.org/x/crypto v0.31.0

require golang.org/x/sys v0.28.0 // indirect
