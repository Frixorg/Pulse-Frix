package redact

import "testing"

func TestStringRedactsURLCredentials(t *testing.T) {
	in := "DATABASE_URL=postgres://user:password@host/db"
	got := String(in)
	if want := "DATABASE_URL=postgres://user:" + Placeholder + "@host/db"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestStringRedactsJWTandPEM(t *testing.T) {
	jwtIn := "auth eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxIn0.abcDEF123456"
	if got := String(jwtIn); got == jwtIn {
		t.Fatalf("JWT was not redacted: %q", got)
	}
	pem := "x -----BEGIN RSA PRIVATE KEY-----\nMIIB...\n-----END RSA PRIVATE KEY----- y"
	if got := String(pem); got == pem || !contains(got, Placeholder) {
		t.Fatalf("PEM was not redacted: %q", got)
	}
}

func TestKeyValueSecretKeys(t *testing.T) {
	cases := map[string]string{
		"DB_PASSWORD":   "hunter2",
		"API_KEY":       "abc123",
		"JWT_SECRET":    "s3cr3t",
		"CLIENT_SECRET": "xyz",
	}
	for k, v := range cases {
		if got := KeyValue(k, v); got != Placeholder {
			t.Errorf("KeyValue(%q,%q)=%q, want %q", k, v, got, Placeholder)
		}
	}
}

func TestKeyValueDoesNotOverRedact(t *testing.T) {
	if got := KeyValue("AUTHOR", "jane"); got != "jane" {
		t.Errorf("AUTHOR should not be redacted, got %q", got)
	}
	if got := KeyValue("PORT", "5432"); got != "5432" {
		t.Errorf("PORT should not be redacted, got %q", got)
	}
}

func TestEnvSlice(t *testing.T) {
	in := []string{"PATH=/usr/bin", "SECRET_TOKEN=abc", "DATABASE_URL=mysql://u:p@h/d"}
	out := EnvSlice(in)
	if out[0] != "PATH=/usr/bin" {
		t.Errorf("PATH altered: %q", out[0])
	}
	if out[1] != "SECRET_TOKEN="+Placeholder {
		t.Errorf("token not redacted: %q", out[1])
	}
	if want := "DATABASE_URL=mysql://u:" + Placeholder + "@h/d"; out[2] != want {
		t.Errorf("url pw not redacted: got %q want %q", out[2], want)
	}
}

func TestKnownTokenFormats(t *testing.T) {
	for _, tok := range []string{"ghp_0123456789abcdef0123456789abcdef0123", "AKIAABCDEFGHIJKLMNOP"} {
		if got := String("token " + tok); contains(got, tok) {
			t.Errorf("known token %q not redacted: %q", tok, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
