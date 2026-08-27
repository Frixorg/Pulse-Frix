// Package auth handles password hashing, tokens, sessions and the request
// principal. It uses only the Go standard library: passwords are hashed with
// PBKDF2-HMAC-SHA256 (implemented here from stdlib primitives) with a high
// iteration count and a per-password random salt. Secrets are never logged.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// pbkdf2Iterations follows OWASP guidance for PBKDF2-HMAC-SHA256.
const pbkdf2Iterations = 210000
const saltLen = 16
const keyLen = 32

// HashPassword returns an encoded PHC-like string:
//
//	pbkdf2-sha256$<iter>$<b64salt>$<b64hash>
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("password too short")
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk := pbkdf2SHA256([]byte(password), salt, pbkdf2Iterations, keyLen)
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s",
		pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(dk),
	), nil
}

// VerifyPassword checks a password against an encoded hash in constant time.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iter, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// pbkdf2SHA256 is a standard-library PBKDF2-HMAC-SHA256 implementation.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	prf := hmac.New(sha256.New, password)
	hLen := prf.Size()
	numBlocks := (keyLen + hLen - 1) / hLen
	var dk []byte
	buf := make([]byte, 4)
	for block := 1; block <= numBlocks; block++ {
		prf.Reset()
		prf.Write(salt)
		binary.BigEndian.PutUint32(buf, uint32(block))
		prf.Write(buf)
		T := prf.Sum(nil)
		U := make([]byte, len(T))
		copy(U, T)
		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(U)
			U = prf.Sum(U[:0])
			for x := range T {
				T[x] ^= U[x]
			}
		}
		dk = append(dk, T...)
	}
	return dk[:keyLen]
}

// MinPasswordLength is the floor enforced on every password Pulse accepts from
// a human (first-boot provisioning, password changes). It is deliberately
// higher than HashPassword's hard minimum, which only guards the primitive.
const MinPasswordLength = 12

// maxPasswordLength bounds the input so a huge body can't be turned into work.
const maxPasswordLength = 256

// weakPasswords are rejected outright regardless of length. This is a tripwire
// for the handful of values that actually show up on fresh installs, not a
// substitute for a real breached-password list.
var weakPasswords = map[string]bool{
	"password":      true,
	"password123":   true,
	"passw0rd123":   true,
	"changeme":      true,
	"changeme123":   true,
	"administrator": true,
	"adminadmin":    true,
	"pulsepulse":    true,
	"letmein12345":  true,
	"123456789012":  true,
	"qwertyuiop12":  true,
}

// ValidatePasswordPolicy checks a human-chosen password before it is hashed.
// The error text is safe to return to the caller: it never echoes the password.
// email may be empty.
func ValidatePasswordPolicy(password, email string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	}
	if len(password) > maxPasswordLength {
		return fmt.Errorf("password must be at most %d characters", maxPasswordLength)
	}
	lower := strings.ToLower(password)
	if weakPasswords[lower] {
		return errors.New("password is too common")
	}
	if email != "" {
		e := strings.ToLower(strings.TrimSpace(email))
		if lower == e {
			return errors.New("password must not be your email address")
		}
		if local, _, ok := strings.Cut(e, "@"); ok && local != "" && lower == local {
			return errors.New("password must not be your email address")
		}
	}
	return nil
}

// ValidateEmail applies a deliberately small sanity check: one "@", something
// on both sides, a dot in the domain, no whitespace, bounded length. Pulse
// never sends mail, so anything stricter would only reject valid operators.
func ValidateEmail(email string) error {
	e := strings.TrimSpace(email)
	if e == "" {
		return errors.New("email is required")
	}
	if len(e) > 254 {
		return errors.New("email is too long")
	}
	if strings.ContainsAny(e, " \t\r\n") {
		return errors.New("email must not contain whitespace")
	}
	local, domain, ok := strings.Cut(e, "@")
	if !ok || local == "" || domain == "" || strings.Contains(domain, "@") {
		return errors.New("email is not valid")
	}
	if !strings.Contains(domain, ".") || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return errors.New("email is not valid")
	}
	return nil
}

// NormalizeEmail lowercases and trims an address for storage and lookup.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
