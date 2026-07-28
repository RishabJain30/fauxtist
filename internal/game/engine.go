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
