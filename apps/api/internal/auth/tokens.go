package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
)

// GenerateToken creates a high-entropy token with the given prefix. The plain
// token is returned to the caller ONCE; only its hash should be stored.
func GenerateToken(prefix string) (plain, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	plain = prefix + "_" + base64.RawURLEncoding.EncodeToString(b)
	hash = HashToken(plain)
	return plain, hash, nil
}

// HashToken hashes a high-entropy token for storage. Because tokens are already
// high-entropy random values, a single SHA-256 is appropriate (unlike
// passwords, which use PBKDF2).
func HashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// EqualToken compares a presented token against a stored hash in constant time.
func EqualToken(plain, storedHash string) bool {
	got := HashToken(plain)
	return subtle.ConstantTimeCompare([]byte(got), []byte(storedHash)) == 1
}

// NewID returns a random identifier with a prefix (used for entity ids).
func NewID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b)
}
