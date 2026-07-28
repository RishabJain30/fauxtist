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
