package identity

import (
	"encoding/base64"
	"testing"
)

func TestNewPlayerIDIs128Bits(t *testing.T) {
	id, err := NewPlayerID()
	if err != nil {
		t.Fatalf("NewPlayerID: %v", err)
	}
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		t.Fatalf("playerId is not URL-safe unpadded base64: %v", err)
	}
	if len(b) != 16 {
		t.Fatalf("decoded playerId = %d bytes, want 16 (128 bits)", len(b))
	}
}

func TestNewReconnectTokenIs256Bits(t *testing.T) {
	tok, err := NewReconnectToken()
	if err != nil {
		t.Fatalf("NewReconnectToken: %v", err)
	}
	b, err := base64.RawURLEncoding.DecodeString(tok)
	if err != nil {
		t.Fatalf("token is not URL-safe unpadded base64: %v", err)
	}
	if len(b) != 32 {
		t.Fatalf("decoded token = %d bytes, want 32 (256 bits)", len(b))
	}
}

func TestGeneratedValuesAreDistinct(t *testing.T) {
	seenIDs := map[string]bool{}
	seenTokens := map[string]bool{}
	for i := 0; i < 200; i++ {
		id, err := NewPlayerID()
		if err != nil {
			t.Fatalf("NewPlayerID: %v", err)
		}
		if seenIDs[id] {
			t.Fatalf("duplicate playerId generated: %q", id)
		}
		seenIDs[id] = true

		tok, err := NewReconnectToken()
		if err != nil {
			t.Fatalf("NewReconnectToken: %v", err)
		}
		if seenTokens[tok] {
			t.Fatalf("duplicate reconnect token generated: %q", tok)
		}
		seenTokens[tok] = true
	}
}

func TestHashDoesNotEqualRawToken(t *testing.T) {
	tok, _ := NewReconnectToken()
	h := Hash(tok)
	if string(h[:]) == tok {
		t.Fatal("hash must not equal the raw token")
	}
}

func TestHashIsDeterministic(t *testing.T) {
	tok := "fixed-example-token"
	if Hash(tok) != Hash(tok) {
		t.Fatal("Hash must be deterministic for the same input")
	}
}

func TestVerifyAcceptsCorrectToken(t *testing.T) {
	tok, _ := NewReconnectToken()
	if !Verify(tok, Hash(tok)) {
		t.Fatal("Verify rejected the correct token")
	}
}

func TestVerifyRejectsWrongToken(t *testing.T) {
	tok, _ := NewReconnectToken()
	other, _ := NewReconnectToken()
	if Verify(other, Hash(tok)) {
		t.Fatal("Verify accepted an unrelated token")
	}
	if Verify("", Hash(tok)) {
		t.Fatal("Verify accepted an empty candidate")
	}
}
