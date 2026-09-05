package hub

import "testing"

// --- Requirement #7: secure randomness, and bounded, safe collision handling ---

func TestGenerateUniqueCodeAvoidsTakenCodes(t *testing.T) {
	taken := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := generateUniqueCode(taken, codeAlphabet, CodeLen, maxCodeAttempts)
		if err != nil {
			t.Fatalf("generateUniqueCode: %v", err)
		}
		if taken[code] {
			t.Fatalf("generateUniqueCode returned an already-taken code %q", code)
		}
		if len(code) != CodeLen {
			t.Fatalf("code %q length = %d, want %d", code, len(code), CodeLen)
		}
		taken[code] = true
	}
}

func TestGenerateUniqueCodeReturnsErrorWhenSpaceExhausted(t *testing.T) {
	// A 1-character alphabet over a 1-character code has exactly one
	// possible value; pre-taking it must exhaust every attempt.
	taken := map[string]bool{"A": true}
	if _, err := generateUniqueCode(taken, "A", 1, 5); err != ErrCodeSpaceExhausted {
		t.Fatalf("err = %v, want ErrCodeSpaceExhausted", err)
	}
}

func TestGenerateUniqueCodeSucceedsAgainstAnAlmostFullSpace(t *testing.T) {
	// Alphabet "AB", length 1: only "A" and "B" are possible. Pre-taking
	// "A" must still deterministically find "B" within the attempt bound.
	taken := map[string]bool{"A": true}
	code, err := generateUniqueCode(taken, "AB", 1, maxCodeAttempts)
	if err != nil {
		t.Fatalf("generateUniqueCode: %v", err)
	}
	if code != "B" {
		t.Fatalf("code = %q, want %q", code, "B")
	}
}

func TestSecureIndexStaysInRangeAndCoversTheSpace(t *testing.T) {
	seen := map[int]bool{}
	for i := 0; i < 500; i++ {
		idx, err := secureIndex(7)
		if err != nil {
			t.Fatalf("secureIndex: %v", err)
		}
		if idx < 0 || idx >= 7 {
			t.Fatalf("secureIndex(7) = %d, out of range", idx)
		}
		seen[idx] = true
	}
	if len(seen) != 7 {
		t.Fatalf("saw %d distinct values out of 7 possible across 500 draws", len(seen))
	}
}
