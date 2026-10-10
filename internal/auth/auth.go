package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

var bearerRe = regexp.MustCompile(`(?i)Bearer\s+(.+)`)

// RandomHex returns nBytes of cryptographically secure randomness hex-encoded,
// matching PHP bin2hex(random_bytes($nBytes)).
func RandomHex(nBytes int) string {
	buf := make([]byte, nBytes)
	if _, err := rand.Read(buf); err != nil {
		// crypto/rand.Read only fails on catastrophic platform failure.
		panic(err)
	}
	return hex.EncodeToString(buf)
}

// NewUserID returns a 32-character hex id (16 random bytes), matching PHP.
func NewUserID() string { return RandomHex(16) }

// NewToken returns a 64-character hex token (32 random bytes), matching PHP.
func NewToken() string { return RandomHex(32) }

// Sha256Hex returns the lowercase hex sha256 of value, matching PHP hash('sha256', ...).
func Sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// VerifyPassword compares a plaintext password against a stored bcrypt hash.
// PHP's password_hash() emits $2y$ hashes; x/crypto/bcrypt historically rejected
// the $2y$ minor version, so normalize it to the equivalent $2a$ form.
func VerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	normalized := hash
	if strings.HasPrefix(normalized, "$2y$") {
		normalized = "$2a$" + normalized[len("$2y$"):]
	}
	return bcrypt.CompareHashAndPassword([]byte(normalized), []byte(password)) == nil
}

// HashPassword produces a bcrypt hash compatible with PHP's PASSWORD_DEFAULT.
func HashPassword(password string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(sum), nil
}

// BearerToken extracts the token from an Authorization: Bearer <token> header.
func BearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return ""
	}
	if matches := bearerRe.FindStringSubmatch(auth); matches != nil {
		return strings.TrimSpace(matches[1])
	}
	return ""
}
