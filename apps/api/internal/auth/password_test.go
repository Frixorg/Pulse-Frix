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
