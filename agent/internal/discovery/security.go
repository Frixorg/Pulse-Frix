package discovery

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/frix-me/pulse/agent/internal/model"
)

// SecurityDetector reads host security posture that other detectors don't cover:
// SSH daemon hardening and shared SSH keys. It is strictly READ-ONLY and reads
// under the host rootfs (PULSE_ROOTFS, e.g. /host) when the agent runs
// containerised. If the files aren't readable it simply emits nothing.
type SecurityDetector struct{}

func (SecurityDetector) ID() string      { return "security" }
func (SecurityDetector) Name() string    { return "Security Detector" }
func (SecurityDetector) Version() string { return "1.0" }

func (SecurityDetector) Available(context.Context) model.Availability {
	return model.Availability{Available: true}
}

func (SecurityDetector) Detect(context.Context) ([]model.Resource, error) {
	root := strings.TrimRight(os.Getenv("PULSE_ROOTFS"), "/")
	var out []model.Resource
	if r := readSSHDConfig(root); r != nil {
		out = append(out, *r)
	}
	if r := analyzeAuthorizedKeys(root); r != nil {
		out = append(out, *r)
	}
	return out, nil
}

func (SecurityDetector) Health(context.Context) model.HealthReport {
	return model.HealthReport{Status: model.StatusHealthy}
}

var (
	weakCipherPatterns = []string{"3des", "cbc", "arcfour", "rc4", "des-", "blowfish", "cast128"}
	weakMacPatterns    = []string{"md5", "sha1", "-96", "umac-64"}
	weakKexPatterns    = []string{"group1-", "group-exchange-sha1", "diffie-hellman-group1", "rsa1024", "gss-group1"}
)

// readSSHDConfig parses sshd_config (+ sshd_config.d/*.conf) for hardening posture.
func readSSHDConfig(root string) *model.Resource {
	paths := []string{filepath.Join(root, "/etc/ssh/sshd_config")}
	extra, _ := filepath.Glob(filepath.Join(root, "/etc/ssh/sshd_config.d/*.conf"))
	paths = append(paths, extra...)

	found := false
	permitRoot, passAuth, emptyPass := "", "", ""
	var weakC, weakM, weakK []string
	for _, p := range paths {
		for _, raw := range readLines(p) {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			found = true
			val := strings.Join(fields[1:], " ")
			switch strings.ToLower(fields[0]) {
			case "permitrootlogin":
				permitRoot = strings.ToLower(fields[1])
			case "passwordauthentication":
				passAuth = strings.ToLower(fields[1])
			case "permitemptypasswords":
				emptyPass = strings.ToLower(fields[1])
			case "ciphers":
				weakC = weakItems(val, weakCipherPatterns)
			case "macs":
				weakM = weakItems(val, weakMacPatterns)
			case "kexalgorithms":
				weakK = weakItems(val, weakKexPatterns)
			}
		}
	}
	if !found {
		return nil
	}
	attrs := map[string]any{
		"permit_root_login":       orElse(permitRoot, "default"),
		"password_authentication": orElse(passAuth, "default"),
		"permit_empty_passwords":  orElse(emptyPass, "no"),
	}
	if len(weakC) > 0 {
		attrs["weak_ciphers"] = weakC
	}
	if len(weakM) > 0 {
		attrs["weak_macs"] = weakM
	}
	if len(weakK) > 0 {
		attrs["weak_kex"] = weakK
	}
	return &model.Resource{
		Type: "ssh_config", ID: "ssh:config", Name: "sshd",
		Health: model.StatusHealthy, DetectedBy: "security", DetectedAt: time.Now().UTC(),
		Attributes: attrs,
	}
}

// analyzeAuthorizedKeys detects the same public key reused across multiple users.
func analyzeAuthorizedKeys(root string) *model.Resource {
	files := map[string]string{"root": filepath.Join(root, "/root/.ssh/authorized_keys")}
	homes, _ := filepath.Glob(filepath.Join(root, "/home/*"))
	for _, h := range homes {
		files[filepath.Base(h)] = filepath.Join(h, ".ssh/authorized_keys")
	}

	keyUsers := map[string]map[string]bool{}
	seenAny := false
	for user, path := range files {
		for _, raw := range readLines(path) {
			line := strings.TrimSpace(raw)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			blob := ""
			for _, tok := range strings.Fields(line) {
				if len(tok) > 80 && !strings.Contains(tok, "@") && !strings.HasPrefix(tok, "ssh-") {
					blob = tok
					break
				}
			}
			if blob == "" {
				continue
			}
			seenAny = true
			if keyUsers[blob] == nil {
				keyUsers[blob] = map[string]bool{}
			}
			keyUsers[blob][user] = true
		}
	}
	if !seenAny {
		return nil
	}
	var shared []string
	for _, users := range keyUsers {
		if len(users) > 1 {
			us := make([]string, 0, len(users))
			for u := range users {
				us = append(us, u)
			}
			sort.Strings(us)
			shared = append(shared, strings.Join(us, ", "))
		}
	}
	attrs := map[string]any{"shared": len(shared) > 0}
	if len(shared) > 0 {
		attrs["shared_keys"] = shared
	}
	return &model.Resource{
		Type: "ssh_keys", ID: "ssh:keys", Name: "authorized_keys",
		Health: model.StatusHealthy, DetectedBy: "security", DetectedAt: time.Now().UTC(),
		Attributes: attrs,
	}
}

func weakItems(list string, patterns []string) []string {
	var out []string
	for _, item := range strings.Split(list, ",") {
		it := strings.ToLower(strings.TrimSpace(item))
		if it == "" {
			continue
		}
		for _, p := range patterns {
			if strings.Contains(it, p) {
				out = append(out, strings.TrimSpace(item))
				break
			}
		}
	}
	return out
}

func orElse(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
