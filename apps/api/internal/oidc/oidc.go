// Package oidc implements a minimal, standard-library OpenID Connect client
// (Authorization Code flow + PKCE). One implementation serves every provider —
// Google, Telegram, or any OIDC-compliant issuer — via their discovery document.
// See docs/API.md.
//
// Security: the code exchange happens server-to-server over TLS with a
// confidential client, and is bound by `state` (CSRF) + PKCE. Identity claims
// are read from the id_token returned over that TLS channel (and, when needed,
// the userinfo endpoint).
package oidc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ProviderConfig configures one identity provider.
type ProviderConfig struct {
	Name         string // url slug, e.g. "google"
	Display      string
	Issuer       string
	ClientID     string
	ClientSecret string
	Scopes       []string
	RedirectURI  string
}

// Claims is the normalized identity we extract from a login.
type Claims struct {
	Provider      string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// discovery is the subset of the OIDC discovery document we use.
type discovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	UserinfoEndpoint      string   `json:"userinfo_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

type provider struct {
	cfg  ProviderConfig
	once sync.Once
	disc *discovery
	err  error
}

// Manager holds configured providers and pending auth states.
type Manager struct {
	http      *http.Client
	providers map[string]*provider
	states    *stateStore
}

// NewManager builds a manager from provider configs.
func NewManager(cfgs []ProviderConfig) *Manager {
	m := &Manager{
		http:      &http.Client{Timeout: 15 * time.Second},
		providers: map[string]*provider{},
		states:    newStateStore(),
	}
	for _, c := range cfgs {
		m.providers[c.Name] = &provider{cfg: c}
	}
	return m
}

// Has reports whether a provider is configured.
func (m *Manager) Has(name string) bool { _, ok := m.providers[name]; return ok }

// Names returns the configured provider slugs.
func (m *Manager) Names() []string {
	out := make([]string, 0, len(m.providers))
	for n := range m.providers {
		out = append(out, n)
	}
	return out
}

func (m *Manager) discover(ctx context.Context, p *provider) (*discovery, error) {
	p.once.Do(func() {
		u := strings.TrimRight(p.cfg.Issuer, "/") + "/.well-known/openid-configuration"
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		resp, err := m.http.Do(req)
		if err != nil {
			p.err = err
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			p.err = fmt.Errorf("discovery %s: status %d", p.cfg.Name, resp.StatusCode)
			return
		}
		var d discovery
		if err := json.NewDecoder(resp.Body).Decode(&d); err != nil {
			p.err = err
			return
		}
		p.disc = &d
	})
	return p.disc, p.err
}

// Start creates a PKCE + state challenge and returns the provider's
// authorization URL to redirect the user to.
func (m *Manager) Start(ctx context.Context, name string) (string, error) {
	p, ok := m.providers[name]
	if !ok {
		return "", errors.New("unknown provider")
	}
	d, err := m.discover(ctx, p)
	if err != nil {
		return "", err
	}

	verifier := randToken(32)
	challenge := s256(verifier)
	state := randToken(24)
	nonce := randToken(16)
	m.states.put(state, stateData{Provider: name, Verifier: verifier, Nonce: nonce, Created: time.Now()})

	scopes := chooseScopes(p.cfg.Scopes, d.ScopesSupported)
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", p.cfg.ClientID)
	q.Set("redirect_uri", p.cfg.RedirectURI)
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)
	q.Set("nonce", nonce)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	return d.AuthorizationEndpoint + "?" + q.Encode(), nil
}

// Callback validates the state, exchanges the code, and returns normalized claims.
func (m *Manager) Callback(ctx context.Context, name, code, state string) (*Claims, error) {
	p, ok := m.providers[name]
	if !ok {
		return nil, errors.New("unknown provider")
	}
	sd, ok := m.states.take(state)
	if !ok || sd.Provider != name {
		return nil, errors.New("invalid or expired state")
	}
	d, err := m.discover(ctx, p)
	if err != nil {
		return nil, err
	}

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", p.cfg.RedirectURI)
	form.Set("client_id", p.cfg.ClientID)
	form.Set("client_secret", p.cfg.ClientSecret)
	form.Set("code_verifier", sd.Verifier)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed: status %d", resp.StatusCode)
	}
	var tok struct {
		IDToken     string `json:"id_token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return nil, err
	}

	claims := &Claims{Provider: name}
	if tok.IDToken != "" {
		if c := parseIDTokenClaims(tok.IDToken); c != nil {
			claims.Subject = c.Sub
			claims.Email = c.Email
			claims.EmailVerified = c.EmailVerified
			claims.Name = firstNonEmpty(c.Name, c.PreferredUsername)
		}
	}
	// Fall back to userinfo for anything missing (e.g. providers that omit
	// claims from the id_token).
	if (claims.Subject == "" || claims.Email == "") && d.UserinfoEndpoint != "" && tok.AccessToken != "" {
		if ui := m.userinfo(ctx, d.UserinfoEndpoint, tok.AccessToken); ui != nil {
			if claims.Subject == "" {
				claims.Subject = ui.Sub
			}
			if claims.Email == "" {
				claims.Email = ui.Email
				claims.EmailVerified = ui.EmailVerified
			}
			if claims.Name == "" {
				claims.Name = firstNonEmpty(ui.Name, ui.PreferredUsername)
			}
		}
	}
	if claims.Subject == "" {
		return nil, errors.New("no subject in identity token")
	}
	return claims, nil
}

type idClaims struct {
	Sub               string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
}

func (m *Manager) userinfo(ctx context.Context, endpoint, accessToken string) *idClaims {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := m.http.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var c idClaims
	if err := json.NewDecoder(resp.Body).Decode(&c); err != nil {
		return nil
	}
	return &c
}

// parseIDTokenClaims decodes (without signature verification) the payload of a
// JWT id_token. The token is trusted because it arrived over the TLS-protected
// token endpoint in a PKCE-bound confidential-client exchange.
func parseIDTokenClaims(idToken string) *idClaims {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}
	var c idClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil
	}
	return &c
}

// chooseScopes intersects desired scopes with what the provider supports (when
// advertised), always keeping "openid".
func chooseScopes(desired, supported []string) []string {
	if len(supported) == 0 {
		return desired
	}
	sup := map[string]bool{}
	for _, s := range supported {
		sup[s] = true
	}
	out := []string{}
	for _, s := range desired {
		if s == "openid" || sup[s] {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		out = []string{"openid"}
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// --- state store ---

type stateData struct {
	Provider string
	Verifier string
	Nonce    string
	Created  time.Time
}

type stateStore struct {
	mu sync.Mutex
	m  map[string]stateData
}

func newStateStore() *stateStore { return &stateStore{m: map[string]stateData{}} }

func (s *stateStore) put(state string, d stateData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// prune expired (>10m)
	for k, v := range s.m {
		if time.Since(v.Created) > 10*time.Minute {
			delete(s.m, k)
		}
	}
	s.m[state] = d
}

func (s *stateStore) take(state string) (stateData, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.m[state]
	if !ok {
		return stateData{}, false
	}
	delete(s.m, state)
	if time.Since(d.Created) > 10*time.Minute {
		return stateData{}, false
	}
	return d, true
}

func randToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func s256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
