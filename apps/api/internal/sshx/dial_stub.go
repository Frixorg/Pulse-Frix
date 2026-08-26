//go:build !ssh

package sshx

import "context"

// Compiled reports whether this binary contains an SSH client. The default
// build is standard-library only (see go.mod), so the console is inert: the
// API answers /ssh/capabilities with enabled=false and the dashboard explains
// how to turn it on instead of offering a terminal that cannot connect.
//
// Build with `-tags ssh` (Docker: --build-arg TAGS=ssh) to compile it in; that
// build pulls golang.org/x/crypto. See docs/SSH_CONSOLE.md.
const Compiled = false

// Dial is the no-op implementation used by the default build.
func Dial(_ context.Context, _ Credentials, _, _ int) (Terminal, string, error) {
	return nil, "", ErrUnsupported
}

// InstallKey is the no-op implementation used by the default build.
func InstallKey(_ context.Context, _ Credentials, _ string) (*SetupResult, error) {
	return nil, ErrUnsupported
}
