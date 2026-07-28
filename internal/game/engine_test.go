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

func TestStartGameAssignsWordAndImpostor(t *testing.T) {
	e := newTestEngine(t, 4)
	events, err := e.StartGame(PlayerID("a"))
	if err != nil {
		t.Fatalf("StartGame error: %v", err)
	}
	s := e.State()
	if s.Phase != PhaseDrawing {
		t.Fatalf("phase = %q, want drawing", s.Phase)
	}
	if s.Round != 1 {
		t.Fatalf("round = %d, want 1", s.Round)
	}
	if s.Word != "Giraffe" || s.Category != "Animal" {
		t.Fatalf("got %q/%q, want Animal/Giraffe", s.Category, s.Word)
	}
	if e.playerIndex(s.ImpostorID) < 0 {
		t.Fatalf("impostor %q is not a valid player", s.ImpostorID)
	}
	// Expect a RoundStarted and a TurnChanged event.
	var sawRound, sawTurn bool
	for _, ev := range events {
		switch ev.(type) {
		case RoundStarted:
			sawRound = true
		case TurnChanged:
			sawTurn = true
		}
	}
	if !sawRound || !sawTurn {
		t.Fatalf("events missing: round=%v turn=%v", sawRound, sawTurn)
	}
}

func TestStartGameRejectsNonHost(t *testing.T) {
	e := newTestEngine(t, 4)
	if _, err := e.StartGame(PlayerID("b")); err != ErrNotHost {
		t.Fatalf("err = %v, want ErrNotHost", err)
	}
}

func TestStartGameRejectsTooFewPlayers(t *testing.T) {
	e := newTestEngine(t, 3)
	if _, err := e.StartGame(PlayerID("a")); err != ErrTooFewPlayers {
		t.Fatalf("err = %v, want ErrTooFewPlayers", err)
	}
}
