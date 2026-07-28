# Fauxtist Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the complete Go backend for Fauxtist — a real-time "secret impostor" drawing party game — with a pure, unit-tested game engine and an actor-per-room WebSocket server.

**Architecture:** A pure `game.Engine` holds all rules and is tested in isolation (no I/O). Each room is a goroutine (`room.Room`) that owns one engine instance and is the only thing that mutates it, receiving player actions over a channel and broadcasting resulting events to WebSocket clients. A `hub.Hub` creates rooms, generates codes, routes connections, and sweeps idle rooms. Secret information (the word) is filtered per-connection at the server boundary, never inside the engine.

**Tech Stack:** Go 1.22+, `nhooyr.io/websocket` (small, context-aware, idiomatic WS library), standard library `net/http`, `net/http/httptest` for integration tests.

**Scope note:** This plan is the backend only. The React frontend and deployment (Docker + Render) are a separate follow-up plan. This backend is fully testable on its own via `go test ./...` and an integration test driving real WebSocket clients.

---

## File Structure

```
fauxtist/
  go.mod                          # module github.com/RishabJain30/fauxtist
  .gitignore
  internal/
    game/
      state.go                    # Phase, PlayerID, Player, Point, Stroke, State, RoundResult
      errors.go                   # sentinel errors
      events.go                   # Event types the engine emits
      engine.go                   # Engine struct + all action methods (the core)
    wordbank/
      wordbank.go                 # WordBank implementing game.WordSource
    wsproto/
      message.go                  # Envelope + typed payloads + type constants
    room/
      room.go                     # Room actor goroutine (owns one Engine)
      client.go                   # Client = one WS connection (read/write pumps)
    hub/
      hub.go                      # Hub: create/route/sweep rooms, code generation
    server/
      server.go                   # HTTP mux, /api/rooms, /ws/room, reconnect tokens
  cmd/
    fauxtist/
      main.go                     # entrypoint: build hub + server, ListenAndServe
```

**Responsibility boundaries:**
- `game` knows nothing about networking, JSON, or WebSockets. Pure logic, deterministic given its injected `rng` and `WordSource`.
- `wsproto` is the wire contract — shared vocabulary between server and (future) frontend.
- `room` translates between wire messages and engine calls, and owns concurrency (one goroutine per room).
- `hub` owns the room lifecycle.
- `server` owns HTTP, connection upgrade, and the per-connection secret filtering.

---

## Task 0: Project scaffold

**Files:**
- Create: `go.mod`, `.gitignore`

- [ ] **Step 1: Install Go (if missing)**

Run: `go version`
If it prints "command not found", run: `brew install go` then re-run `go version`.
Expected: `go version go1.22` or newer.

- [ ] **Step 2: Initialize the module**

Run from repo root (`~/Practice Project/fauxtist`):
```bash
go mod init github.com/RishabJain30/fauxtist
go get nhooyr.io/websocket@latest
```
Expected: `go.mod` and `go.sum` created; `go.mod` names the module and lists `nhooyr.io/websocket`.

- [ ] **Step 3: Add `.gitignore`**

Create `.gitignore`:
```gitignore
# Go
/bin/
*.test
*.out
coverage.*

# Frontend (added in the frontend plan)
/web/node_modules/
/web/dist/

# Editor / OS
.DS_Store
.idea/
.vscode/*
!.vscode/extensions.json
```

- [ ] **Step 4: Verify the toolchain builds an empty module**

Run: `go build ./...`
Expected: no output, exit code 0 (nothing to build yet, but confirms module is valid).

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum .gitignore
git commit -m "chore: initialize Go module and gitignore"
```

---

## Task 1: Domain types and engine constructor

**Files:**
- Create: `internal/game/state.go`
- Create: `internal/game/errors.go`
- Create: `internal/game/engine.go`
- Test: `internal/game/engine_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/game/engine_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run TestNewEngine -v`
Expected: FAIL — `undefined: Engine`, `undefined: Player`, etc.

- [ ] **Step 3: Write the types**

Create `internal/game/state.go`:
```go
package game

// Phase is the current stage of a round.
type Phase string

const (
	PhaseLobby      Phase = "lobby"
	PhaseDrawing    Phase = "drawing"
	PhaseDiscussion Phase = "discussion"
	PhaseVoting     Phase = "voting"
	PhaseReveal     Phase = "reveal"
	PhaseGameOver   Phase = "game_over"
)

// PlayerID is a stable per-game identifier for a player.
type PlayerID string

// Point is a normalized canvas coordinate in [0,1].
type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Stroke is one continuous pen movement contributed on a player's turn.
type Stroke struct {
	By     PlayerID `json:"by"`
	Points []Point  `json:"points"`
	Color  string   `json:"color"`
	Width  float64  `json:"width"`
}

// Player is a participant and their running score.
type Player struct {
	ID    PlayerID `json:"id"`
	Name  string   `json:"name"`
	Score int      `json:"score"`
}

// RoundResult summarizes a completed round.
type RoundResult struct {
	ImpostorID           PlayerID          `json:"impostorId"`
	Word                 string            `json:"word"`
	Caught               bool              `json:"caught"`
	ImpostorGuess        string            `json:"impostorGuess"`
	ImpostorGuessedRight bool              `json:"impostorGuessedRight"`
	Tally                map[PlayerID]int  `json:"tally"`
	ScoreDelta           map[PlayerID]int  `json:"scoreDelta"`
}

// State is the full game state. The engine is the sole mutator.
type State struct {
	Phase         Phase
	Players       []Player
	HostID        PlayerID
	Round         int // 1-based; 0 while in lobby
	TotalRounds   int
	ImpostorID    PlayerID
	Category      string
	Word          string
	TurnIndex     int
	Lap           int
	TotalLaps     int
	Strokes       []Stroke
	Votes         map[PlayerID]PlayerID
	ImpostorGuess string
	UsedWords     map[string]bool
	LastResult    *RoundResult
}

// WordSource supplies category/word pairs, excluding already-used words.
type WordSource interface {
	Pick(exclude map[string]bool) (category, word string, ok bool)
}
```

Create `internal/game/errors.go`:
```go
package game

import "errors"

var (
	ErrWrongPhase   = errors.New("action not allowed in current phase")
	ErrNotHost      = errors.New("only the host may perform this action")
	ErrNotYourTurn  = errors.New("not this player's turn")
	ErrUnknownPlayer = errors.New("unknown player")
	ErrTooFewPlayers = errors.New("need at least 4 players")
	ErrAlreadyVoted = errors.New("player already voted")
	ErrNotImpostor  = errors.New("only the impostor may guess")
	ErrNoWords      = errors.New("word source exhausted")
)

// MinPlayers is the minimum required to start a game.
const MinPlayers = 4
```

- [ ] **Step 4: Write the engine constructor**

Create `internal/game/engine.go`:
```go
package game

import "math/rand"

// Engine holds and mutates game state. It is not safe for concurrent use;
// the owning Room goroutine serializes all calls.
type Engine struct {
	state         State
	rng           *rand.Rand
	words         WordSource
	impostorOrder []int // player indices; round i uses impostorOrder[i-1]
}

// NewEngine creates a lobby-phase engine. totalRounds defaults to len(players)
// so every player is impostor exactly once when totalRounds == len(players).
func NewEngine(players []Player, host PlayerID, totalRounds int, rng *rand.Rand, words WordSource) *Engine {
	return &Engine{
		state: State{
			Phase:       PhaseLobby,
			Players:     append([]Player(nil), players...),
			HostID:      host,
			TotalRounds: totalRounds,
			TotalLaps:   2,
			UsedWords:   map[string]bool{},
		},
		rng:   rng,
		words: words,
	}
}

// State returns a copy-safe snapshot. Slices/maps are shallow-copied so callers
// cannot mutate engine internals.
func (e *Engine) State() State {
	s := e.state
	s.Players = append([]Player(nil), e.state.Players...)
	s.Strokes = append([]Stroke(nil), e.state.Strokes...)
	s.Votes = map[PlayerID]PlayerID{}
	for k, v := range e.state.Votes {
		s.Votes[k] = v
	}
	return s
}

// playerIndex returns the index of id in Players, or -1.
func (e *Engine) playerIndex(id PlayerID) int {
	for i, p := range e.state.Players {
		if p.ID == id {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/game/ -run TestNewEngine -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/game/
git commit -m "feat(game): domain types and engine constructor"
```

---

## Task 2: StartGame — lobby to first drawing round

**Files:**
- Create: `internal/game/events.go`
- Modify: `internal/game/engine.go`
- Test: `internal/game/engine_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/game/engine_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run TestStartGame -v`
Expected: FAIL — `undefined: RoundStarted`, `e.StartGame undefined`.

- [ ] **Step 3: Write the events**

Create `internal/game/events.go`:
```go
package game

// Event is something the engine emits when state changes. The server translates
// events into wire messages, applying per-player secret filtering.
type Event interface{ isEvent() }

// RoundStarted carries full round info; the server reveals Word only to
// non-impostors and Category to the impostor.
type RoundStarted struct {
	Round      int
	Category   string
	Word       string
	ImpostorID PlayerID
	Order      []PlayerID // draw order for this round
}

// TurnChanged announces whose turn it is to draw.
type TurnChanged struct {
	CurrentPlayer PlayerID
	Lap           int
	TotalLaps     int
}

// StrokeAdded broadcasts a committed stroke.
type StrokeAdded struct{ Stroke Stroke }

// PhaseChanged announces a phase transition.
type PhaseChanged struct{ Phase Phase }

// VoteRecorded announces that a vote was cast (not who it targeted).
type VoteRecorded struct {
	Voter      PlayerID
	VotesCast  int
	VotesTotal int
}

// RoundEnded carries the result of a completed round.
type RoundEnded struct{ Result RoundResult }

// GameEnded carries final standings.
type GameEnded struct{ FinalScores []Player }

func (RoundStarted) isEvent() {}
func (TurnChanged) isEvent()  {}
func (StrokeAdded) isEvent()  {}
func (PhaseChanged) isEvent() {}
func (VoteRecorded) isEvent() {}
func (RoundEnded) isEvent()   {}
func (GameEnded) isEvent()    {}
```

- [ ] **Step 4: Write StartGame and the round-setup helper**

Append to `internal/game/engine.go`:
```go
// StartGame moves from lobby to the first drawing round. Host-only.
func (e *Engine) StartGame(by PlayerID) ([]Event, error) {
	if e.state.Phase != PhaseLobby {
		return nil, ErrWrongPhase
	}
	if by != e.state.HostID {
		return nil, ErrNotHost
	}
	if len(e.state.Players) < MinPlayers {
		return nil, ErrTooFewPlayers
	}
	// Precompute a shuffled impostor order so each player is impostor at most
	// once before any repeats.
	e.impostorOrder = e.rng.Perm(len(e.state.Players))
	return e.beginRound(1)
}

// beginRound sets up round n: picks a word, assigns the impostor, resets the
// canvas and turn pointer, and returns the round-start events.
func (e *Engine) beginRound(n int) ([]Event, error) {
	cat, word, ok := e.words.Pick(e.state.UsedWords)
	if !ok {
		// Recover by resetting the used-word tracker once.
		e.state.UsedWords = map[string]bool{}
		cat, word, ok = e.words.Pick(e.state.UsedWords)
		if !ok {
			return nil, ErrNoWords
		}
	}
	e.state.UsedWords[word] = true

	impIdx := e.impostorOrder[(n-1)%len(e.impostorOrder)]
	e.state.Round = n
	e.state.Phase = PhaseDrawing
	e.state.Category = cat
	e.state.Word = word
	e.state.ImpostorID = e.state.Players[impIdx].ID
	e.state.TurnIndex = 0
	e.state.Lap = 0
	e.state.Strokes = nil
	e.state.Votes = map[PlayerID]PlayerID{}
	e.state.ImpostorGuess = ""
	e.state.LastResult = nil

	order := make([]PlayerID, len(e.state.Players))
	for i, p := range e.state.Players {
		order[i] = p.ID
	}
	return []Event{
		RoundStarted{Round: n, Category: cat, Word: word, ImpostorID: e.state.ImpostorID, Order: order},
		TurnChanged{CurrentPlayer: e.state.Players[0].ID, Lap: 0, TotalLaps: e.state.TotalLaps},
	}, nil
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/game/ -run TestStartGame -v`
Expected: PASS (all three StartGame tests).

- [ ] **Step 6: Commit**

```bash
git add internal/game/
git commit -m "feat(game): start game, assign word and impostor, emit events"
```

---

## Task 3: AddStroke — turn rotation and transition to discussion

**Files:**
- Modify: `internal/game/engine.go`
- Test: `internal/game/engine_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/game/engine_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run 'TestAddStroke|TestDrawing' -v`
Expected: FAIL — `e.AddStroke undefined`.

- [ ] **Step 3: Write AddStroke**

Append to `internal/game/engine.go`:
```go
// AddStroke records the current drawer's stroke, advances the turn, and (when
// all laps are done) transitions to the discussion phase.
func (e *Engine) AddStroke(by PlayerID, s Stroke) ([]Event, error) {
	if e.state.Phase != PhaseDrawing {
		return nil, ErrWrongPhase
	}
	if by != e.state.Players[e.state.TurnIndex].ID {
		return nil, ErrNotYourTurn
	}
	s.By = by
	e.state.Strokes = append(e.state.Strokes, s)
	events := []Event{StrokeAdded{Stroke: s}}
	return append(events, e.advanceTurn()...), nil
}

// advanceTurn moves to the next drawer, wrapping laps, and returns the resulting
// TurnChanged or (at the end of the final lap) the transition to discussion.
func (e *Engine) advanceTurn() []Event {
	e.state.TurnIndex++
	if e.state.TurnIndex >= len(e.state.Players) {
		e.state.TurnIndex = 0
		e.state.Lap++
		if e.state.Lap >= e.state.TotalLaps {
			e.state.Phase = PhaseDiscussion
			return []Event{PhaseChanged{Phase: PhaseDiscussion}}
		}
	}
	return []Event{TurnChanged{
		CurrentPlayer: e.state.Players[e.state.TurnIndex].ID,
		Lap:           e.state.Lap,
		TotalLaps:     e.state.TotalLaps,
	}}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/game/ -run 'TestAddStroke|TestDrawing' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/
git commit -m "feat(game): stroke handling, turn rotation, drawing->discussion"
```

---

## Task 4: EndDiscussion — transition to voting

**Files:**
- Modify: `internal/game/engine.go`
- Test: `internal/game/engine_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/game/engine_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run TestEndDiscussion -v`
Expected: FAIL — `e.EndDiscussion undefined`.

- [ ] **Step 3: Write EndDiscussion**

Append to `internal/game/engine.go`:
```go
// EndDiscussion moves from discussion to voting. Triggered by the host or by the
// room's discussion timer (the room passes the host ID in the timer case).
func (e *Engine) EndDiscussion(by PlayerID) ([]Event, error) {
	if e.state.Phase != PhaseDiscussion {
		return nil, ErrWrongPhase
	}
	if by != e.state.HostID {
		return nil, ErrNotHost
	}
	e.state.Phase = PhaseVoting
	return []Event{PhaseChanged{Phase: PhaseVoting}}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/game/ -run TestEndDiscussion -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/
git commit -m "feat(game): discussion->voting transition"
```

---

## Task 5: CastVote — tally, catch determination, scoring, reveal

**Files:**
- Modify: `internal/game/engine.go`
- Test: `internal/game/engine_test.go`

**Design note — "caught":** the impostor is *caught* if they receive strictly the most votes (plurality). Ties (including a tie for first involving the impostor) count as **not caught** — the group failed to converge on them.

**Scoring (from spec):**
- Caught && impostor guesses word wrong → each non-impostor +1.
- Caught && impostor guesses word right → impostor +2 (handled in Task 6).
- Not caught → impostor +2.

When voting completes: if caught, go to `PhaseReveal` (impostor gets a guess in Task 6). If not caught, apply the impostor-wins scoring immediately and emit `RoundEnded`.

- [ ] **Step 1: Write the failing test**

Append to `internal/game/engine_test.go`:
```go
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
	// Each non-impostor votes for a different non-impostor; impostor votes for someone.
	// Result: no single player has a plurality -> not caught.
	_, _ = e.CastVote(others[0], others[1])
	_, _ = e.CastVote(others[1], others[2])
	_, _ = e.CastVote(others[2], others[0])
	_, err := e.CastVote(imp, others[0]) // last vote; others[0] now has 2, still...
	if err != nil {
		t.Fatalf("impostor vote: %v", err)
	}
	s := e.State()
	if s.Phase != PhaseGameOver && s.Phase != PhaseDrawing && s.Phase != PhaseReveal {
		// After a non-caught round the engine advances to the next round (drawing)
		// or ends the game; it must NOT be stuck in voting.
	}
	if s.LastResult == nil {
		t.Fatalf("expected a round result")
	}
	if s.LastResult.Caught {
		t.Fatalf("expected Caught=false when votes are split")
	}
	// Impostor should have gained 2 points.
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run 'TestVoting|TestDoubleVote' -v`
Expected: FAIL — `e.CastVote undefined`.

- [ ] **Step 3: Write CastVote plus tally/scoring helpers**

Append to `internal/game/engine.go`:
```go
// CastVote records a vote. When every player has voted, it tallies results:
// if the impostor is caught it moves to reveal (impostor may guess in Task 6);
// otherwise it scores the impostor's win and advances the round.
func (e *Engine) CastVote(voter, target PlayerID) ([]Event, error) {
	if e.state.Phase != PhaseVoting {
		return nil, ErrWrongPhase
	}
	if e.playerIndex(voter) < 0 || e.playerIndex(target) < 0 {
		return nil, ErrUnknownPlayer
	}
	if _, done := e.state.Votes[voter]; done {
		return nil, ErrAlreadyVoted
	}
	e.state.Votes[voter] = target
	events := []Event{VoteRecorded{
		Voter:      voter,
		VotesCast:  len(e.state.Votes),
		VotesTotal: len(e.state.Players),
	}}
	if len(e.state.Votes) < len(e.state.Players) {
		return events, nil
	}
	return append(events, e.finishVoting()...), nil
}

// tally counts votes received per player.
func (e *Engine) tally() map[PlayerID]int {
	t := map[PlayerID]int{}
	for _, target := range e.state.Votes {
		t[target]++
	}
	return t
}

// caughtByPlurality reports whether the impostor has strictly the most votes.
func (e *Engine) caughtByPlurality(t map[PlayerID]int) bool {
	impVotes := t[e.state.ImpostorID]
	if impVotes == 0 {
		return false
	}
	for id, v := range t {
		if id != e.state.ImpostorID && v >= impVotes {
			return false
		}
	}
	return true
}

// finishVoting evaluates the round once all votes are in.
func (e *Engine) finishVoting() []Event {
	t := e.tally()
	caught := e.caughtByPlurality(t)
	e.state.LastResult = &RoundResult{
		ImpostorID: e.state.ImpostorID,
		Word:       e.state.Word,
		Caught:     caught,
		Tally:      t,
		ScoreDelta: map[PlayerID]int{},
	}
	if caught {
		// Impostor gets a chance to steal the win by guessing the word.
		e.state.Phase = PhaseReveal
		return []Event{PhaseChanged{Phase: PhaseReveal}}
	}
	// Impostor evaded detection: +2 and advance.
	e.applyScore(e.state.ImpostorID, 2)
	return e.endRound()
}

// applyScore adds delta to a player's score and records it in the round result.
func (e *Engine) applyScore(id PlayerID, delta int) {
	for i := range e.state.Players {
		if e.state.Players[i].ID == id {
			e.state.Players[i].Score += delta
		}
	}
	if e.state.LastResult != nil {
		e.state.LastResult.ScoreDelta[id] += delta
	}
}

// endRound emits RoundEnded and either starts the next round or ends the game.
func (e *Engine) endRound() []Event {
	events := []Event{RoundEnded{Result: *e.state.LastResult}}
	if e.state.Round >= e.state.TotalRounds {
		e.state.Phase = PhaseGameOver
		return append(events, GameEnded{FinalScores: append([]Player(nil), e.state.Players...)})
	}
	next, err := e.beginRound(e.state.Round + 1)
	if err != nil {
		// Should not happen with a healthy word source; end the game defensively.
		e.state.Phase = PhaseGameOver
		return append(events, GameEnded{FinalScores: append([]Player(nil), e.state.Players...)})
	}
	return append(events, next...)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/game/ -run 'TestVoting|TestDoubleVote' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/game/
git commit -m "feat(game): voting, plurality catch, impostor-win scoring"
```

---

## Task 6: ImpostorGuess — steal-the-win path and round advance

**Files:**
- Modify: `internal/game/engine.go`
- Test: `internal/game/engine_test.go`

**Design note — matching:** the guess matches if it equals the word case-insensitively after trimming surrounding whitespace.

- [ ] **Step 1: Write the failing test**

Append to `internal/game/engine_test.go`:
```go
import "strings" // add to the existing import block if not present

func caughtEngine(t *testing.T, n int) *Engine {
	t.Helper()
	e := votingEngine(t, n)
	imp := e.State().ImpostorID
	for _, p := range e.state.Players {
		if _, err := e.CastVote(p.ID, imp); err != nil {
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
	// Round result reflects a correct steal; impostor +2.
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
		// Split votes so no one is caught.
		_, _ = e.CastVote(others[0], others[1])
		_, _ = e.CastVote(others[1], others[2])
		_, _ = e.CastVote(others[2], others[0])
		_, _ = e.CastVote(imp, others[0])
	}
	if e.State().Phase != PhaseGameOver {
		t.Fatalf("phase = %q, want game_over", e.State().Phase)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/game/ -run 'TestImpostor|TestOnlyImpostor|TestGameEnds' -v`
Expected: FAIL — `e.ImpostorGuess undefined`.

- [ ] **Step 3: Write ImpostorGuess**

Add `"strings"` to the import block in `internal/game/engine.go` (change `import "math/rand"` to a grouped import):
```go
import (
	"math/rand"
	"strings"
)
```

Append to `internal/game/engine.go`:
```go
// ImpostorGuess resolves the reveal phase after the impostor was caught. A
// correct guess steals the win (impostor +2); a wrong guess gives every
// non-impostor +1. Either way the round then advances.
func (e *Engine) ImpostorGuess(by PlayerID, guess string) ([]Event, error) {
	if e.state.Phase != PhaseReveal {
		return nil, ErrWrongPhase
	}
	if by != e.state.ImpostorID {
		return nil, ErrNotImpostor
	}
	right := strings.EqualFold(strings.TrimSpace(guess), strings.TrimSpace(e.state.Word))
	e.state.ImpostorGuess = guess
	e.state.LastResult.ImpostorGuess = guess
	e.state.LastResult.ImpostorGuessedRight = right
	if right {
		e.applyScore(e.state.ImpostorID, 2)
	} else {
		for _, p := range e.state.Players {
			if p.ID != e.state.ImpostorID {
				e.applyScore(p.ID, 1)
			}
		}
	}
	return e.endRound(), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/game/ -run 'TestImpostor|TestOnlyImpostor|TestGameEnds' -v`
Expected: PASS.

- [ ] **Step 5: Run the full engine suite and check coverage**

Run: `go test ./internal/game/ -cover`
Expected: PASS, coverage ≥ 85%.

- [ ] **Step 6: Commit**

```bash
git add internal/game/
git commit -m "feat(game): impostor guess, steal-win, round advance, game over"
```

---

## Task 7: WordBank implementing WordSource

**Files:**
- Create: `internal/wordbank/wordbank.go`
- Test: `internal/wordbank/wordbank_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/wordbank/wordbank_test.go`:
```go
package wordbank

import (
	"math/rand"
	"testing"
)

func TestPickReturnsPair(t *testing.T) {
	wb := New(rand.New(rand.NewSource(1)))
	cat, word, ok := wb.Pick(map[string]bool{})
	if !ok {
		t.Fatal("Pick returned ok=false on a fresh bank")
	}
	if cat == "" || word == "" {
		t.Fatalf("empty pair: %q/%q", cat, word)
	}
}

func TestPickExcludesUsed(t *testing.T) {
	wb := New(rand.New(rand.NewSource(1)))
	used := map[string]bool{}
	// Exhaust the bank; every returned word must be new.
	for i := 0; i < wb.Len(); i++ {
		_, word, ok := wb.Pick(used)
		if !ok {
			t.Fatalf("Pick failed at %d with capacity %d", i, wb.Len())
		}
		if used[word] {
			t.Fatalf("Pick returned already-used word %q", word)
		}
		used[word] = true
	}
	// Now everything is used -> ok=false.
	if _, _, ok := wb.Pick(used); ok {
		t.Fatal("expected ok=false when all words used")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wordbank/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the WordBank**

Create `internal/wordbank/wordbank.go`:
```go
package wordbank

import "math/rand"

type pair struct{ category, word string }

// WordBank is a static, in-memory source of category/word pairs. It satisfies
// game.WordSource without importing the game package (structural typing).
type WordBank struct {
	pairs []pair
	rng   *rand.Rand
}

// New returns a WordBank seeded with the built-in pack.
func New(rng *rand.Rand) *WordBank {
	return &WordBank{pairs: defaultPairs(), rng: rng}
}

// Len is the total number of pairs, useful for tests.
func (wb *WordBank) Len() int { return len(wb.pairs) }

// Pick returns a random pair whose word is not in exclude. ok is false if none
// remain.
func (wb *WordBank) Pick(exclude map[string]bool) (string, string, bool) {
	var avail []pair
	for _, p := range wb.pairs {
		if !exclude[p.word] {
			avail = append(avail, p)
		}
	}
	if len(avail) == 0 {
		return "", "", false
	}
	p := avail[wb.rng.Intn(len(avail))]
	return p.category, p.word, true
}

func defaultPairs() []pair {
	return []pair{
		{"Animal", "Giraffe"}, {"Animal", "Octopus"}, {"Animal", "Penguin"},
		{"Animal", "Kangaroo"}, {"Animal", "Hedgehog"}, {"Animal", "Dolphin"},
		{"Food", "Pizza"}, {"Food", "Sushi"}, {"Food", "Pancakes"},
		{"Food", "Popcorn"}, {"Food", "Spaghetti"}, {"Food", "Cupcake"},
		{"Object", "Umbrella"}, {"Object", "Telescope"}, {"Object", "Anchor"},
		{"Object", "Lighthouse"}, {"Object", "Hourglass"}, {"Object", "Compass"},
		{"Place", "Volcano"}, {"Place", "Desert"}, {"Place", "Castle"},
		{"Place", "Waterfall"}, {"Place", "Igloo"}, {"Place", "Windmill"},
		{"Sport", "Surfing"}, {"Sport", "Bowling"}, {"Sport", "Archery"},
		{"Sport", "Skateboarding"}, {"Sport", "Fencing"}, {"Sport", "Basketball"},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wordbank/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/wordbank/
git commit -m "feat(wordbank): static word/category source"
```

---

## Task 8: Wire protocol — envelope and typed payloads

**Files:**
- Create: `internal/wsproto/message.go`
- Test: `internal/wsproto/message_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/wsproto/message_test.go`:
```go
package wsproto

import (
	"encoding/json"
	"testing"
)

func TestDecodeEnvelopeAndPayload(t *testing.T) {
	raw := `{"type":"stroke","payload":{"points":[{"x":0.5,"y":0.5}],"color":"#000","width":3}}`
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Type != TypeStroke {
		t.Fatalf("type = %q, want %q", env.Type, TypeStroke)
	}
	var p StrokePayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if len(p.Points) != 1 || p.Points[0].X != 0.5 {
		t.Fatalf("bad points: %+v", p.Points)
	}
}

func TestEncodeServerMessage(t *testing.T) {
	env, err := Encode(TypePhaseChanged, PhaseChangedPayload{Phase: "voting"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(b) != `{"type":"phase_changed","payload":{"phase":"voting"}}` {
		t.Fatalf("unexpected json: %s", b)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/wsproto/ -v`
Expected: FAIL — `undefined: Envelope`.

- [ ] **Step 3: Write the protocol**

Create `internal/wsproto/message.go`:
```go
package wsproto

import "encoding/json"

// Message type constants. Client->server and server->client share one namespace.
const (
	// Client -> server
	TypeJoin          = "join"
	TypeStartGame     = "start_game"
	TypeStroke        = "stroke"
	TypeChatMessage   = "chat_message"
	TypeCastVote      = "cast_vote"
	TypeImpostorGuess = "impostor_guess"
	TypeEndDiscussion = "end_discussion"

	// Server -> client
	TypeRoomState    = "room_state"
	TypePlayerJoined = "player_joined"
	TypePlayerLeft   = "player_left"
	TypeRoundStarted = "round_started"
	TypeStrokeBroadcast = "stroke_broadcast"
	TypeTurnChanged  = "turn_changed"
	TypePhaseChanged = "phase_changed"
	TypeVoteUpdate   = "vote_update"
	TypeRoundResult  = "round_result"
	TypeGameOver     = "game_over"
	TypeChatBroadcast = "chat_broadcast"
	TypeError        = "error"
)

// Envelope is the outer wire frame for every message.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Encode builds an Envelope from a typed payload.
func Encode(t string, payload any) (Envelope, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Type: t, Payload: b}, nil
}

// ---- Client -> server payloads ----

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type JoinPayload struct {
	Name           string `json:"name"`
	ReconnectToken string `json:"reconnectToken,omitempty"`
}

type StrokePayload struct {
	Points []Point `json:"points"`
	Color  string  `json:"color"`
	Width  float64 `json:"width"`
}

type ChatPayload struct {
	Text string `json:"text"`
}

type VotePayload struct {
	Target string `json:"target"`
}

type ImpostorGuessPayload struct {
	Guess string `json:"guess"`
}

// ---- Server -> client payloads ----

type PhaseChangedPayload struct {
	Phase string `json:"phase"`
}

type TurnChangedPayload struct {
	CurrentPlayer string `json:"currentPlayer"`
	Lap           int    `json:"lap"`
	TotalLaps     int    `json:"totalLaps"`
}

type ErrorPayload struct {
	Message string `json:"message"`
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/wsproto/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/wsproto/
git commit -m "feat(wsproto): message envelope and typed payloads"
```

---

## Task 9: Client — a single WebSocket connection

**Files:**
- Create: `internal/room/client.go`

This task has no standalone unit test; it is exercised by the integration test in Task 13. It defines the connection abstraction the Room broadcasts to.

- [ ] **Step 1: Write the client**

Create `internal/room/client.go`:
```go
package room

import (
	"context"
	"encoding/json"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// Client is one player's live WebSocket connection.
type Client struct {
	PlayerID game.PlayerID
	Name     string
	conn     *websocket.Conn
	send     chan wsproto.Envelope
}

// newClient wraps a websocket connection.
func newClient(id game.PlayerID, name string, conn *websocket.Conn) *Client {
	return &Client{
		PlayerID: id,
		Name:     name,
		conn:     conn,
		send:     make(chan wsproto.Envelope, 32),
	}
}

// writeLoop drains the send channel to the socket until the context is done.
func (c *Client) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-c.send:
			if !ok {
				return
			}
			b, err := json.Marshal(env)
			if err != nil {
				continue
			}
			if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
				return
			}
		}
	}
}

// trySend enqueues a message, dropping it if the buffer is full (a slow client
// must never block the room goroutine).
func (c *Client) trySend(env wsproto.Envelope) {
	select {
	case c.send <- env:
	default:
	}
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/room/`
Expected: no output, exit 0.

- [ ] **Step 3: Commit**

```bash
git add internal/room/client.go
git commit -m "feat(room): websocket client wrapper with buffered send"
```

---

## Task 10: Room actor — inbox loop, engine calls, broadcast with secret filtering

**Files:**
- Create: `internal/room/room.go`

The Room owns one engine and is the only goroutine that touches it. Inbound actions arrive on `inbox`; the Run loop applies them and broadcasts resulting events. `round_started` is filtered per player so the impostor never receives the word.

- [ ] **Step 1: Write the room**

Create `internal/room/room.go`:
```go
package room

import (
	"context"
	"encoding/json"
	"math/rand"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wordbank"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// inbound couples a decoded envelope with its sender.
type inbound struct {
	from    game.PlayerID
	envelope wsproto.Envelope
}

// registration/unregistration requests to the room loop.
type joinReq struct {
	client *Client
	resp   chan struct{}
}

// Room is the actor goroutine that owns a single game.
type Room struct {
	Code    string
	engine  *game.Engine
	clients map[game.PlayerID]*Client

	inbox   chan inbound
	joins   chan joinReq
	leaves  chan game.PlayerID
	done    chan struct{}

	discussionTimer *time.Timer
	discussionDur   time.Duration
}

// NewRoom builds a lobby-phase room. Players are added as they join (Task 12
// creates their engine entries before the game starts); for simplicity in v1 the
// engine is created once enough players have joined the lobby.
func NewRoom(code string, players []game.Player, host game.PlayerID, seed int64) *Room {
	rng := rand.New(rand.NewSource(seed))
	wb := wordbank.New(rand.New(rand.NewSource(seed + 1)))
	return &Room{
		Code:          code,
		engine:        game.NewEngine(players, host, len(players), rng, wb),
		clients:       map[game.PlayerID]*Client{},
		inbox:         make(chan inbound, 64),
		joins:         make(chan joinReq, 8),
		leaves:        make(chan game.PlayerID, 8),
		done:          make(chan struct{}),
		discussionDur: 45 * time.Second,
	}
}

// Run is the single-goroutine event loop. Nothing else mutates the engine.
func (r *Room) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case j := <-r.joins:
			r.clients[j.client.PlayerID] = j.client
			r.sendSnapshot(j.client)
			close(j.resp)
		case id := <-r.leaves:
			delete(r.clients, id)
		case msg := <-r.inbox:
			r.handle(msg)
		}
	}
}

// Join registers a client and blocks until the room has processed it.
func (r *Room) Join(c *Client) {
	resp := make(chan struct{})
	r.joins <- joinReq{client: c, resp: resp}
	<-resp
}

// Leave unregisters a client.
func (r *Room) Leave(id game.PlayerID) { r.leaves <- id }

// Submit hands an inbound message to the loop.
func (r *Room) Submit(from game.PlayerID, env wsproto.Envelope) {
	r.inbox <- inbound{from: from, envelope: env}
}

// handle dispatches one inbound message to the engine and broadcasts events.
func (r *Room) handle(msg inbound) {
	switch msg.envelope.Type {
	case wsproto.TypeStartGame:
		r.apply(r.engine.StartGame(msg.from))
	case wsproto.TypeStroke:
		var p wsproto.StrokePayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, "bad stroke payload")
			return
		}
		r.apply(r.engine.AddStroke(msg.from, toStroke(msg.from, p)))
	case wsproto.TypeEndDiscussion:
		r.apply(r.engine.EndDiscussion(msg.from))
	case wsproto.TypeCastVote:
		var p wsproto.VotePayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, "bad vote payload")
			return
		}
		r.apply(r.engine.CastVote(msg.from, game.PlayerID(p.Target)))
	case wsproto.TypeImpostorGuess:
		var p wsproto.ImpostorGuessPayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, "bad guess payload")
			return
		}
		r.apply(r.engine.ImpostorGuess(msg.from, p.Guess))
	case wsproto.TypeChatMessage:
		var p wsproto.ChatPayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.broadcastChat(msg.from, p.Text)
	default:
		r.sendError(msg.from, "unknown message type")
	}
}

// apply broadcasts engine events, or reports an engine error to the actor.
func (r *Room) apply(events []game.Event, err error) {
	if err != nil {
		// Errors are per-action; we cannot know the sender here, so broadcast a
		// generic error only in development. In v1 we swallow validation errors
		// (client UI prevents most of them). Kept explicit for future logging.
		return
	}
	for _, ev := range events {
		r.broadcastEvent(ev)
	}
}

func toStroke(by game.PlayerID, p wsproto.StrokePayload) game.Stroke {
	pts := make([]game.Point, len(p.Points))
	for i, pt := range p.Points {
		pts[i] = game.Point{X: pt.X, Y: pt.Y}
	}
	return game.Stroke{By: by, Points: pts, Color: p.Color, Width: p.Width}
}
```

- [ ] **Step 2: Verify it compiles (broadcast helpers come next)**

Run: `go build ./internal/room/ 2>&1 | head`
Expected: errors about undefined `sendSnapshot`, `sendError`, `broadcastEvent`, `broadcastChat` — these are added in Task 11. Do not commit yet.

---

## Task 11: Room broadcast helpers and discussion timer

**Files:**
- Create: `internal/room/broadcast.go`

- [ ] **Step 1: Write the broadcast helpers**

Create `internal/room/broadcast.go`:
```go
package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// broadcast sends an envelope to every connected client.
func (r *Room) broadcast(env wsproto.Envelope) {
	for _, c := range r.clients {
		c.trySend(env)
	}
}

// sendTo sends an envelope to one client if present.
func (r *Room) sendTo(id game.PlayerID, env wsproto.Envelope) {
	if c, ok := r.clients[id]; ok {
		c.trySend(env)
	}
}

func (r *Room) sendError(id game.PlayerID, msg string) {
	env, err := wsproto.Encode(wsproto.TypeError, wsproto.ErrorPayload{Message: msg})
	if err == nil {
		r.sendTo(id, env)
	}
}

// sendSnapshot sends the full current state to a (re)joining client. The word is
// omitted if the recipient is the impostor.
func (r *Room) sendSnapshot(c *Client) {
	s := r.engine.State()
	snap := stateSnapshot(s, c.PlayerID)
	if env, err := wsproto.Encode(wsproto.TypeRoomState, snap); err == nil {
		c.trySend(env)
	}
}

// broadcastEvent fans one engine event out to clients with per-player filtering.
func (r *Room) broadcastEvent(ev game.Event) {
	switch e := ev.(type) {
	case game.RoundStarted:
		// Reveal the word to everyone EXCEPT the impostor; the impostor gets the
		// category only.
		for id, c := range r.clients {
			payload := map[string]any{
				"round":    e.Round,
				"category": e.Category,
				"order":    e.Order,
				"youAreImpostor": id == e.ImpostorID,
			}
			if id != e.ImpostorID {
				payload["word"] = e.Word
			}
			if env, err := wsproto.Encode(wsproto.TypeRoundStarted, payload); err == nil {
				c.trySend(env)
			}
		}
	case game.TurnChanged:
		env, _ := wsproto.Encode(wsproto.TypeTurnChanged, wsproto.TurnChangedPayload{
			CurrentPlayer: string(e.CurrentPlayer), Lap: e.Lap, TotalLaps: e.TotalLaps,
		})
		r.broadcast(env)
	case game.StrokeAdded:
		env, _ := wsproto.Encode(wsproto.TypeStrokeBroadcast, e.Stroke)
		r.broadcast(env)
	case game.PhaseChanged:
		env, _ := wsproto.Encode(wsproto.TypePhaseChanged, wsproto.PhaseChangedPayload{Phase: string(e.Phase)})
		r.broadcast(env)
		r.onPhaseChange(e.Phase)
	case game.VoteRecorded:
		env, _ := wsproto.Encode(wsproto.TypeVoteUpdate, map[string]any{
			"votesCast": e.VotesCast, "votesTotal": e.VotesTotal,
		})
		r.broadcast(env)
	case game.RoundEnded:
		env, _ := wsproto.Encode(wsproto.TypeRoundResult, e.Result)
		r.broadcast(env)
	case game.GameEnded:
		env, _ := wsproto.Encode(wsproto.TypeGameOver, map[string]any{"finalScores": e.FinalScores})
		r.broadcast(env)
	}
}

func (r *Room) broadcastChat(from game.PlayerID, text string) {
	env, err := wsproto.Encode(wsproto.TypeChatBroadcast, map[string]any{
		"from": string(from), "text": text,
	})
	if err == nil {
		r.broadcast(env)
	}
}

// onPhaseChange starts/stops the discussion timer.
func (r *Room) onPhaseChange(p game.Phase) {
	if r.discussionTimer != nil {
		r.discussionTimer.Stop()
		r.discussionTimer = nil
	}
	if p == game.PhaseDiscussion {
		host := r.engine.State().HostID
		r.discussionTimer = time.AfterFunc(r.discussionDur, func() {
			// Timer fires on its own goroutine; route back through the inbox so
			// the engine is only ever touched by the Run loop.
			r.Submit(host, wsproto.Envelope{Type: wsproto.TypeEndDiscussion})
		})
	}
}

// stateSnapshot builds a room_state payload, hiding the word from the impostor.
func stateSnapshot(s game.State, viewer game.PlayerID) map[string]any {
	snap := map[string]any{
		"phase":       string(s.Phase),
		"players":     s.Players,
		"hostId":      string(s.HostID),
		"round":       s.Round,
		"totalRounds": s.TotalRounds,
		"category":    s.Category,
		"strokes":     s.Strokes,
		"turnIndex":   s.TurnIndex,
		"lap":         s.Lap,
		"totalLaps":   s.TotalLaps,
	}
	if s.Phase != game.PhaseLobby {
		snap["youAreImpostor"] = viewer == s.ImpostorID
		if viewer != s.ImpostorID {
			snap["word"] = s.Word
		}
	}
	if s.LastResult != nil {
		snap["lastResult"] = s.LastResult
	}
	return snap
}
```

- [ ] **Step 2: Verify the room package compiles**

Run: `go build ./internal/room/`
Expected: no output, exit 0.

- [ ] **Step 3: Commit Tasks 10–11 together**

```bash
git add internal/room/room.go internal/room/broadcast.go
git commit -m "feat(room): actor loop, event broadcast, secret filtering, discussion timer"
```

---

## Task 12: Hub — room registry, code generation, HTTP + WS server, reconnect tokens

**Files:**
- Create: `internal/hub/hub.go`
- Create: `internal/server/server.go`
- Test: `internal/hub/hub_test.go`

**Design note — lobby membership:** for v1, a room's player set is fixed when the room is created via `POST /api/rooms` (the host supplies their name), and additional players are appended as they join over WebSocket *while the room is still in the lobby phase*. To keep the engine immutable in shape mid-game, joins after `start_game` are treated as reconnects only (matching an existing seat by token) — new seats are rejected once the game has started.

- [ ] **Step 1: Write the failing hub test**

Create `internal/hub/hub_test.go`:
```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/hub/ -v`
Expected: FAIL — `undefined: New`.

- [ ] **Step 3: Write the hub**

Create `internal/hub/hub.go`:
```go
package hub

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/room"
)

// CodeLen is the length of a room join code.
const CodeLen = 4

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars

// entry couples a room with its cancel func and metadata for idle sweeping.
type entry struct {
	room   *room.Room
	cancel context.CancelFunc
	host   game.PlayerID
	seed   int64
}

// Hub owns the lifecycle of all rooms.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*entry
	rng   *rand.Rand
	seq   int64
}

// New creates an empty hub.
func New() *Hub {
	return &Hub{
		rooms: map[string]*entry{},
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// CreateRoom registers a new room whose only member (initially) is the host, and
// returns its join code. The engine's player list grows as players join in the
// lobby (see server.go).
func (h *Hub) CreateRoom(hostName string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	code := h.uniqueCodeLocked()
	h.seq++
	seed := time.Now().UnixNano() + h.seq
	// The host is player index 0. Its stable PlayerID is assigned by the server
	// on first WS connect; here we pre-seat a placeholder host id equal to code+"-host".
	host := game.PlayerID(code + "-host")
	players := []game.Player{{ID: host, Name: hostName}}
	r := room.NewRoom(code, players, host, seed)
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	h.rooms[code] = &entry{room: r, cancel: cancel, host: host, seed: seed}
	return code
}

// Get returns a room by code.
func (h *Hub) Get(code string) (*room.Room, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[code]
	if !ok {
		return nil, false
	}
	return e.room, true
}

// HostID returns the pre-seated host id for a room.
func (h *Hub) HostID(code string) (game.PlayerID, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[code]
	if !ok {
		return "", false
	}
	return e.host, true
}

func (h *Hub) uniqueCodeLocked() string {
	for {
		b := make([]byte, CodeLen)
		for i := range b {
			b[i] = codeAlphabet[h.rng.Intn(len(codeAlphabet))]
		}
		code := string(b)
		if _, exists := h.rooms[code]; !exists {
			return code
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/hub/ -v`
Expected: PASS.

- [ ] **Step 5: Commit the hub**

```bash
git add internal/hub/
git commit -m "feat(hub): room registry and unambiguous code generation"
```

- [ ] **Step 6: Write the HTTP + WS server**

Create `internal/server/server.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/room"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// Server wires HTTP routes to the hub.
type Server struct {
	hub *hub.Hub
	mux *http.ServeMux
}

// New builds a Server with routes registered.
func New(h *hub.Hub) *Server {
	s := &Server{hub: h, mux: http.NewServeMux()}
	s.mux.HandleFunc("POST /api/rooms", s.createRoom)
	s.mux.HandleFunc("/ws/room/{code}", s.joinRoom)
	return s
}

// Handler exposes the mux (for httptest and main).
func (s *Server) Handler() http.Handler { return s.mux }

type createRoomReq struct {
	Name string `json:"name"`
}
type createRoomResp struct {
	Code string `json:"code"`
}

func (s *Server) createRoom(w http.ResponseWriter, r *http.Request) {
	var req createRoomReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Name) == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	code := s.hub.CreateRoom(req.Name)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(createRoomResp{Code: code})
}

func (s *Server) joinRoom(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	rm, ok := s.hub.Get(code)
	if !ok {
		http.Error(w, "no such room", http.StatusNotFound)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"}, // dev; tighten for prod deploy
	})
	if err != nil {
		return
	}
	ctx := r.Context()

	// First frame must be a join message naming the player.
	name, playerID, err := readJoin(ctx, conn, code, s.hub)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "expected join")
		return
	}

	c := room.NewClientForServer(playerID, name, conn)
	rm.Join(c)
	defer rm.Leave(playerID)

	go c.WriteLoopForServer(ctx)
	readLoop(ctx, conn, rm, playerID)
}

// readJoin blocks for the initial join frame and resolves the player's ID.
// A join naming the host slot claims the pre-seated host id; everyone else gets
// a fresh id derived from their name + a counter handled inside the room.
func readJoin(ctx context.Context, conn *websocket.Conn, code string, h *hub.Hub) (string, game.PlayerID, error) {
	_, data, err := conn.Read(ctx)
	if err != nil {
		return "", "", err
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.Type != wsproto.TypeJoin {
		return "", "", errBadJoin
	}
	var p wsproto.JoinPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil || strings.TrimSpace(p.Name) == "" {
		return "", "", errBadJoin
	}
	// Reconnect token (if valid) restores the seat; otherwise mint an id.
	if p.ReconnectToken != "" {
		return p.Name, game.PlayerID(p.ReconnectToken), nil
	}
	if host, ok := h.HostID(code); ok && strings.EqualFold(p.Name, hostName(host)) {
		return p.Name, host, nil
	}
	return p.Name, game.PlayerID(code + "-" + p.Name), nil
}

func hostName(_ game.PlayerID) string { return "" } // host matched by token in v1

var errBadJoin = &joinError{}

type joinError struct{}

func (*joinError) Error() string { return "bad join frame" }

func readLoop(ctx context.Context, conn *websocket.Conn, rm *room.Room, id game.PlayerID) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var env wsproto.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}
		rm.Submit(id, env)
	}
}

var _ = context.Background
```

**Design note:** the `Client` fields used above (`NewClientForServer`, `WriteLoopForServer`) are exported shims so `server` can construct clients without `room` internals leaking. Add them to `internal/room/client.go`.

- [ ] **Step 7: Add exported client shims**

Append to `internal/room/client.go`:
```go
import "nhooyr.io/websocket" // ensure this import exists in the file's import block

// NewClientForServer is the exported constructor used by the server package.
func NewClientForServer(id game.PlayerID, name string, conn *websocket.Conn) *Client {
	return newClient(id, name, conn)
}

// WriteLoopForServer runs the client's write pump (exported for the server).
func (c *Client) WriteLoopForServer(ctx context.Context) { c.writeLoop(ctx) }
```

- [ ] **Step 8: Verify the whole tree compiles**

Run: `go build ./...`
Expected: no output, exit 0. Fix any import errors (the `game` import in server.go is used via `game.PlayerID`).

- [ ] **Step 9: Commit the server**

```bash
git add internal/server/ internal/room/client.go
git commit -m "feat(server): HTTP room creation and WebSocket join/read loops"
```

---

## Task 13: Integration test and entrypoint

**Files:**
- Create: `internal/server/server_test.go`
- Create: `cmd/fauxtist/main.go`

- [ ] **Step 1: Write the integration test**

Create `internal/server/server_test.go`:
```go
package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func dial(t *testing.T, wsURL, name string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	env, _ := wsproto.Encode(wsproto.TypeJoin, wsproto.JoinPayload{Name: name})
	b, _ := json.Marshal(env)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write join: %v", err)
	}
	return c
}

func readEnv(t *testing.T, c *websocket.Conn) wsproto.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return env
}

func TestJoinReceivesRoomState(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	// Create a room over HTTP.
	body := strings.NewReader(`{"name":"Alice"}`)
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	if cr.Code == "" {
		t.Fatal("no room code returned")
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code
	c := dial(t, wsURL, "Alice")
	defer c.Close(websocket.StatusNormalClosure, "")

	env := readEnv(t, c)
	if env.Type != wsproto.TypeRoomState {
		t.Fatalf("first message type = %q, want room_state", env.Type)
	}
}

func TestStrokeBroadcastsToAllClients(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	a := dial(t, wsURL, "Alice")
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dial(t, wsURL, "Bob")
	defer b.Close(websocket.StatusNormalClosure, "")

	// Drain each client's initial room_state and player_joined frames briefly.
	_ = readEnv(t, a)
	_ = readEnv(t, b)

	// This test asserts the transport path: a chat message from A reaches B.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chat, _ := wsproto.Encode(wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "hi"})
	cb, _ := json.Marshal(chat)
	if err := a.Write(ctx, websocket.MessageText, cb); err != nil {
		t.Fatalf("write chat: %v", err)
	}

	// B should eventually receive a chat_broadcast.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		env := readEnv(t, b)
		if env.Type == wsproto.TypeChatBroadcast {
			return
		}
	}
	t.Fatal("Bob never received chat_broadcast")
}
```

- [ ] **Step 2: Run the integration test to verify it fails or passes**

Run: `go test ./internal/server/ -v`
Expected: initially may FAIL if join-id/room membership wiring is incomplete. If `TestStrokeBroadcastsToAllClients` fails because Bob is not registered in the room, fix `readJoin`/room membership so any joining client is added to `r.clients` (the Room already registers any client via `Join`; ensure the server calls `rm.Join` for every connection, which it does). Re-run until PASS.

- [ ] **Step 3: Write the entrypoint**

Create `cmd/fauxtist/main.go`:
```go
package main

import (
	"log"
	"net/http"
	"os"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/server"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	h := hub.New()
	srv := server.New(h)
	log.Printf("fauxtist listening on :%s", port)
	if err := http.ListenAndServe(":"+port, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
```

- [ ] **Step 4: Verify the full build and run**

Run: `go build ./... && go vet ./...`
Expected: no output, exit 0.

Run: `PORT=8080 go run ./cmd/fauxtist &` then `curl -s -XPOST localhost:8080/api/rooms -d '{"name":"Alice"}'`
Expected: JSON like `{"code":"ABCD"}`. Then `kill %1`.

- [ ] **Step 5: Run the whole suite with coverage**

Run: `go test ./... -cover`
Expected: all packages PASS; `internal/game` coverage ≥ 85%.

- [ ] **Step 6: Commit**

```bash
git add internal/server/server_test.go cmd/fauxtist/main.go
git commit -m "feat(server): integration tests and entrypoint"
```

- [ ] **Step 7: Push**

```bash
git push
```

---

## Self-Review Notes (addressed in this plan)

- **Spec coverage:** room codes (Task 12), 4–8 players (min enforced in `StartGame` Task 2; max enforcement is a lobby-join check to add in the frontend plan when join-while-lobby is finalized), one-stroke-per-turn with 2 laps (Task 3), discussion→voting (Task 4), plurality catch + scoring (Task 5), impostor steal-win (Task 6), word bank with used-word reset (Tasks 2 & 7), server-authoritative validation (engine returns errors for wrong phase/turn), secret filtering (Task 11), reconnect tokens (Task 12 `readJoin`), single-binary embed + Docker/Render (deferred to the frontend/deploy plan).
- **Known follow-ups for the frontend/deploy plan:** (1) finalize lobby join flow and max-8 enforcement at connection time; (2) `go:embed` of the built React app and static file serving; (3) Dockerfile + Render config; (4) turn timeout auto-advance (optional polish); (5) tighten `OriginPatterns` from `"*"` to the deployed origin.
- **Type consistency:** engine method names (`StartGame`, `AddStroke`, `EndDiscussion`, `CastVote`, `ImpostorGuess`) are used identically in `room.handle`. Event types in `events.go` match the `switch` in `broadcastEvent`. Message type constants in `wsproto` match both the room dispatch and the integration test.
```
