package game

import "sort"

// This file implements steps 16–17 of resolution — Domination streaks and
// victory evaluation — plus the ranked standings and tiebreaker chain used to
// decide a winner (or a shared win) at the final round.

// nonForfeitedIDs returns the sorted ids of players still in the match.
func (s *State) nonForfeitedIDs() []PlayerID {
	var out []PlayerID
	for _, pid := range s.SortedPlayerIDs() {
		if !s.player(pid).Forfeited {
			out = append(out, pid)
		}
	}
	return out
}

// updateDominationAndVictory runs after relic influence is awarded: it updates
// each player's Domination streak, records the round's influence totals, and
// decides whether the match ends now (forfeit-to-one, Domination, or the final
// round). When it ends, it sets Result and pendingGameOver; AdvanceRound then
// transitions to GAME_OVER after the round summary.
func (s *State) updateDominationAndVictory() {
	// Step 16: Domination streaks. Controlling at least the threshold number
	// of relics begins or continues a streak; dropping below resets it.
	for i := range s.Players {
		if s.Players[i].Forfeited {
			s.Players[i].DominationStreak = 0
			continue
		}
		if s.relicsControlledBy(s.Players[i].ID) >= DominationThreshold {
			s.Players[i].DominationStreak++
		} else {
			s.Players[i].DominationStreak = 0
		}
	}

	if s.InfluenceHistory == nil {
		s.InfluenceHistory = map[PlayerID][]int{}
	}
	for _, pid := range s.SortedPlayerIDs() {
		s.InfluenceHistory[pid] = append(s.InfluenceHistory[pid], s.player(pid).Influence)
	}

	// Step 17: victory.
	active := s.nonForfeitedIDs()
	if len(active) == 0 {
		s.finishGame(VictoryNoContest, nil)
		return
	}
	if len(active) == 1 {
		s.finishGame(VictoryForfeit, []PlayerID{active[0]})
		return
	}
	for _, pid := range active {
		if s.player(pid).DominationStreak >= DominationConsecutive {
			s.finishGame(VictoryDomination, []PlayerID{pid})
			return
		}
	}
	if s.Round >= s.TotalRounds {
		winners := s.topRanked()
		reason := VictoryInfluence
		if len(winners) > 1 {
			reason = VictoryShared
		}
		s.finishGame(reason, winners)
	}
}

// endForfeitIfAlone ends the match by Forfeit (or No Contest) the moment only
// one (or zero) active player remains, without waiting for a round boundary.
// Used by the engine after a resign. Reports whether the match ended.
func (s *State) endForfeitIfAlone() bool {
	active := s.nonForfeitedIDs()
	switch len(active) {
	case 0:
		s.finishGame(VictoryNoContest, nil)
		return true
	case 1:
		s.finishGame(VictoryForfeit, []PlayerID{active[0]})
		return true
	default:
		return false
	}
}

// computeStandings ranks every player by the tiebreaker chain: influence,
// then relics controlled, then non-capital territories, then total armies,
// then remaining energy. Forfeited players always sort below active ones.
// Players identical on the whole chain share a rank.
func (s *State) computeStandings() []Standing {
	st := make([]Standing, 0, len(s.Players))
	for _, pid := range s.SortedPlayerIDs() {
		p := s.player(pid)
		relics, terr, armies := 0, 0, 0
		for _, tid := range s.SortedTileIDs() {
			t := s.Tiles[tid]
			if t.Owner != pid {
				continue
			}
			if t.Type == TileRelic {
				relics++
			}
			if t.Type != TileCapital {
				terr++
			}
			armies += t.Armies
		}
		st = append(st, Standing{
			Player:           pid,
			Influence:        p.Influence,
			RelicsControlled: relics,
			Territories:      terr,
			Armies:           armies,
			Energy:           p.Energy,
			Forfeited:        p.Forfeited,
		})
	}

	better := func(a, b Standing) bool {
		if a.Forfeited != b.Forfeited {
			return !a.Forfeited
		}
		if a.Influence != b.Influence {
			return a.Influence > b.Influence
		}
		if a.RelicsControlled != b.RelicsControlled {
			return a.RelicsControlled > b.RelicsControlled
		}
		if a.Territories != b.Territories {
			return a.Territories > b.Territories
		}
		if a.Armies != b.Armies {
			return a.Armies > b.Armies
		}
		if a.Energy != b.Energy {
			return a.Energy > b.Energy
		}
		return a.Player < b.Player // deterministic order only; does not break a shared rank
	}
	sort.SliceStable(st, func(i, j int) bool { return better(st[i], st[j]) })

	sameChain := func(a, b Standing) bool {
		return a.Forfeited == b.Forfeited &&
			a.Influence == b.Influence &&
			a.RelicsControlled == b.RelicsControlled &&
			a.Territories == b.Territories &&
			a.Armies == b.Armies &&
			a.Energy == b.Energy
	}
	rank := 1
	for i := range st {
		if i > 0 && !sameChain(st[i-1], st[i]) {
			rank = i + 1
		}
		st[i].Rank = rank
	}
	return st
}

// topRanked returns every active player sharing the best (rank 1) standing —
// one player for a clear win, several for a shared victory.
func (s *State) topRanked() []PlayerID {
	var out []PlayerID
	for _, x := range s.computeStandings() {
		if x.Rank == 1 && !x.Forfeited {
			out = append(out, x.Player)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// finishGame records the final, authoritative match result. Winners is one
// player for a clear/Domination/Forfeit win, several for a shared win, or
// empty for a no contest.
func (s *State) finishGame(reason VictoryReason, winners []PlayerID) {
	standings := s.computeStandings()
	stats := map[PlayerID]MatchStats{}
	for pid, st := range s.Stats {
		stats[pid] = *st
	}
	hist := map[PlayerID][]int{}
	for pid, h := range s.InfluenceHistory {
		hist[pid] = append([]int(nil), h...)
	}
	s.Result = &GameResult{
		Reason:           reason,
		Winners:          append([]PlayerID(nil), winners...),
		Standings:        standings,
		Stats:            stats,
		InfluenceHistory: hist,
	}
	s.pendingGameOver = true
}
