package discovery

import "testing"

func TestSudoDisabledByDefault(t *testing.T) {
	t.Setenv("PULSE_USE_SUDO", "")
	if sudoEnabled() {
		t.Error("privilege escalation must be off unless the operator opts in")
	}
	for _, v := range []string{"false", "0", "no", "off", "maybe"} {
		t.Setenv("PULSE_USE_SUDO", v)
		if sudoEnabled() {
			t.Errorf("%q should not enable sudo", v)
		}
	}
	for _, v := range []string{"true", "1", "yes", "on", "TRUE"} {
		t.Setenv("PULSE_USE_SUDO", v)
		if !sudoEnabled() {
			t.Errorf("%q should enable sudo", v)
		}
	}
}

func TestPortFromSSAddress(t *testing.T) {
	cases := map[string]int{
		"0.0.0.0:80":     80,
		"[::]:443":       443,
		"*:9100":         9100,
		"127.0.0.1:5432": 5432,
		"garbage":        0,
		"0.0.0.0:":       0,
		"":               0,
	}
	for in, want := range cases {
		if got := portFromSSAddress(in); got != want {
			t.Errorf("%q: got %d, want %d", in, got, want)
		}
	}
}

func TestSSUsersRegexp(t *testing.T) {
	line := `tcp   LISTEN 0 4096 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=812,fd=3))`
	m := ssUsers.FindStringSubmatch(line)
	if m == nil {
		t.Fatal("expected the users column to match")
	}
	if m[1] != "sshd" || m[2] != "812" {
		t.Errorf("got process %q pid %q", m[1], m[2])
	}
	if ssUsers.FindStringSubmatch("tcp LISTEN 0 4096 0.0.0.0:22 0.0.0.0:*") != nil {
		t.Error("a line with no users column must not match")
	}
}
