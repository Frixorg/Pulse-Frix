package auth

import "testing"

func TestHashAndVerifyPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("hash failed: %v", err)
	}
	if !VerifyPassword("correct horse battery", hash) {
		t.Error("correct password did not verify")
	}
	if VerifyPassword("wrong password", hash) {
		t.Error("wrong password verified")
	}
}

func TestHashPasswordRejectsShort(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Error("expected error for short password")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	if VerifyPassword("x", "not-a-valid-hash") {
		t.Error("malformed hash should not verify")
	}
}

func TestTokenHashAndEqual(t *testing.T) {
	plain, hash, err := GenerateToken("pst")
	if err != nil {
		t.Fatalf("token gen failed: %v", err)
	}
	if !EqualToken(plain, hash) {
		t.Error("token should equal its hash")
	}
	if EqualToken("wrong", hash) {
		t.Error("wrong token matched")
	}
}

func TestValidatePasswordPolicy(t *testing.T) {
	cases := []struct {
		name     string
		password string
		email    string
		wantErr  bool
	}{
		{"good", "correct horse battery staple", "ops@example.com", false},
		{"exactly at the minimum", "abcdefghijkl", "ops@example.com", false},
		{"one short of the minimum", "abcdefghijk", "ops@example.com", true},
		{"common", "password123", "ops@example.com", true},
		{"equals the email", "ops@example.com", "ops@example.com", true},
		{"equals the local part", "administrator", "administrator@example.com", true},
		{"no email to compare against", "a-long-enough-secret", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidatePasswordPolicy(tc.password, tc.email)
			if tc.wantErr && err == nil {
				t.Error("expected the policy to reject this password")
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected the policy to accept this password: %v", err)
			}
		})
	}
}

func TestValidatePasswordPolicyRejectsOverlyLong(t *testing.T) {
	long := ""
	for i := 0; i < 300; i++ {
		long += "a"
	}
	if err := ValidatePasswordPolicy(long, ""); err == nil {
		t.Error("expected an overly long password to be rejected")
	}
}

func TestValidateEmail(t *testing.T) {
	valid := []string{"ops@example.com", "a.b+c@sub.example.co.uk"}
	for _, e := range valid {
		if err := ValidateEmail(e); err != nil {
			t.Errorf("%q should be valid: %v", e, err)
		}
	}
	invalid := []string{"", "no-at-sign", "@example.com", "ops@", "ops@localhost", "a b@example.com", "a@b@example.com"}
	for _, e := range invalid {
		if err := ValidateEmail(e); err == nil {
			t.Errorf("%q should be rejected", e)
		}
	}
}

func TestNormalizeEmail(t *testing.T) {
	if got := NormalizeEmail("  OPS@Example.COM "); got != "ops@example.com" {
		t.Errorf("got %q", got)
	}
}
