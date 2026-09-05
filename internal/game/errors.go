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
	ErrRoomFull      = errors.New("room is full")
	ErrNotInLobby    = errors.New("players can only be removed from the lobby")
)

// MinPlayers is the minimum required to start a game.
const MinPlayers = 4

// MaxPlayers is the maximum roster size for a room.
const MaxPlayers = 8
