// Package version holds build and protocol version information for the agent.
package version

// These are overridden at build time via -ldflags, e.g.:
//
//	go build -ldflags "-X github.com/frix-me/pulse/agent/internal/version.Version=1.0.0"
var (
	// Version is the semantic version of the agent binary.
	Version = "0.1.0-dev"
	// Commit is the git commit the binary was built from.
	Commit = "unknown"
	// BuildDate is the RFC3339 build timestamp.
	BuildDate = "unknown"
)

// Protocol is the agent<->server protocol version this build speaks.
// The server advertises a supported range and negotiates on connect.
// See docs/AGENT_PROTOCOL.md.
const Protocol = "1.0"

// UserAgent returns the value used for the User-Agent / agent identification.
func UserAgent() string {
	return "pulse-agent/" + Version + " (protocol " + Protocol + ")"
}
