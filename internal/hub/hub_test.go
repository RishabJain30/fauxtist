package hub

import "testing"

func TestCreateRoomReturnsUniqueCodes(t *testing.T) {
	h := New()
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		code := h.CreateRoom("Host")
		if code == "" {
			t.Fatal("empty code")
		}
		if seen[code] {
			t.Fatalf("duplicate code %q", code)
		}
		seen[code] = true
		if len(code) != CodeLen {
			t.Fatalf("code %q length = %d, want %d", code, len(code), CodeLen)
		}
	}
}

func TestGetRoomMissing(t *testing.T) {
	h := New()
	if _, ok := h.Get("ZZZZ"); ok {
		t.Fatal("expected missing room")
	}
}
