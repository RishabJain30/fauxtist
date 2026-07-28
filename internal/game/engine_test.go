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

func startedEngine(t *testing.T, n int) *Engine {
	t.Helper()
	e := newTestEngine(t, n)
	if _, err := e.StartGame(PlayerID("a")); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	return e
}

func currentDrawer(e *Engine) PlayerID {
	return e.state.Players[e.state.TurnIndex].ID
}

func TestAddStrokeAdvancesTurn(t *testing.T) {
	e := startedEngine(t, 4)
	drawer := currentDrawer(e)
	_, err := e.AddStroke(drawer, Stroke{By: drawer, Points: []Point{{X: 0.1, Y: 0.1}}})
	if err != nil {
		t.Fatalf("AddStroke: %v", err)
	}
	if e.state.TurnIndex != 1 {
		t.Fatalf("turnIndex = %d, want 1", e.state.TurnIndex)
	}
	if len(e.State().Strokes) != 1 {
		t.Fatalf("strokes = %d, want 1", len(e.State().Strokes))
	}
}

func TestAddStrokeRejectsOutOfTurn(t *testing.T) {
	e := startedEngine(t, 4)
	notDrawer := e.state.Players[1].ID
	if _, err := e.AddStroke(notDrawer, Stroke{By: notDrawer}); err != ErrNotYourTurn {
		t.Fatalf("err = %v, want ErrNotYourTurn", err)
	}
}

func TestDrawingEndsAfterAllLaps(t *testing.T) {
	e := startedEngine(t, 4)
	// 4 players * 2 laps = 8 strokes total.
	for i := 0; i < 8; i++ {
		d := currentDrawer(e)
		if _, err := e.AddStroke(d, Stroke{By: d, Points: []Point{{X: 0.5, Y: 0.5}}}); err != nil {
			t.Fatalf("stroke %d: %v", i, err)
		}
	}
	if e.State().Phase != PhaseDiscussion {
		t.Fatalf("phase = %q, want discussion", e.State().Phase)
	}
	if len(e.State().Strokes) != 8 {
		t.Fatalf("strokes = %d, want 8", len(e.State().Strokes))
	}
}

func discussionEngine(t *testing.T, n int) *Engine {
	t.Helper()
	e := startedEngine(t, n)
	for i := 0; i < n*e.state.TotalLaps; i++ {
		d := currentDrawer(e)
		if _, err := e.AddStroke(d, Stroke{By: d}); err != nil {
			t.Fatalf("stroke %d: %v", i, err)
		}
	}
	return e
}

func TestEndDiscussionMovesToVoting(t *testing.T) {
	e := discussionEngine(t, 4)
	events, err := e.EndDiscussion(PlayerID("a"))
	if err != nil {
		t.Fatalf("EndDiscussion: %v", err)
	}
	if e.State().Phase != PhaseVoting {
		t.Fatalf("phase = %q, want voting", e.State().Phase)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
}

func TestEndDiscussionWrongPhaseRejected(t *testing.T) {
	e := startedEngine(t, 4) // still drawing
	if _, err := e.EndDiscussion(PlayerID("a")); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase", err)
	}
}

func votingEngine(t *testing.T, n int) *Engine {
	t.Helper()
	e := discussionEngine(t, n)
	if _, err := e.EndDiscussion(PlayerID("a")); err != nil {
		t.Fatalf("EndDiscussion: %v", err)
	}
	return e
}

func nonImpostors(e *Engine) []PlayerID {
	var out []PlayerID
	for _, p := range e.state.Players {
		if p.ID != e.state.ImpostorID {
			out = append(out, p.ID)
		}
	}
	return out
}

func TestVotingCaughtGoesToReveal(t *testing.T) {
	e := votingEngine(t, 4)
	imp := e.State().ImpostorID
	// Everyone (including impostor) votes for the impostor -> plurality -> caught.
	for _, p := range e.state.Players {
		if _, err := e.CastVote(p.ID, imp); err != nil {
			t.Fatalf("vote by %s: %v", p.ID, err)
		}
	}
	if e.State().Phase != PhaseReveal {
		t.Fatalf("phase = %q, want reveal", e.State().Phase)
	}
	if !e.State().LastResult.Caught {
		t.Fatalf("expected Caught=true")
	}
}

func TestVotingNotCaughtImpostorScores(t *testing.T) {
	e := votingEngine(t, 4)
	imp := e.State().ImpostorID
	others := nonImpostors(e)
	// Each non-impostor votes for a different non-impostor; impostor votes too.
	// Result: impostor receives zero votes -> not caught.
	_, _ = e.CastVote(others[0], others[1])
	_, _ = e.CastVote(others[1], others[2])
	_, _ = e.CastVote(others[2], others[0])
	_, err := e.CastVote(imp, others[0])
	if err != nil {
		t.Fatalf("impostor vote: %v", err)
	}
	s := e.State()
	if s.LastResult == nil {
		t.Fatalf("expected a round result")
	}
	if s.LastResult.Caught {
		t.Fatalf("expected Caught=false when impostor gets no votes")
	}
	var impScore int
	for _, p := range s.Players {
		if p.ID == imp {
			impScore = p.Score
		}
	}
	if impScore != 2 {
		t.Fatalf("impostor score = %d, want 2", impScore)
	}
}

func TestDoubleVoteRejected(t *testing.T) {
	e := votingEngine(t, 4)
	voter := e.state.Players[0].ID
	target := e.state.Players[1].ID
	if _, err := e.CastVote(voter, target); err != nil {
		t.Fatalf("first vote: %v", err)
	}
	if _, err := e.CastVote(voter, target); err != ErrAlreadyVoted {
		t.Fatalf("err = %v, want ErrAlreadyVoted", err)
	}
}
