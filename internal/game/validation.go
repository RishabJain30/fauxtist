package game

// This file is the authoritative, server-side command validator. Every
// command a client submits — whether a public declaration or a hidden order —
// is checked here against the current authoritative board before it can ever
// affect a resolution. The client's own legality hints are advisory only.

// validateSingleCommand checks one command's structural legality against the
// board for the given player: tile existence, ownership, adjacency, target
// type, and army bounds. It does NOT check energy affordability or per-round
// limits — those are draft-wide and handled by validateRealCommands.
func (s *State) validateSingleCommand(pid PlayerID, c Command) error {
	switch c.Type {
	case CmdHold:
		return nil
	case CmdMarch:
		return s.validateMarch(pid, c)
	case CmdFortify:
		return s.validateFortify(pid, c)
	case CmdRecruit:
		return s.validateRecruit(pid, c)
	case CmdBuildFortress:
		return s.validateBuildFortress(pid, c)
	case CmdBuildMine:
		return s.validateBuildMine(pid, c)
	default:
		return ErrUnknownCommand
	}
}

func (s *State) validateMarch(pid PlayerID, c Command) error {
	from := s.Tiles[c.From]
	to := s.Tiles[c.To]
	if from == nil || to == nil {
		return ErrUnknownTile
	}
	if from.Owner != pid {
		return ErrNotOwned
	}
	if !s.adjacent(c.From, c.To) {
		return ErrNotAdjacent
	}
	if to.Type == TileCapital && to.CapitalOwner != pid {
		return ErrCapitalTargeted
	}
	if c.Armies < MarchMinArmies || c.Armies > MarchMaxArmies {
		return ErrBadArmyCount
	}
	// At least one army must remain at the source (validated against the
	// current — beginning-of-round — garrison; recruits this round have not
	// been applied yet).
	if from.Armies-c.Armies < 1 {
		return ErrNotEnoughArmies
	}
	return nil
}

func (s *State) validateFortify(pid PlayerID, c Command) error {
	to := s.Tiles[c.To]
	if to == nil {
		return ErrUnknownTile
	}
	if to.Owner != pid {
		return ErrNotOwned
	}
	if to.Type == TileCapital {
		return ErrBadTarget
	}
	return nil
}

func (s *State) validateRecruit(pid PlayerID, c Command) error {
	to := s.Tiles[c.To]
	if to == nil {
		return ErrUnknownTile
	}
	if to.Owner != pid {
		return ErrNotOwned
	}
	isCapital := to.Type == TileCapital && to.CapitalOwner == pid
	isFortress := to.Structure == StructureFortress
	if !isCapital && !isFortress {
		return ErrBadTarget
	}
	return nil
}

func (s *State) validateBuildFortress(pid PlayerID, c Command) error {
	to := s.Tiles[c.To]
	if to == nil {
		return ErrUnknownTile
	}
	if to.Owner != pid {
		return ErrNotOwned
	}
	if to.Structure != StructureNone {
		return ErrBadTarget
	}
	// A Fortress may go on a normal tile or an unused mine site; never on a
	// relic or a capital.
	if to.Type != TileNormal && to.Type != TileMineSite {
		return ErrBadTarget
	}
	return nil
}

func (s *State) validateBuildMine(pid PlayerID, c Command) error {
	to := s.Tiles[c.To]
	if to == nil {
		return ErrUnknownTile
	}
	if to.Owner != pid {
		return ErrNotOwned
	}
	if to.Type != TileMineSite {
		return ErrBadTarget
	}
	if to.Structure != StructureNone {
		return ErrBadTarget
	}
	return nil
}

// validateRealCommands validates a player's complete ordered list of real
// commands as a set: each is structurally legal, the per-round limits hold
// (one Recruit, one Build, one March per origin, one Fortify per tile), total
// energy is affordable, and no origin marches out more armies than it has.
// The list is exactly what will resolve — Holds included — so it must already
// be the assembled real-command list (declaration-inclusive when not Faux).
func (s *State) validateRealCommands(pid PlayerID, cmds []Command) error {
	player := s.player(pid)
	if player == nil {
		return ErrUnknownPlayer
	}
	if len(cmds) > RealCommandSlots {
		return ErrTooManyCommands
	}

	var recruits, builds int
	marchFrom := map[TileID]bool{}
	fortifyOn := map[TileID]bool{}
	marchedFromOrigin := map[TileID]int{}
	energy := 0

	for _, c := range cmds {
		if err := s.validateSingleCommand(pid, c); err != nil {
			return err
		}
		energy += c.EnergyCost()
		switch c.Type {
		case CmdMarch:
			if marchFrom[c.From] {
				return ErrMarchOriginLimit
			}
			marchFrom[c.From] = true
			marchedFromOrigin[c.From] += c.Armies
		case CmdFortify:
			if fortifyOn[c.To] {
				return ErrDuplicateCommand
			}
			fortifyOn[c.To] = true
		case CmdRecruit:
			recruits++
			if recruits > 1 {
				return ErrRecruitLimit
			}
		case CmdBuildFortress, CmdBuildMine:
			builds++
			if builds > 1 {
				return ErrBuildLimit
			}
		}
	}

	if energy > player.Energy {
		return ErrNotEnoughEnergy
	}
	// Per-origin army reservation against the beginning-of-round garrison.
	for origin, out := range marchedFromOrigin {
		if t := s.Tiles[origin]; t != nil && t.Armies-out < 1 {
			return ErrNotEnoughArmies
		}
	}
	return nil
}
