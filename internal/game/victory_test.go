package game

import "testing"

// These tests exercise relic influence, Domination streaks, the victory
// evaluation, the ranked tiebreaker chain, and the forfeit / no-contest end
// conditions. They build small hand-made States (reusing mkState/tl/plr/plan
// from resolution_test.go) and call the resolver or the unexported victory
// helpers directly.

func containsID(ids []PlayerID, want PlayerID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

// ---- relic influence ----

func TestVictory_RelicInfluencePerRound(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("R1", 0, 0, TileRelic, "p1", 1, StructureNone),
		tl("R2", 5, 0, TileRelic, "p1", 1, StructureNone),
		tl("R3", -5, 0, TileRelic, "p1", 1, StructureNone),
		tl("N", 10, 0, TileNormal, "p2", 1, StructureNone),
	)
	resolveRound(s) // everyone holds

	if got := s.player("p1").Influence; got != 3 {
		t.Fatalf("p1 influence = %d, want 3 (one per controlled relic)", got)
	}
	if got := s.player("p2").Influence; got != 0 {
		t.Fatalf("p2 influence = %d, want 0", got)
	}
	if s.Result != nil {
		t.Fatalf("no victory expected this round, got %+v", s.Result)
	}
}

// ---- domination streaks ----

func TestVictory_DominationStreakIncrements(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("R1", 0, 0, TileRelic, "p1", 1, StructureNone),
		tl("R2", 5, 0, TileRelic, "p1", 1, StructureNone),
		tl("R3", -5, 0, TileRelic, "p1", 1, StructureNone),
		tl("N", 10, 0, TileNormal, "p2", 1, StructureNone),
	)
	s.updateDominationAndVictory()
	if got := s.player("p1").DominationStreak; got != 1 {
		t.Fatalf("streak = %d, want 1 after one round at threshold", got)
	}
	if s.pendingGameOver || s.Result != nil {
		t.Fatalf("single round at threshold must not end the match")
	}
}

func TestVictory_DominationStreakResets(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("R1", 0, 0, TileRelic, "p1", 1, StructureNone),
		tl("R2", 5, 0, TileRelic, "p1", 1, StructureNone), // only 2 relics (< threshold)
		tl("N", 10, 0, TileNormal, "p2", 1, StructureNone),
	)
	s.player("p1").DominationStreak = 5 // as if riding a streak
	s.updateDominationAndVictory()
	if got := s.player("p1").DominationStreak; got != 0 {
		t.Fatalf("streak = %d, want 0 after dropping below threshold", got)
	}
	if s.Result != nil {
		t.Fatalf("reset should not win the match, got %+v", s.Result)
	}
}

func TestVictory_DominationWin(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("R1", 0, 0, TileRelic, "p1", 1, StructureNone),
		tl("R2", 5, 0, TileRelic, "p1", 1, StructureNone),
		tl("R3", -5, 0, TileRelic, "p1", 1, StructureNone),
		tl("N", 10, 0, TileNormal, "p2", 1, StructureNone),
	)
	resolveRound(s) // round 1: streak -> 1
	if s.Result != nil {
		t.Fatalf("match ended too early after round 1")
	}
	s.Round = 2
	resolveRound(s) // round 2: streak -> 2 == DominationConsecutive -> win

	if !s.pendingGameOver {
		t.Fatalf("pendingGameOver = false, want true after domination")
	}
	if s.Result == nil {
		t.Fatalf("Result is nil after domination win")
	}
	if s.Result.Reason != VictoryDomination {
		t.Fatalf("reason = %q, want %q", s.Result.Reason, VictoryDomination)
	}
	if len(s.Result.Winners) != 1 || s.Result.Winners[0] != "p1" {
		t.Fatalf("winners = %v, want [p1]", s.Result.Winners)
	}
}

// ---- final-round victory ----

func TestVictory_FinalRoundInfluenceWinner(t *testing.T) {
	s := mkState([]Player{
		{ID: "p1", Influence: 5, FauxAvailable: true},
		{ID: "p2", Influence: 2, FauxAvailable: true},
	})
	s.Round = 1
	s.TotalRounds = 1
	s.updateDominationAndVictory()

	if s.Result == nil {
		t.Fatalf("final round did not produce a result")
	}
	if s.Result.Reason != VictoryInfluence {
		t.Fatalf("reason = %q, want %q", s.Result.Reason, VictoryInfluence)
	}
	if len(s.Result.Winners) != 1 || s.Result.Winners[0] != "p1" {
		t.Fatalf("winners = %v, want [p1]", s.Result.Winners)
	}
}

func TestVictory_SharedWin(t *testing.T) {
	s := mkState([]Player{
		{ID: "p1", Influence: 3, FauxAvailable: true},
		{ID: "p2", Influence: 3, FauxAvailable: true},
	})
	s.Round = 1
	s.TotalRounds = 1
	s.updateDominationAndVictory()

	if s.Result == nil {
		t.Fatalf("final round did not produce a result")
	}
	if s.Result.Reason != VictoryShared {
		t.Fatalf("reason = %q, want %q", s.Result.Reason, VictoryShared)
	}
	if len(s.Result.Winners) != 2 {
		t.Fatalf("winners = %v, want two shared winners", s.Result.Winners)
	}
	if !containsID(s.Result.Winners, "p1") || !containsID(s.Result.Winners, "p2") {
		t.Fatalf("winners = %v, want both p1 and p2", s.Result.Winners)
	}
}

// ---- tiebreaker chain (computeStandings) ----

func assertRanking(t *testing.T, s *State) {
	t.Helper()
	st := s.computeStandings()
	if len(st) != 2 {
		t.Fatalf("got %d standings, want 2", len(st))
	}
	if st[0].Player != "p1" || st[0].Rank != 1 {
		t.Fatalf("top standing = %+v, want p1 rank 1", st[0])
	}
	if st[1].Player != "p2" || st[1].Rank != 2 {
		t.Fatalf("second standing = %+v, want p2 rank 2", st[1])
	}
}

func TestStandings_TiebreakInfluence(t *testing.T) {
	s := mkState([]Player{
		{ID: "p1", Influence: 5},
		{ID: "p2", Influence: 3},
	})
	assertRanking(t, s)
}

func TestStandings_TiebreakRelics(t *testing.T) {
	// Equal influence/energy/territories/armies; only relic control differs.
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("R", 0, 0, TileRelic, "p1", 1, StructureNone),
		tl("N", 5, 0, TileNormal, "p2", 1, StructureNone),
	)
	assertRanking(t, s)
}

func TestStandings_TiebreakTerritories(t *testing.T) {
	// Equal influence/energy/relics/armies; only territory count differs.
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 1, StructureNone),
		tl("B", 1, 0, TileNormal, "p1", 1, StructureNone), // p1: 2 tiles, 2 armies
		tl("C", 5, 0, TileNormal, "p2", 2, StructureNone), // p2: 1 tile, 2 armies
	)
	assertRanking(t, s)
}

func TestStandings_TiebreakArmies(t *testing.T) {
	// Equal influence/energy/relics/territories; only army totals differ.
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 5, 0, TileNormal, "p2", 1, StructureNone),
	)
	assertRanking(t, s)
}

func TestStandings_TiebreakEnergy(t *testing.T) {
	// Everything equal except remaining energy.
	s := mkState([]Player{plr("p1", 5), plr("p2", 3)})
	assertRanking(t, s)
}

func TestStandings_IdenticalShareRank(t *testing.T) {
	s := mkState([]Player{
		{ID: "p1", Influence: 4, Energy: 2},
		{ID: "p2", Influence: 4, Energy: 2},
	})
	st := s.computeStandings()
	if st[0].Rank != 1 || st[1].Rank != 1 {
		t.Fatalf("identical players should share rank 1, got %d and %d", st[0].Rank, st[1].Rank)
	}
}

// ---- forfeit / no contest ----

func TestVictory_ForfeitLastStanding(t *testing.T) {
	s := mkState([]Player{
		{ID: "p1", FauxAvailable: true},
		{ID: "p2", Forfeited: true},
		{ID: "p3", Forfeited: true},
	})
	if !s.endForfeitIfAlone() {
		t.Fatalf("endForfeitIfAlone = false, want true with one player left")
	}
	if s.Result == nil || s.Result.Reason != VictoryForfeit {
		t.Fatalf("reason = %v, want %q", s.Result, VictoryForfeit)
	}
	if len(s.Result.Winners) != 1 || s.Result.Winners[0] != "p1" {
		t.Fatalf("winners = %v, want [p1]", s.Result.Winners)
	}
}

func TestVictory_NoContest(t *testing.T) {
	s := mkState([]Player{
		{ID: "p1", Forfeited: true},
		{ID: "p2", Forfeited: true},
		{ID: "p3", Forfeited: true},
	})
	if !s.endForfeitIfAlone() {
		t.Fatalf("endForfeitIfAlone = false, want true with zero players left")
	}
	if s.Result == nil || s.Result.Reason != VictoryNoContest {
		t.Fatalf("reason = %v, want %q", s.Result, VictoryNoContest)
	}
	if len(s.Result.Winners) != 0 {
		t.Fatalf("winners = %v, want empty for no contest", s.Result.Winners)
	}
}

func TestEngine_ResignToForfeitVictory(t *testing.T) {
	e := startedEngine(t, 3)
	if err := e.Resign("p2"); err != nil {
		t.Fatalf("Resign p2: %v", err)
	}
	if e.EndForfeitIfAlone() {
		t.Fatalf("match ended with two players still active")
	}
	if err := e.Resign("p3"); err != nil {
		t.Fatalf("Resign p3: %v", err)
	}
	if !e.EndForfeitIfAlone() {
		t.Fatalf("EndForfeitIfAlone = false, want true when only host remains")
	}
	s := e.State()
	if s.Phase != PhaseGameOver {
		t.Fatalf("phase = %q, want game over", s.Phase)
	}
	if s.Result == nil || s.Result.Reason != VictoryForfeit {
		t.Fatalf("reason = %v, want %q", s.Result, VictoryForfeit)
	}
	if len(s.Result.Winners) != 1 || s.Result.Winners[0] != "h" {
		t.Fatalf("winners = %v, want [h]", s.Result.Winners)
	}
}
