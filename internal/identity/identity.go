// Package identity provides identifiers, opaque tokens and password hashing.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/crypto/argon2"
)

// ID is a UUIDv4 string.
type ID = string

// NewID returns a random UUIDv4 string using crypto/rand.
func NewID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand must never fail on supported platforms; panic rather
		// than silently generating a predictable identifier.
		panic("identity: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewToken returns a URL-safe random token with n bytes of entropy.
func NewToken(n int) (string, error) {
	if n < 16 {
		return "", errors.New("identity: token entropy must be at least 16 bytes")
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("identity: generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the hex SHA-256 digest used for token lookups.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Argon2id parameters. These follow OWASP guidance for interactive login.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024 // KiB
	argonThreads = 2
	argonKeyLen  = 32
	argonSaltLen = 16
)

// HashPassword returns a PHC-formatted Argon2id hash.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("identity: password must be at least 8 characters")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("identity: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a PHC-formatted Argon2id hash in
// constant time. It returns false for malformed hashes.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false
	}
	var memory uint32
	var timeCost uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &timeCost, &threads); err != nil {
		return false
	}
	if memory == 0 || timeCost == 0 || threads == 0 || memory > 1024*1024 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, timeCost, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// PKCE generates a code verifier and challenge for OIDC authorization requests.
// The verifier must be stored server-side and passed to the callback endpoint for token exchange.
// Returns (verifier, challenge) where both are base64url-encoded strings.
func PKCE() (verifier string, challenge string) {
	verifierBytes := make([]byte, 32)
	if _, err := rand.Read(verifierBytes); err != nil {
		panic("identity: PKCE verifier generation failed: " + err.Error())
	}
	verifier = base64.RawURLEncoding.EncodeToString(verifierBytes)

	challengeBytes := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	return verifier, challenge
}

// OIDCAuthorizationURL builds the OIDC authorization URL with PKCE parameters.
// The caller must store the code_verifier and use it in the token exchange.
// The state parameter should be a cryptographically random value to prevent CSRF.
func OIDCAuthorizationURL(authURL, clientID, redirectURL, state, codeVerifier string) (*url.URL, error) {
	base, err := url.Parse(authURL)
	if err != nil {
		return nil, fmt.Errorf("identity: parse auth_url: %w", err)
	}

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURL)
	params.Set("scope", "openid profile email")
	params.Set("state", state)
	params.Set("code_challenge", codeVerifier)
	params.Set("code_challenge_method", "S256")

	base.RawQuery = params.Encode()
	return base, nil
}