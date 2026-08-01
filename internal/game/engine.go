package game

import (
	"math/rand"
	"strings"
)

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
	e.state.TotalRounds = len(e.state.Players)
	e.impostorOrder = e.rng.Perm(len(e.state.Players))
	return e.beginRound(1)
}

// UpsertPlayer adds a new player during the lobby, or renames an existing one
// (any phase, for reconnects). New players are rejected once the game has
// started or the room is full.
func (e *Engine) UpsertPlayer(p Player) error {
	if i := e.playerIndex(p.ID); i >= 0 {
		if p.Name != "" {
			e.state.Players[i].Name = p.Name
		}
		if p.Emoji != "" {
			e.state.Players[i].Emoji = p.Emoji
		}
		return nil
	}
	if e.state.Phase != PhaseLobby {
		return ErrWrongPhase
	}
	if len(e.state.Players) >= MaxPlayers {
		return ErrRoomFull
	}
	e.state.Players = append(e.state.Players, p)
	return nil
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
	// LastResult intentionally NOT cleared here: it holds the most recently
	// completed round's result so a client joining mid-next-round can still show
	// it. finishVoting overwrites it when the current round completes.

	order := make([]PlayerID, len(e.state.Players))
	for i, p := range e.state.Players {
		order[i] = p.ID
	}
	return []Event{
		RoundStarted{Round: n, Category: cat, Word: word, ImpostorID: e.state.ImpostorID, Order: order},
		TurnChanged{CurrentPlayer: e.state.Players[0].ID, Lap: 0, TotalLaps: e.state.TotalLaps},
	}, nil
}

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
	e.state.Phase = PhaseReveal
	if caught {
		// Impostor gets a chance to steal the win by guessing the word; the
		// round is not final yet, so no RoundEnded until the guess.
		return []Event{PhaseChanged{Phase: PhaseReveal}}
	}
	// Impostor evaded detection: +2. Result is final; hold on the reveal phase
	// until the room advances the round.
	e.applyScore(e.state.ImpostorID, 2)
	return append([]Event{PhaseChanged{Phase: PhaseReveal}}, e.finalizeRound()...)
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

// finalizeRound marks the round result final and holds on the reveal phase.
// The room advances to the next round (or ends the game) via AdvanceRound after
// a short reveal hold, so players can actually read the result.
func (e *Engine) finalizeRound() []Event {
	e.state.Phase = PhaseReveal
	return []Event{RoundEnded{Result: *e.state.LastResult}}
}

// Restart begins a fresh game from the game-over screen: resets scores and word
// history, keeps the same players, and starts round 1. Host-only.
func (e *Engine) Restart(by PlayerID) ([]Event, error) {
	if e.state.Phase != PhaseGameOver {
		return nil, ErrWrongPhase
	}
	if by != e.state.HostID {
		return nil, ErrNotHost
	}
	if len(e.state.Players) < MinPlayers {
		return nil, ErrTooFewPlayers
	}
	for i := range e.state.Players {
		e.state.Players[i].Score = 0
	}
	e.state.UsedWords = map[string]bool{}
	e.state.LastResult = nil
	e.state.TotalRounds = len(e.state.Players)
	e.impostorOrder = e.rng.Perm(len(e.state.Players))
	return e.beginRound(1)
}

// AdvanceRound leaves the reveal phase for the next round, or ends the game
// after the final round. Called by the room once the reveal hold elapses.
func (e *Engine) AdvanceRound() []Event {
	if e.state.Phase != PhaseReveal {
		return nil
	}
	if e.state.Round >= e.state.TotalRounds {
		e.state.Phase = PhaseGameOver
		return []Event{GameEnded{FinalScores: append([]Player(nil), e.state.Players...)}}
	}
	next, err := e.beginRound(e.state.Round + 1)
	if err != nil {
		// Should not happen with a healthy word source; end the game defensively.
		e.state.Phase = PhaseGameOver
		return []Event{GameEnded{FinalScores: append([]Player(nil), e.state.Players...)}}
	}
	return next
}

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
	return e.finalizeRound(), nil
}
