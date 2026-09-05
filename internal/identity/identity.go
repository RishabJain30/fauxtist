// Package identity mints and verifies opaque per-player seat credentials.
//
// A player's public playerId and secret reconnectToken are both generated
// from crypto/rand, independent of any room code or display name, so
// neither is guessable from information a player necessarily already has
// (the room code) or shares (their name). Only the sha256 hash of a
// reconnect token is ever kept in memory; the raw token exists only in the
// response handed back to its owner.
package identity

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

const (
	playerIDBytes = 16 // 128 bits
	tokenBytes    = 32 // 256 bits
)

// TokenHash is the sha256 digest of a reconnect token, safe to hold in
// server memory in place of the raw token.
type TokenHash [sha256.Size]byte

// NewPlayerID returns a random, unguessable public player identifier.
func NewPlayerID() (string, error) {
	return randomToken(playerIDBytes)
}

// NewReconnectToken returns a random bearer secret proving seat ownership.
func NewReconnectToken() (string, error) {
	return randomToken(tokenBytes)
}

// Hash digests a raw reconnect token for storage.
func Hash(token string) TokenHash {
	return sha256.Sum256([]byte(token))
}

// Verify reports whether candidate hashes to want, in constant time.
func Verify(candidate string, want TokenHash) bool {
	got := Hash(candidate)
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
