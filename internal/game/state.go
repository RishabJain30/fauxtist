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
	ImpostorID           PlayerID         `json:"impostorId"`
	Word                 string           `json:"word"`
	Caught               bool             `json:"caught"`
	ImpostorGuess        string           `json:"impostorGuess"`
	ImpostorGuessedRight bool             `json:"impostorGuessedRight"`
	Tally                map[PlayerID]int `json:"tally"`
	ScoreDelta           map[PlayerID]int `json:"scoreDelta"`
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
