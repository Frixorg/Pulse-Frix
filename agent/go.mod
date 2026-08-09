module github.com/frix-me/pulse/agent

go 1.22

// The agent depends only on the Go standard library. This keeps the binary
// small, the supply chain minimal, and the build reproducible. Docker is
// accessed over its unix socket via net/http (no Docker SDK required).
