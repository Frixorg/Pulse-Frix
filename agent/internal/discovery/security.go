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

// sshdSetting is one effective sshd_config value together with the file that
// supplied it. Naming the file is the whole diagnosis on a cloud image, where
// the answer is almost never /etc/ssh/sshd_config itself.
type sshdSetting struct {
	value  string
	source string
}

// sshdConfig is the effective GLOBAL configuration, resolved the way sshd
// resolves it.
type sshdConfig struct {
	settings map[string]sshdSetting
	files    []string
}

// set records a keyword only if it has not been seen yet: first match wins.
func (c *sshdConfig) set(keyword, value, source string) {
	k := strings.ToLower(keyword)
	if _, seen := c.settings[k]; seen {
		return
	}
	c.settings[k] = sshdSetting{value: value, source: source}
}

func (c *sshdConfig) value(keyword string) string {
	return c.settings[strings.ToLower(keyword)].value
}

func (c *sshdConfig) source(keyword string) string {
	return c.settings[strings.ToLower(keyword)].source
}

// splitDirective separates a keyword from its argument. sshd accepts either
// whitespace or an optional '=' between the two, so "PasswordAuthentication=no"
// and "PasswordAuthentication no" are the same directive.
func splitDirective(line string) (string, string) {
	i := strings.IndexFunc(line, func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(strings.TrimLeft(line[i:], " \t="))
}

// includePaths expands one Include directive. A relative pattern resolves
// against /etc/ssh, and each glob expands in lexical order — which is why
// 00-hardening.conf is parsed before 99-tunnel-key.conf, and therefore wins.
func includePaths(root, spec string) []string {
	var out []string
	for _, pattern := range strings.Fields(spec) {
		pattern = strings.Trim(pattern, `"`)
		if !strings.HasPrefix(pattern, "/") {
			pattern = filepath.Join("/etc/ssh", pattern)
		}
		matches, _ := filepath.Glob(filepath.Join(root, pattern))
		sort.Strings(matches)
		out = append(out, matches...)
	}
	return out
}

// parseSSHD walks one config file in sshd's own order, following Include
// directives at the point they appear. depth bounds the recursion.
func parseSSHD(root, path string, depth int, cfg *sshdConfig) {
	if depth > 8 {
		return
	}
	lines := readLines(path)
	if lines == nil {
		return
	}
	cfg.files = append(cfg.files, displayPath(path))

	// Everything after a Match header is conditional on the connection, so it
	// is not part of the global posture. "Match all" returns to unconditional.
	conditional := false
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keyword, value := splitDirective(line)
		switch strings.ToLower(keyword) {
		case "match":
			conditional = !strings.EqualFold(value, "all")
			continue
		case "include":
			for _, inc := range includePaths(root, value) {
				parseSSHD(root, inc, depth+1, cfg)
			}
			continue
		}
		if conditional || value == "" {
			continue
		}
		cfg.set(keyword, value, displayPath(path))
	}
}

// firstField returns the leading token of a value, for single-argument keywords.
func firstField(v string) string {
	if f := strings.Fields(v); len(f) > 0 {
		return f[0]
	}
	return ""
}

// passwordLoginAvailable reports whether sshd will offer password auth at all.
// An unset PasswordAuthentication defaults to yes.
func passwordLoginAvailable(passAuth string) bool { return passAuth != "no" }

// rootPasswordLoginAvailable applies the second gate that catches people out.
// Since OpenSSH 7.0 an unset PermitRootLogin defaults to prohibit-password, so
// root gets no password prompt unless the host opted back in explicitly — the
// daemon simply advertises publickey and disconnects.
func rootPasswordLoginAvailable(passAuth, permitRoot string) bool {
	return passwordLoginAvailable(passAuth) && permitRoot == "yes"
}

// readSSHDConfig resolves the EFFECTIVE sshd posture the way sshd does.
//
// Two rules decide the answer, and getting either wrong reports the opposite of
// the truth:
//
//   - Include is expanded where it appears. Debian and Ubuntu images put
//     "Include /etc/ssh/sshd_config.d/*.conf" at the TOP of sshd_config, so the
//     drop-ins are parsed before anything written below them.
//   - The FIRST value obtained for a keyword wins. Later ones are ignored.
//
// So a 00-hardening.conf that sets "PasswordAuthentication no" beats the
// "PasswordAuthentication yes" further down sshd_config. Reading the main file
// first, or letting the last match win, reports password logins as available on
// a host that refuses them outright.
func readSSHDConfig(root string) *model.Resource {
	cfg := &sshdConfig{settings: map[string]sshdSetting{}}
	parseSSHD(root, filepath.Join(root, "/etc/ssh/sshd_config"), 0, cfg)
	// A host can be configured entirely from drop-ins, so look there even when
	// the main file is missing and the include chain never ran.
	if len(cfg.files) == 0 {
		for _, p := range includePaths(root, "/etc/ssh/sshd_config.d/*.conf") {
			parseSSHD(root, p, 1, cfg)
		}
	}
	if len(cfg.files) == 0 {
		return nil
	}

	permitRoot := strings.ToLower(firstField(cfg.value("permitrootlogin")))
	passAuth := strings.ToLower(firstField(cfg.value("passwordauthentication")))
	emptyPass := strings.ToLower(firstField(cfg.value("permitemptypasswords")))

	attrs := map[string]any{
		"permit_root_login":       orElse(permitRoot, "default"),
		"password_authentication": orElse(passAuth, "default"),
		"permit_empty_passwords":  orElse(emptyPass, "no"),
		"config_files":            cfg.files,
		// What an operator staring at "Permission denied" actually needs.
		"password_login_available":      passwordLoginAvailable(passAuth),
		"root_password_login_available": rootPasswordLoginAvailable(passAuth, permitRoot),
	}
	for attr, keyword := range map[string]string{
		"permit_root_login_source":       "permitrootlogin",
		"password_authentication_source": "passwordauthentication",
		"permit_empty_passwords_source":  "permitemptypasswords",
	} {
		if src := cfg.source(keyword); src != "" {
			attrs[attr] = src
		}
	}
	if weak := weakItems(cfg.value("ciphers"), weakCipherPatterns); len(weak) > 0 {
		attrs["weak_ciphers"] = weak
	}
	if weak := weakItems(cfg.value("macs"), weakMacPatterns); len(weak) > 0 {
		attrs["weak_macs"] = weak
	}
	if weak := weakItems(cfg.value("kexalgorithms"), weakKexPatterns); len(weak) > 0 {
		attrs["weak_kex"] = weak
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
