package game

import (
	"math/rand"
	"testing"
)

// fakeWords returns a fixed sequence so tests are deterministic regardless of rng.
type fakeWords struct {
	pairs [][2]string // {category, word}
	i     int
}

func (f *fakeWords) Pick(_ map[string]bool) (string, string, bool) {
	if f.i >= len(f.pairs) {
		return "", "", false
	}
	p := f.pairs[f.i]
	f.i++
	return p[0], p[1], true
}

func testPlayers(n int) []Player {
	ps := make([]Player, n)
	for i := 0; i < n; i++ {
		ps[i] = Player{ID: PlayerID(string(rune('a' + i))), Name: string(rune('A' + i))}
	}
	return ps
}

func newTestEngine(t *testing.T, n int) *Engine {
	t.Helper()
	words := &fakeWords{pairs: [][2]string{
		{"Animal", "Giraffe"}, {"Food", "Pizza"}, {"Animal", "Otter"},
		{"Food", "Taco"}, {"Object", "Umbrella"}, {"Object", "Anchor"},
	}}
	return NewEngine(testPlayers(n), PlayerID("a"), n, rand.New(rand.NewSource(1)), words)
}

func TestNewEngineStartsInLobby(t *testing.T) {
	e := newTestEngine(t, 4)
	s := e.State()
	if s.Phase != PhaseLobby {
		t.Fatalf("phase = %q, want %q", s.Phase, PhaseLobby)
	}
	if len(s.Players) != 4 {
		t.Fatalf("players = %d, want 4", len(s.Players))
	}
	if s.HostID != PlayerID("a") {
		t.Fatalf("host = %q, want a", s.HostID)
	}
	if s.TotalRounds != 4 {
		t.Fatalf("totalRounds = %d, want 4", s.TotalRounds)
	}
}
