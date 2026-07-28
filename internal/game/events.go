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
