//go:build nosshconsole

package sshx

import "context"

// This file builds only with `-tags nosshconsole`, which produces a control
// plane with no SSH client at all — a standard-library-only binary for
// deployments that want the smallest possible dependency surface and have no
// use for the browser console.
//
// The DEFAULT build compiles dial_ssh.go instead, so the SSH tab works out of
// the box with no build flags. See docs/SSH_CONSOLE.md.

// Compiled reports whether this binary contains an SSH client.
const Compiled = false

// Dial is the no-op implementation used by the nosshconsole build.
func Dial(_ context.Context, _ Credentials, _, _ int) (Terminal, string, error) {
	return nil, "", ErrUnsupported
}

// InstallKey is the no-op implementation used by the nosshconsole build.
func InstallKey(_ context.Context, _ Credentials, _ string) (*SetupResult, error) {
	return nil, ErrUnsupported
}
