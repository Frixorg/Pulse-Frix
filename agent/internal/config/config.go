// Package config loads agent configuration from the environment and manages the
// agent's cryptographic identity. Secrets are generated locally and NEVER
// hardcoded; the Ed25519 private key never leaves the VPS.
package config

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Mode is the deployment mode.
type Mode string

const (
	ModeLocal Mode = "local"
	ModeCloud Mode = "cloud"
)

// Config is the runtime configuration for the agent.
type Config struct {
	Mode            Mode
	APIURL          string
	EnrollmentToken string
	DataDir         string
	DockerSocket    string
	DiscoveryEvery  time.Duration
	MetricsEvery    time.Duration
	LogLevel        string
	LogFormat       string

	Identity Identity
}

// Identity holds the agent's stable, cryptographically-secure identifiers.
// Never derived from hostname / IP / MAC alone. See docs/AGENT_PROTOCOL.md.
type Identity struct {
	InstallationID string `json:"installation_id"`
	ServerID       string `json:"server_id"`
	AgentID        string `json:"agent_id"`
	PublicKey      string `json:"public_key"`  // base64 ed25519 public key
	PrivateKey     string `json:"private_key"` // base64 ed25519 private key (local only)
	Enrolled       bool   `json:"enrolled"`    // true once registered with the cloud
}

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// Load reads configuration from the environment, applies safe defaults, and
// loads-or-creates the agent identity under DataDir.
func Load() (*Config, error) {
	cfg := &Config{
		Mode:            Mode(env("PULSE_MODE", "local")),
		APIURL:          env("PULSE_API_URL", "https://pulse.frix.me"),
		EnrollmentToken: env("PULSE_ENROLLMENT_TOKEN", ""),
		DataDir:         env("PULSE_DATA_DIR", defaultDataDir()),
		DockerSocket:    env("PULSE_DOCKER_SOCKET", "/var/run/docker.sock"),
		DiscoveryEvery:  durationEnv("PULSE_DISCOVERY_INTERVAL", 60*time.Second),
		MetricsEvery:    durationEnv("PULSE_METRICS_INTERVAL", 15*time.Second),
		LogLevel:        env("PULSE_LOG_LEVEL", "info"),
		LogFormat:       env("PULSE_LOG_FORMAT", "json"),
	}
	if cfg.Mode != ModeLocal && cfg.Mode != ModeCloud {
		return nil, errors.New("PULSE_MODE must be 'local' or 'cloud'")
	}
	id, err := loadOrCreateIdentity(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	// Allow the server id to be provided explicitly (e.g. from the installer).
	if sid := env("PULSE_SERVER_ID", ""); sid != "" {
		id.ServerID = sid
	}
	cfg.Identity = *id
	return cfg, nil
}

func defaultDataDir() string {
	if fi, err := os.Stat("/opt/pulse"); err == nil && fi.IsDir() {
		return "/opt/pulse/agent"
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pulse")
}

func durationEnv(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

// loadOrCreateIdentity reads identity.json or creates a new identity (with a
// fresh Ed25519 keypair) if none exists.
func loadOrCreateIdentity(dir string) (*Identity, error) {
	path := filepath.Join(dir, "identity.json")
	if b, err := os.ReadFile(path); err == nil {
		var id Identity
		if err := json.Unmarshal(b, &id); err != nil {
			return nil, err
		}
		return &id, nil
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	id := &Identity{
		InstallationID: randomID("ins"),
		ServerID:       randomID("srv"),
		AgentID:        randomID("agt"),
		PublicKey:      base64.StdEncoding.EncodeToString(pub),
		PrivateKey:     base64.StdEncoding.EncodeToString(priv),
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(id, "", "  ")
	// Identity contains the private key: strict 0600 perms, local only.
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return nil, err
	}
	return id, nil
}

// SaveIdentity atomically persists identity.json (contains the private key;
// written 0600, local only).
func SaveIdentity(dir string, id *Identity) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "identity.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Signer returns the Ed25519 private key for request signing.
func (id Identity) Signer() (ed25519.PrivateKey, error) {
	raw, err := base64.StdEncoding.DecodeString(id.PrivateKey)
	if err != nil {
		return nil, err
	}
	if len(raw) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key size")
	}
	return ed25519.PrivateKey(raw), nil
}

// randomID returns a cryptographically-secure identifier with a short prefix.
func randomID(prefix string) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b)
}
