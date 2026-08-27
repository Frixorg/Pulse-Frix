package discovery

import "testing"

// parseCgroup is what separates a host-native workload from a containerised
// one, so both cgroup generations and both Docker layouts are covered.
func TestParseCgroup(t *testing.T) {
	cases := []struct {
		name      string
		content   string
		wantUnit  string
		wantIsCtr bool
	}{
		{
			name:     "cgroup v2 host service",
			content:  "0::/system.slice/nginx.service",
			wantUnit: "nginx.service",
		},
		{
			name:      "cgroup v2 nested slice",
			content:   "0::/system.slice/system-getty.slice/getty@tty1.service",
			wantUnit:  "getty@tty1.service",
			wantIsCtr: false,
		},
		{
			name:      "cgroup v1 docker",
			content:   "1:name=systemd:/docker/3f2a1b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708",
			wantIsCtr: true,
		},
		{
			name:      "systemd-managed docker scope",
			content:   "0::/system.slice/docker-3f2a1b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708.scope",
			wantIsCtr: true,
		},
		{
			name:     "no unit and no container",
			content:  "0::/",
			wantUnit: "",
		},
		{
			name:     "empty input",
			content:  "",
			wantUnit: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			unit, container := parseCgroup(tc.content)
			if unit != tc.wantUnit {
				t.Errorf("unit: got %q, want %q", unit, tc.wantUnit)
			}
			if (container != "") != tc.wantIsCtr {
				t.Errorf("container: got %q, want containerised=%v", container, tc.wantIsCtr)
			}
			if container != "" && len(container) != 12 {
				t.Errorf("container id should be shortened to 12 chars, got %q", container)
			}
		})
	}
}

func TestShortContainerID(t *testing.T) {
	long := "3f2a1b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708"
	if got := shortContainerID(long); got != "3f2a1b4c5d6e" {
		t.Errorf("got %q", got)
	}
	if got := shortContainerID("abc"); got != "abc" {
		t.Errorf("a short id must be left alone, got %q", got)
	}
}

func TestIsHexID(t *testing.T) {
	if !isHexID("3f2a1b4c5d6e7f8091a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f708") {
		t.Error("a 64-char lowercase hex string is a container id")
	}
	if isHexID("system.slice") {
		t.Error("a slice name is not a container id")
	}
	if isHexID("3F2A1B4C5D6E7F8091A2B3C4D5E6F708192A3B4C5D6E7F8091A2B3C4D5E6F708") {
		t.Error("uppercase is not the cgroup form and must not match")
	}
}

func TestDisplayPathStripsRootfs(t *testing.T) {
	t.Setenv("PULSE_ROOTFS", "/host")
	if got := displayPath("/host/etc/nginx/nginx.conf"); got != "/etc/nginx/nginx.conf" {
		t.Errorf("got %q", got)
	}
	// A path that is not under the rootfs is returned untouched.
	if got := displayPath("/etc/nginx/nginx.conf"); got != "/etc/nginx/nginx.conf" {
		t.Errorf("got %q", got)
	}
	if got := hostPath("/etc/hosts"); got != "/host/etc/hosts" {
		t.Errorf("got %q", got)
	}
}
