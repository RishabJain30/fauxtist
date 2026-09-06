package game

import "errors"

var (
	// Lifecycle / permission.
	ErrWrongPhase     = errors.New("action not allowed in current phase")
	ErrNotHost        = errors.New("only the host may perform this action")
	ErrUnknownPlayer  = errors.New("unknown player")
	ErrTooFewPlayers  = errors.New("need at least three active players")
	ErrTooManyPlayers = errors.New("room is full")
	ErrRoomFull       = errors.New("room is full")
	ErrNotInLobby     = errors.New("players can only be removed in the lobby")
	ErrGameStarted    = errors.New("the match has already started")
	ErrInvalidPreset  = errors.New("unknown match preset")
	ErrForfeited      = errors.New("player has left the match")

	// Command validation.
	ErrUnknownCommand   = errors.New("unknown command type")
	ErrUnknownTile      = errors.New("unknown tile")
	ErrNotOwned         = errors.New("tile is not owned by this player")
	ErrNotAdjacent      = errors.New("tiles are not adjacent")
	ErrCapitalTargeted  = errors.New("an enemy capital cannot be targeted")
	ErrBadArmyCount     = errors.New("invalid army count")
	ErrNotEnoughArmies  = errors.New("not enough armies for this command")
	ErrNotEnoughEnergy  = errors.New("not enough energy for this command")
	ErrTooManyCommands  = errors.New("too many commands submitted")
	ErrDuplicateCommand = errors.New("that command may only be used once this round")
	ErrBadTarget        = errors.New("that command cannot target this tile")
	ErrFauxUnavailable  = errors.New("no Faux Order available")
	ErrFauxOnHold       = errors.New("an auto-Hold declaration cannot be made Faux")
	ErrAlreadyLocked    = errors.New("orders are locked")
	ErrRecruitLimit     = errors.New("only one Recruit per round")
	ErrBuildLimit       = errors.New("only one Build per round")
	ErrMarchOriginLimit = errors.New("only one March per origin per round")
)
