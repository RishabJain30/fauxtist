package game

import "errors"

var (
	ErrWrongPhase    = errors.New("action not allowed in current phase")
	ErrNotHost       = errors.New("only the host may perform this action")
	ErrNotYourTurn   = errors.New("not this player's turn")
	ErrUnknownPlayer = errors.New("unknown player")
	ErrTooFewPlayers = errors.New("need at least 4 players")
	ErrAlreadyVoted  = errors.New("player already voted")
	ErrNotImpostor   = errors.New("only the impostor may guess")
	ErrNoWords       = errors.New("word source exhausted")
)

// MinPlayers is the minimum required to start a game.
const MinPlayers = 4
