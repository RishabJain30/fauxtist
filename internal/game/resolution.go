package game

import "sort"

// This file implements Fauxlands' deterministic, simultaneous round
// resolution. The whole round is computed atomically from an immutable
// planning-start snapshot: no dice, no dependence on Go map iteration order,
// no dependence on the order commands arrived over the network. Every map is
// iterated in sorted-id order, every player in sorted-id order, so the same
// inputs always produce the same outcome. Clients only animate the Resolution
// this produces; they never compute combat themselves.

// sanitizeCommands defensively re-validates a player's assembled real
// commands against the planning-start board and coerces anything illegal (or
// anything that would exceed a per-round limit, energy, or army reservation)
// to Hold. Submit-time validation already prevents this in practice; this is
// belt-and-suspenders so a resolution can never act on an illegal command.
func sanitizeCommands(s *State, pid PlayerID, cmds []Command) []Command {
	player := s.player(pid)
	if player == nil {
		return nil
	}
	out := make([]Command, 0, len(cmds))
	spent := 0
	recruited, built := false, false
	marchFrom := map[TileID]bool{}
	marchedOut := map[TileID]int{}
	fortifyOn := map[TileID]bool{}
	for _, c := range cmds {
		ok := s.validateSingleCommand(pid, c) == nil
		if ok {
			switch c.Type {
			case CmdMarch:
				if marchFrom[c.From] {
					ok = false
				} else if t := s.Tiles[c.From]; t == nil || t.Armies-(marchedOut[c.From]+c.Armies) < 1 {
					ok = false
				}
			case CmdRecruit:
				if recruited {
					ok = false
				}
			case CmdBuildFortress, CmdBuildMine:
				if built {
					ok = false
				}
			case CmdFortify:
				if fortifyOn[c.To] {
					ok = false
				}
			}
		}
		if ok && spent+c.EnergyCost() > player.Energy {
			ok = false
		}
		if !ok {
			c = HoldCommand()
		}
		switch c.Type {
		case CmdMarch:
			marchFrom[c.From] = true
			marchedOut[c.From] += c.Armies
		case CmdRecruit:
			recruited = true
		case CmdBuildFortress, CmdBuildMine:
			built = true
		case CmdFortify:
			fortifyOn[c.To] = true
		}
		spent += c.EnergyCost()
		out = append(out, c)
	}
	return out
}

type marchOrder struct {
	player PlayerID
	from   TileID
	to     TileID
	armies int
}

type buildOrder struct {
	player PlayerID
	to     TileID
	kind   CommandType
	cost   int
}

// resolveRound mutates s to the post-round board and returns the round's
// Resolution (animation timeline + summary + final public board). It performs
// steps 1–15 of the resolution order; step 16–17 (domination streaks and
// victory) run in victory.go via updateDominationAndVictory, called at the end
// so the summary can report the resulting streaks.
func resolveRound(s *State) Resolution {
	round := s.Round
	var frames []ResolutionFrame

	// --- Frozen planning-start snapshot: every combat decision reads these,
	// never the mutating board. ---
	startOwner := map[TileID]PlayerID{}
	startArmies := map[TileID]int{}
	startStruct := map[TileID]Structure{}
	for _, tid := range s.SortedTileIDs() {
		t := s.Tiles[tid]
		startOwner[tid] = t.Owner
		startArmies[tid] = t.Armies
		startStruct[tid] = t.Structure
	}

	energyBefore := map[PlayerID]int{}
	nonCapBefore := map[PlayerID]int{}
	for _, pid := range s.SortedPlayerIDs() {
		energyBefore[pid] = s.player(pid).Energy
	}
	for _, tid := range s.SortedTileIDs() {
		if t := s.Tiles[tid]; t.Owner != "" && t.Type != TileCapital {
			nonCapBefore[t.Owner]++
		}
	}

	armiesLost := map[PlayerID]int{}
	fauxUsed := map[PlayerID]bool{}

	// --- Steps 1–3: assemble, reveal Faux, defensively validate. ---
	realCmds := map[PlayerID][]Command{}
	for _, pid := range s.SortedPlayerIDs() {
		p := s.player(pid)
		if p.Forfeited {
			continue
		}
		if order, ok := s.Orders[pid]; ok && order.Faux {
			if decl, dok := s.Declarations[pid]; dok && decl.Submitted && decl.Command.Type != CmdHold && p.FauxAvailable {
				fauxUsed[pid] = true
			}
		}
		realCmds[pid] = sanitizeCommands(s, pid, s.realCommands(pid))
	}
	// Step 2 frames + mark Faux spent (sorted for determinism).
	for _, pid := range s.SortedPlayerIDs() {
		if fauxUsed[pid] {
			p := s.player(pid)
			p.FauxAvailable = false
			p.FauxUsedRound = round
			if st := s.Stats[pid]; st != nil {
				st.FauxUsedRound = round
			}
			frames = append(frames, ResolutionFrame{Kind: FrameFauxRevealed, Player: pid})
		}
	}

	// --- Step 4: reserve (deduct) energy for every command; failed Builds
	// are refunded at step 14. ---
	for _, pid := range s.SortedPlayerIDs() {
		p := s.player(pid)
		for _, c := range realCmds[pid] {
			p.Energy -= c.EnergyCost()
		}
	}

	// --- Collect commands into per-type, deterministically ordered lists. ---
	curArmies := map[TileID]int{}
	for tid, a := range startArmies {
		curArmies[tid] = a
	}
	fortifyBonus := map[TileID]int{}
	var recruits, fortifies []struct {
		player PlayerID
		to     TileID
	}
	var marches []marchOrder
	var builds []buildOrder
	for _, pid := range s.SortedPlayerIDs() {
		for _, c := range realCmds[pid] {
			switch c.Type {
			case CmdRecruit:
				recruits = append(recruits, struct {
					player PlayerID
					to     TileID
				}{pid, c.To})
			case CmdFortify:
				fortifies = append(fortifies, struct {
					player PlayerID
					to     TileID
				}{pid, c.To})
			case CmdMarch:
				marches = append(marches, marchOrder{pid, c.From, c.To, c.Armies})
			case CmdBuildFortress:
				builds = append(builds, buildOrder{pid, c.To, c.Type, CostBuildFortress})
			case CmdBuildMine:
				builds = append(builds, buildOrder{pid, c.To, c.Type, CostBuildMine})
			}
		}
	}

	// --- Step 5: apply Recruit. ---
	for _, r := range recruits {
		curArmies[r.to] += RecruitArmies
		frames = append(frames, ResolutionFrame{Kind: FrameRecruit, Player: r.player, To: r.to, Armies: RecruitArmies})
	}
	// --- Step 6: apply temporary Fortify bonuses. ---
	for _, f := range fortifies {
		fortifyBonus[f.to] += FortifyBonus
		frames = append(frames, ResolutionFrame{Kind: FrameFortify, Player: f.player, To: f.to})
	}
	// --- Step 7: remove outgoing March armies from origins. ---
	for _, m := range marches {
		curArmies[m.from] -= m.armies
		frames = append(frames, ResolutionFrame{Kind: FrameMarch, Player: m.player, From: m.from, To: m.to, Armies: m.armies})
	}
	// --- Step 8: friendly reinforcements merge before battle. ---
	for _, m := range marches {
		if startOwner[m.to] == m.player {
			curArmies[m.to] += m.armies
		}
	}
	// --- Step 9: aggregate hostile arrivals per destination, per player. ---
	incoming := map[TileID]map[PlayerID]int{}
	for _, m := range marches {
		if startOwner[m.to] != m.player {
			if incoming[m.to] == nil {
				incoming[m.to] = map[PlayerID]int{}
			}
			incoming[m.to][m.player] += m.armies
		}
	}

	// --- Steps 10–12: resolve every contested tile from the snapshot. ---
	ownerChange := map[TileID]PlayerID{}
	battledTiles := make([]TileID, 0, len(incoming))
	for tid := range incoming {
		battledTiles = append(battledTiles, tid)
	}
	sort.Slice(battledTiles, func(i, j int) bool { return battledTiles[i] < battledTiles[j] })

	for _, tid := range battledTiles {
		atk := incoming[tid]
		attackers := make([]PlayerID, 0, len(atk))
		for pid := range atk {
			attackers = append(attackers, pid)
		}
		sort.Slice(attackers, func(i, j int) bool { return attackers[i] < attackers[j] })

		sides := make([]BattleSide, 0, len(attackers))
		maxStr := -1
		var strongest []PlayerID
		for _, pid := range attackers {
			str := atk[pid]
			sides = append(sides, BattleSide{Player: pid, Armies: str, Strength: str})
			if str > maxStr {
				maxStr = str
				strongest = []PlayerID{pid}
			} else if str == maxStr {
				strongest = append(strongest, pid)
			}
		}

		defArmies := curArmies[tid]
		defBonus := fortifyBonus[tid]
		if startStruct[tid] == StructureFortress {
			defBonus += FortressBonus
		}
		defOwner := startOwner[tid]
		defEff := defArmies + defBonus
		defSide := &BattleSide{Player: defOwner, Armies: defArmies, Strength: defEff}

		captured := len(strongest) == 1 && maxStr > defEff
		frame := ResolutionFrame{Kind: FrameBattle, To: tid, Attackers: sides, Defender: defSide}

		if captured {
			winner := strongest[0]
			winnerArmies := atk[winner]
			second := defEff
			for _, sd := range sides {
				if sd.Player != winner && sd.Strength > second {
					second = sd.Strength
				}
			}
			survivors := winnerArmies - second
			if survivors < 1 {
				survivors = 1
			}
			armiesLost[winner] += winnerArmies - survivors
			for _, sd := range sides {
				if sd.Player != winner {
					armiesLost[sd.Player] += sd.Armies
				}
			}
			if defOwner != "" {
				armiesLost[defOwner] += defArmies
			}
			ownerChange[tid] = winner
			curArmies[tid] = survivors
			if st := s.Stats[winner]; st != nil {
				st.Captures++
			}
			frame.Winner = winner
			frame.Result = survivors
			frames = append(frames, frame)
			frames = append(frames, ResolutionFrame{Kind: FrameCapture, Player: winner, To: tid, Armies: survivors})
		} else {
			if defOwner != "" {
				remaining := defArmies - maxStr
				if remaining < 1 {
					remaining = 1
				}
				armiesLost[defOwner] += defArmies - remaining
				curArmies[tid] = remaining
			} else {
				curArmies[tid] = defArmies // neutral holds: guardian (1) stays, empty (0) stays
			}
			for _, sd := range sides {
				armiesLost[sd.Player] += sd.Armies
			}
			frame.Result = curArmies[tid]
			frames = append(frames, frame)
		}
	}

	// --- Step 11 (apply) + step 12 (remove captured structures): write the
	// new board. Every tile's army count comes from the working garrison. ---
	for _, tid := range s.SortedTileIDs() {
		t := s.Tiles[tid]
		if no, ok := ownerChange[tid]; ok {
			t.Owner = no
			if t.Type != TileCapital {
				t.Structure = StructureNone
			}
		}
		t.Armies = curArmies[tid]
	}

	// --- Steps 13–14: builds resolve after combat; refund if the tile was
	// lost. ---
	for _, b := range builds {
		t := s.Tiles[b.to]
		if t != nil && t.Owner == b.player {
			if b.kind == CmdBuildFortress {
				t.Structure = StructureFortress
				if st := s.Stats[b.player]; st != nil {
					st.FortressesBuilt++
				}
			} else {
				t.Structure = StructureMine
				if st := s.Stats[b.player]; st != nil {
					st.MinesBuilt++
				}
			}
			frames = append(frames, ResolutionFrame{Kind: FrameBuild, Player: b.player, To: b.to, Structure: t.Structure})
		} else {
			s.player(b.player).Energy += b.cost // refund escrow
			frames = append(frames, ResolutionFrame{Kind: FrameBuildFailed, Player: b.player, To: b.to})
		}
	}

	// --- Step 15: award relic influence. ---
	for _, tid := range s.SortedTileIDs() {
		t := s.Tiles[tid]
		if t.Type == TileRelic && t.Owner != "" {
			if p := s.player(t.Owner); p != nil && !p.Forfeited {
				p.Influence++
				frames = append(frames, ResolutionFrame{Kind: FrameRelic, Player: t.Owner, To: tid, Influence: 1})
			}
		}
	}

	// Accumulate match stats.
	for pid, lost := range armiesLost {
		if st := s.Stats[pid]; st != nil {
			st.ArmiesLost += lost
		}
	}

	// --- Steps 16–17: domination streaks + victory evaluation (may set
	// pendingGameOver / Result). Must run before the summary so it can report
	// the resulting streaks. ---
	s.updateDominationAndVictory()

	// --- Build the per-player round summary. ---
	summary := RoundSummary{Round: round}
	for _, pid := range s.SortedPlayerIDs() {
		p := s.player(pid)
		nonCapAfter := 0
		for _, tid := range s.SortedTileIDs() {
			if t := s.Tiles[tid]; t.Owner == pid && t.Type != TileCapital {
				nonCapAfter++
			}
		}
		relics := s.relicsControlledBy(pid)
		summary.Players = append(summary.Players, PlayerRoundSummary{
			Player:           pid,
			EnergyDelta:      p.Energy - energyBefore[pid],
			InfluenceDelta:   relics,
			TerritoryDelta:   nonCapAfter - nonCapBefore[pid],
			ArmiesLost:       armiesLost[pid],
			FauxUsed:         fauxUsed[pid],
			RelicsControlled: relics,
			DominationStreak: p.DominationStreak,
		})
	}

	// Public final board.
	board := make([]Tile, 0, len(s.Tiles))
	for _, tid := range s.SortedTileIDs() {
		board = append(board, *s.Tiles[tid])
	}

	res := Resolution{Round: round, Frames: frames, Summary: summary, Board: board}
	s.Resolution = &res
	return res
}
