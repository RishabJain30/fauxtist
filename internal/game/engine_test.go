package game

import (
	"math/rand"
	"strings"
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
		if _, err := e.CastVote(p.ID, imp, nil); err != nil {
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
	_, _ = e.CastVote(others[0], others[1], nil)
	_, _ = e.CastVote(others[1], others[2], nil)
	_, _ = e.CastVote(others[2], others[0], nil)
	_, err := e.CastVote(imp, others[0], nil)
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
	if _, err := e.CastVote(voter, target, nil); err != nil {
		t.Fatalf("first vote: %v", err)
	}
	if _, err := e.CastVote(voter, target, nil); err != ErrAlreadyVoted {
		t.Fatalf("err = %v, want ErrAlreadyVoted", err)
	}
}

func caughtEngine(t *testing.T, n int) *Engine {
	t.Helper()
	e := votingEngine(t, n)
	imp := e.State().ImpostorID
	for _, p := range e.state.Players {
		if _, err := e.CastVote(p.ID, imp, nil); err != nil {
			t.Fatalf("vote: %v", err)
		}
	}
	return e
}

func TestImpostorGuessRightStealsWin(t *testing.T) {
	e := caughtEngine(t, 4)
	imp := e.State().ImpostorID
	word := e.State().LastResult.Word
	if _, err := e.ImpostorGuess(imp, strings.ToUpper(word)); err != nil {
		t.Fatalf("guess: %v", err)
	}
	s := e.State()
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

func TestImpostorGuessWrongOthersScore(t *testing.T) {
	e := caughtEngine(t, 4)
	imp := e.State().ImpostorID
	if _, err := e.ImpostorGuess(imp, "definitely-not-the-word"); err != nil {
		t.Fatalf("guess: %v", err)
	}
	s := e.State()
	for _, p := range s.Players {
		if p.ID == imp {
			if p.Score != 0 {
				t.Fatalf("impostor score = %d, want 0", p.Score)
			}
		} else {
			if p.Score != 1 {
				t.Fatalf("non-impostor %s score = %d, want 1", p.ID, p.Score)
			}
		}
	}
}

func TestOnlyImpostorMayGuess(t *testing.T) {
	e := caughtEngine(t, 4)
	nonImp := nonImpostors(e)[0]
	if _, err := e.ImpostorGuess(nonImp, "x"); err != ErrNotImpostor {
		t.Fatalf("err = %v, want ErrNotImpostor", err)
	}
}

func TestUpsertPlayerAddsDuringLobby(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.UpsertPlayer(Player{ID: "z", Name: "Zoe"}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	if len(e.State().Players) != 5 {
		t.Fatalf("players = %d, want 5", len(e.State().Players))
	}
}

func TestUpsertPlayerRenamesExisting(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.UpsertPlayer(Player{ID: "a", Name: "Alice2"}); err != nil {
		t.Fatalf("UpsertPlayer: %v", err)
	}
	if len(e.State().Players) != 4 {
		t.Fatalf("players = %d, want 4 (rename, not add)", len(e.State().Players))
	}
	if e.State().Players[0].Name != "Alice2" {
		t.Fatalf("name = %q, want Alice2", e.State().Players[0].Name)
	}
}

func TestUpsertPlayerRejectsNewAfterStart(t *testing.T) {
	e := startedEngine(t, 4)
	if err := e.UpsertPlayer(Player{ID: "z", Name: "Zoe"}); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase", err)
	}
}

func TestUpsertPlayerRejectsWhenFull(t *testing.T) {
	e := newTestEngine(t, MaxPlayers)
	if err := e.UpsertPlayer(Player{ID: "over", Name: "Over"}); err != ErrRoomFull {
		t.Fatalf("err = %v, want ErrRoomFull", err)
	}
}

func TestStartGameScalesRoundsToPlayers(t *testing.T) {
	e := newTestEngine(t, 4)
	_ = e.UpsertPlayer(Player{ID: "e", Name: "Eve"}) // now 5 players
	if _, err := e.StartGame(PlayerID("a")); err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	if e.State().TotalRounds != 5 {
		t.Fatalf("totalRounds = %d, want 5", e.State().TotalRounds)
	}
}

func playToGameOver(t *testing.T, n int) *Engine {
	t.Helper()
	e := newTestEngine(t, n)
	if _, err := e.StartGame(PlayerID("a")); err != nil {
		t.Fatalf("start: %v", err)
	}
	for r := 0; r < n; r++ {
		for i := 0; i < n*e.state.TotalLaps; i++ {
			d := currentDrawer(e)
			_, _ = e.AddStroke(d, Stroke{By: d})
		}
		_, _ = e.EndDiscussion(PlayerID("a"))
		imp := e.State().ImpostorID
		others := nonImpostors(e)
		_, _ = e.CastVote(others[0], others[1], nil)
		_, _ = e.CastVote(others[1], others[2], nil)
		_, _ = e.CastVote(others[2], others[0], nil)
		_, _ = e.CastVote(imp, others[0], nil)
		e.AdvanceRound()
	}
	return e
}

func TestRestartStartsFreshGame(t *testing.T) {
	e := playToGameOver(t, 4)
	if e.State().Phase != PhaseGameOver {
		t.Fatalf("precondition: phase = %q, want game_over", e.State().Phase)
	}
	if _, err := e.Restart(PlayerID("a")); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	s := e.State()
	if s.Phase != PhaseDrawing {
		t.Fatalf("phase = %q, want drawing", s.Phase)
	}
	if s.Round != 1 {
		t.Fatalf("round = %d, want 1", s.Round)
	}
	for _, p := range s.Players {
		if p.Score != 0 {
			t.Fatalf("score not reset: %s = %d", p.ID, p.Score)
		}
	}
}

func TestRestartRejectsNonHostAndWrongPhase(t *testing.T) {
	e := playToGameOver(t, 4)
	if _, err := e.Restart(PlayerID("b")); err != ErrNotHost {
		t.Fatalf("err = %v, want ErrNotHost", err)
	}
	e2 := startedEngine(t, 4) // still drawing
	if _, err := e2.Restart(PlayerID("a")); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase", err)
	}
}

func TestNotCaughtHoldsOnRevealUntilAdvance(t *testing.T) {
	e := votingEngine(t, 4)
	imp := e.State().ImpostorID
	others := nonImpostors(e)
	_, _ = e.CastVote(others[0], others[1], nil)
	_, _ = e.CastVote(others[1], others[2], nil)
	_, _ = e.CastVote(others[2], others[0], nil)
	_, _ = e.CastVote(imp, others[0], nil) // not caught

	if e.State().Phase != PhaseReveal {
		t.Fatalf("phase = %q, want reveal (should hold, not auto-advance)", e.State().Phase)
	}
	e.AdvanceRound()
	if e.State().Phase != PhaseDrawing {
		t.Fatalf("after AdvanceRound phase = %q, want drawing", e.State().Phase)
	}
	if e.State().Round != 2 {
		t.Fatalf("round = %d, want 2", e.State().Round)
	}
}

func TestGameEndsAfterFinalRound(t *testing.T) {
	// totalRounds defaults to len(players); play all rounds and expect game over.
	e := newTestEngine(t, 4)
	if _, err := e.StartGame(PlayerID("a")); err != nil {
		t.Fatalf("start: %v", err)
	}
	for r := 0; r < 4; r++ {
		// Drive one full round: draw all strokes, discuss, vote (not caught path).
		for i := 0; i < 4*e.state.TotalLaps; i++ {
			d := currentDrawer(e)
			_, _ = e.AddStroke(d, Stroke{By: d})
		}
		_, _ = e.EndDiscussion(PlayerID("a"))
		imp := e.State().ImpostorID
		others := nonImpostors(e)
		// Split votes so the impostor receives none -> not caught.
		_, _ = e.CastVote(others[0], others[1], nil)
		_, _ = e.CastVote(others[1], others[2], nil)
		_, _ = e.CastVote(others[2], others[0], nil)
		_, _ = e.CastVote(imp, others[0], nil)
		// Round holds on reveal; advance to the next round (or game over).
		e.AdvanceRound()
	}
	if e.State().Phase != PhaseGameOver {
		t.Fatalf("phase = %q, want game_over", e.State().Phase)
	}
}

func TestRemovePlayerOnlyAllowedInLobby(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.RemovePlayer(PlayerID("b")); err != nil {
		t.Fatalf("RemovePlayer in lobby: %v", err)
	}
	if len(e.State().Players) != 3 {
		t.Fatalf("players = %d, want 3", len(e.State().Players))
	}

	e2 := startedEngine(t, 4) // now drawing
	if err := e2.RemovePlayer(PlayerID("b")); err != ErrNotInLobby {
		t.Fatalf("err = %v, want ErrNotInLobby", err)
	}
	if len(e2.State().Players) != 4 {
		t.Fatalf("players = %d, want 4 (unchanged)", len(e2.State().Players))
	}
}

func TestRemovePlayerRejectsUnknown(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.RemovePlayer(PlayerID("nope")); err != ErrUnknownPlayer {
		t.Fatalf("err = %v, want ErrUnknownPlayer", err)
	}
}

func TestSetHostIDTransitionsOwnership(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.SetHostID(PlayerID("b")); err != nil {
		t.Fatalf("SetHostID: %v", err)
	}
	if e.State().HostID != PlayerID("b") {
		t.Fatalf("hostID = %q, want b", e.State().HostID)
	}
}

func TestSetHostIDRejectsUnknown(t *testing.T) {
	e := newTestEngine(t, 4)
	if err := e.SetHostID(PlayerID("nope")); err != ErrUnknownPlayer {
		t.Fatalf("err = %v, want ErrUnknownPlayer", err)
	}
	if e.State().HostID != PlayerID("a") {
		t.Fatalf("hostID changed to %q, want unchanged a", e.State().HostID)
	}
}

func TestSkipTurnAdvancesWithoutStroke(t *testing.T) {
	e := startedEngine(t, 4)
	before := len(e.State().Strokes)
	events, err := e.SkipTurn()
	if err != nil {
		t.Fatalf("SkipTurn: %v", err)
	}
	if len(e.State().Strokes) != before {
		t.Fatalf("strokes = %d, want unchanged %d (skip must not draw)", len(e.State().Strokes), before)
	}
	var sawTurnChanged bool
	for _, ev := range events {
		if _, ok := ev.(TurnChanged); ok {
			sawTurnChanged = true
		}
	}
	if !sawTurnChanged {
		t.Fatal("expected a TurnChanged event, same as a normal stroke would produce")
	}
}

func TestSkipTurnRejectsWrongPhase(t *testing.T) {
	e := newTestEngine(t, 4) // still lobby
	if _, err := e.SkipTurn(); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase", err)
	}
}

func TestCastVoteIgnoresDisconnectedPlayers(t *testing.T) {
	e := votingEngine(t, 4)
	imp := e.State().ImpostorID
	others := nonImpostors(e)
	// Only 2 of 4 are connected; both are non-impostor "others". Once both
	// have voted, voting must resolve without waiting for the other two.
	connected := map[PlayerID]bool{others[0]: true, others[1]: true}
	if _, err := e.CastVote(others[0], others[1], connected); err != nil {
		t.Fatalf("vote 1: %v", err)
	}
	if e.State().Phase != PhaseVoting {
		t.Fatalf("phase = %q, want voting (only 1/2 connected voters in)", e.State().Phase)
	}
	events, err := e.CastVote(others[1], others[0], connected)
	if err != nil {
		t.Fatalf("vote 2: %v", err)
	}
	if e.State().Phase != PhaseReveal {
		t.Fatalf("phase = %q, want reveal once every connected voter has voted", e.State().Phase)
	}
	_ = imp
	var sawResolved bool
	for _, ev := range events {
		if _, ok := ev.(PhaseChanged); ok {
			sawResolved = true
		}
	}
	if !sawResolved {
		t.Fatal("expected voting to resolve and emit a PhaseChanged event")
	}
}

func TestCheckVotingResolutionRecomputesOnPresenceChange(t *testing.T) {
	e := votingEngine(t, 4)
	others := nonImpostors(e)
	imp := e.State().ImpostorID
	connectedAll := map[PlayerID]bool{others[0]: true, others[1]: true, others[2]: true, imp: true}
	if _, err := e.CastVote(others[0], others[1], connectedAll); err != nil {
		t.Fatalf("vote: %v", err)
	}
	// 1/4 voted; not enough while everyone is connected.
	if ev := e.CheckVotingResolution(connectedAll); ev != nil {
		t.Fatalf("expected no resolution yet, got %v", ev)
	}
	// The three unvoted players disconnect; only the one who already voted
	// remains connected — resolution must now trigger without a new vote.
	connectedOne := map[PlayerID]bool{others[0]: true}
	events := e.CheckVotingResolution(connectedOne)
	if e.State().Phase != PhaseReveal {
		t.Fatalf("phase = %q, want reveal", e.State().Phase)
	}
	if len(events) == 0 {
		t.Fatal("expected resolution events")
	}
}

func TestVotingWaitsWhenNoOneConnected(t *testing.T) {
	e := votingEngine(t, 4)
	others := nonImpostors(e)
	imp := e.State().ImpostorID
	connectedAll := map[PlayerID]bool{others[0]: true, others[1]: true, others[2]: true, imp: true}
	if _, err := e.CastVote(others[0], others[1], connectedAll); err != nil {
		t.Fatalf("vote: %v", err)
	}
	// Only 1/4 voted, so this alone would not resolve — but the check must
	// also never resolve (or panic/loop) against a zero-connected set.
	if ev := e.CheckVotingResolution(map[PlayerID]bool{}); ev != nil {
		t.Fatalf("expected no resolution with zero connected voters, got %v", ev)
	}
	if e.State().Phase != PhaseVoting {
		t.Fatalf("phase = %q, want voting (still waiting)", e.State().Phase)
	}
}

func TestResolveImpostorTimeoutScoresLikeWrongGuess(t *testing.T) {
	e := caughtEngine(t, 4)
	imp := e.State().ImpostorID
	events, err := e.ResolveImpostorTimeout()
	if err != nil {
		t.Fatalf("ResolveImpostorTimeout: %v", err)
	}
	s := e.State()
	if !s.LastResult.ImpostorTimedOut {
		t.Fatal("expected ImpostorTimedOut = true")
	}
	if s.LastResult.ImpostorGuessedRight {
		t.Fatal("expected ImpostorGuessedRight = false")
	}
	for _, p := range s.Players {
		want := 1
		if p.ID == imp {
			want = 0
		}
		if p.Score != want {
			t.Fatalf("player %s score = %d, want %d", p.ID, p.Score, want)
		}
	}
	var sawRoundEnded bool
	for _, ev := range events {
		if _, ok := ev.(RoundEnded); ok {
			sawRoundEnded = true
		}
	}
	if !sawRoundEnded {
		t.Fatal("expected a RoundEnded event, same as a resolved guess would produce")
	}
}

func TestGuessThenTimeoutOnlyResolvesOnce(t *testing.T) {
	e := caughtEngine(t, 4)
	imp := e.State().ImpostorID
	word := e.State().LastResult.Word
	if _, err := e.ImpostorGuess(imp, strings.ToUpper(word)); err != nil {
		t.Fatalf("guess: %v", err)
	}
	// A timeout racing in after a real guess must not double-resolve.
	if _, err := e.ResolveImpostorTimeout(); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase (already resolved by a real guess)", err)
	}
	imScore := 0
	for _, p := range e.State().Players {
		if p.ID == imp {
			imScore = p.Score
		}
	}
	if imScore != 2 {
		t.Fatalf("impostor score = %d, want 2 (unchanged by the stale timeout)", imScore)
	}
}

func TestTimeoutThenLateGuessOnlyResolvesOnce(t *testing.T) {
	e := caughtEngine(t, 4)
	imp := e.State().ImpostorID
	if _, err := e.ResolveImpostorTimeout(); err != nil {
		t.Fatalf("ResolveImpostorTimeout: %v", err)
	}
	// A real guess arriving late (e.g. network delay) after the timeout
	// already resolved must not double-resolve or overwrite the result.
	if _, err := e.ImpostorGuess(imp, "anything"); err != ErrWrongPhase {
		t.Fatalf("err = %v, want ErrWrongPhase (already resolved by timeout)", err)
	}
	for _, p := range e.State().Players {
		want := 1
		if p.ID == imp {
			want = 0
		}
		if p.Score != want {
			t.Fatalf("player %s score = %d, want %d (unchanged by the late guess)", p.ID, p.Score, want)
		}
	}
}
