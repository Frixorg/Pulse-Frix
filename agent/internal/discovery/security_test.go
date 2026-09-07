package discovery

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSSHD lays out a host rootfs with a main sshd_config and drop-ins.
func writeSSHD(t *testing.T, main string, dropins map[string]string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "etc", "ssh", "sshd_config.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if main != "" {
		if err := os.WriteFile(filepath.Join(root, "etc", "ssh", "sshd_config"), []byte(main), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range dropins {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PULSE_ROOTFS", root)
	return root
}

// The layout that locked an operator out: a stock Ubuntu sshd_config whose
// Include sits at the top and whose own directives are commented out, plus
// hardening drop-ins that turn password auth off. sshd keeps the FIRST value it
// obtains, so the drop-in wins — even though sshd_config is listed first and
// says "yes" further down.
func TestSSHDDropInBeatsMainConfig(t *testing.T) {
	root := writeSSHD(t, `
Include /etc/ssh/sshd_config.d/*.conf

#PermitRootLogin prohibit-password
PasswordAuthentication yes
`, map[string]string{
		"00-hardening.conf":         "PermitRootLogin prohibit-password\nPasswordAuthentication no\n",
		"60-cloudimg-settings.conf": "PasswordAuthentication no\n",
		"99-tunnel-key.conf":        "PasswordAuthentication no\n",
	})

	res := readSSHDConfig(root)
	if res == nil {
		t.Fatal("expected an ssh_config resource")
	}
	if got := res.Attributes["password_authentication"]; got != "no" {
		t.Errorf("password_authentication = %v, want no (the drop-in is parsed first and wins)", got)
	}
	if got := res.Attributes["permit_root_login"]; got != "prohibit-password" {
		t.Errorf("permit_root_login = %v, want prohibit-password", got)
	}
	if got := res.Attributes["password_login_available"]; got != false {
		t.Errorf("password_login_available = %v, want false", got)
	}
	if got := res.Attributes["root_password_login_available"]; got != false {
		t.Errorf("root_password_login_available = %v, want false", got)
	}
	// The source is the whole diagnosis: it says which file to edit.
	if got := res.Attributes["password_authentication_source"]; got != "/etc/ssh/sshd_config.d/00-hardening.conf" {
		t.Errorf("password_authentication_source = %v, want the 00-hardening.conf drop-in", got)
	}
}

// With no PermitRootLogin anywhere, OpenSSH >= 7.0 defaults to
// prohibit-password, so root still gets no password prompt even though
// PasswordAuthentication is on for everyone else.
func TestSSHDRootDefaultsToProhibitPassword(t *testing.T) {
	root := writeSSHD(t, "PasswordAuthentication yes\n", nil)

	res := readSSHDConfig(root)
	if res == nil {
		t.Fatal("expected an ssh_config resource")
	}
	if got := res.Attributes["password_login_available"]; got != true {
		t.Errorf("password_login_available = %v, want true", got)
	}
	if got := res.Attributes["root_password_login_available"]; got != false {
		t.Errorf("root_password_login_available = %v, want false (default is prohibit-password)", got)
	}
}

// Directives inside a Match block are conditional on the connection and must
// never be reported as the global posture.
func TestSSHDIgnoresMatchBlocks(t *testing.T) {
	root := writeSSHD(t, `
PasswordAuthentication no

Match Address 10.0.0.0/8
    PasswordAuthentication yes
`, nil)

	res := readSSHDConfig(root)
	if res == nil {
		t.Fatal("expected an ssh_config resource")
	}
	if got := res.Attributes["password_authentication"]; got != "no" {
		t.Errorf("password_authentication = %v, want no (the Match block is conditional)", got)
	}
}

// A host configured entirely from drop-ins, with no main sshd_config at all.
func TestSSHDDropInsOnly(t *testing.T) {
	root := writeSSHD(t, "", map[string]string{
		"50-cloud-init.conf": "PasswordAuthentication=no\n",
	})

	res := readSSHDConfig(root)
	if res == nil {
		t.Fatal("expected an ssh_config resource from drop-ins alone")
	}
	// "Keyword=value" is as valid as "Keyword value".
	if got := res.Attributes["password_authentication"]; got != "no" {
		t.Errorf("password_authentication = %v, want no", got)
	}
}

// Nothing readable means nothing reported — never a guess.
func TestSSHDAbsentReportsNothing(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PULSE_ROOTFS", root)
	if res := readSSHDConfig(root); res != nil {
		t.Errorf("expected no resource when no config is readable, got %+v", res)
	}
}
