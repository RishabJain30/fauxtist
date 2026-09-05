package hub

import (
	"crypto/rand"
	"errors"
)

// codeAlphabet excludes visually ambiguous characters (0/O, 1/I, etc.) so a
// code read aloud or typed by hand is less error-prone.
const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// CodeLen is the length of a room join code.
const CodeLen = 4

// maxCodeAttempts bounds how many times generateUniqueCode will draw a
// fresh code before giving up. With CodeLen=4 and a 33-character alphabet
// (~1.19M possible codes), a collision against any realistic number of
// concurrently live rooms is already vanishingly unlikely on the first
// attempt; this only exists so a pathological caller (or a test) gets a
// clean error instead of an infinite loop if the space is ever
// exhausted.
const maxCodeAttempts = 20

// ErrCodeSpaceExhausted is returned when no unique code could be minted
// within maxCodeAttempts.
var ErrCodeSpaceExhausted = errors.New("could not generate a unique room code")

// generateUniqueCode draws a cryptographically random code from alphabet,
// retrying up to maxAttempts times against the taken set (guessability of
// a room code is a real security property — an outsider must not be able
// to predict or brute-force their way into someone else's game — so this
// uses crypto/rand rather than math/rand, which is used elsewhere in this
// package only for game-internal, non-security-sensitive randomness).
// Pure and dependency-free so it's testable without a Hub.
func generateUniqueCode(taken map[string]bool, alphabet string, length, maxAttempts int) (string, error) {
	for attempt := 0; attempt < maxAttempts; attempt++ {
		code, err := randomCode(alphabet, length)
		if err != nil {
			return "", err
		}
		if !taken[code] {
			return code, nil
		}
	}
	return "", ErrCodeSpaceExhausted
}

func randomCode(alphabet string, length int) (string, error) {
	b := make([]byte, length)
	for i := range b {
		idx, err := secureIndex(len(alphabet))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[idx]
	}
	return string(b), nil
}

// secureIndex returns a uniformly distributed index in [0, n) using
// rejection sampling against crypto/rand, so the result carries no modulo
// bias (n need not divide 256 evenly).
func secureIndex(n int) (int, error) {
	limit := 256 - (256 % n)
	for {
		var b [1]byte
		if _, err := rand.Read(b[:]); err != nil {
			return 0, err
		}
		if int(b[0]) < limit {
			return int(b[0]) % n, nil
		}
	}
}
