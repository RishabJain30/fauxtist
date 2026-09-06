package game

import (
	"reflect"
	"testing"
)

// These tests exercise the deterministic simultaneous resolver directly on
// small hand-built boards, so each combat rule is checked in isolation.

func tl(id string, q, r int, typ TileType, owner PlayerID, armies int, st Structure) *Tile {
	return &Tile{ID: TileID(id), Coord: Axial{Q: q, R: r}, Type: typ, Owner: owner, Armies: armies, Structure: st}
}

func mkState(players []Player, tiles ...*Tile) *State {
	s := &State{
		Phase:        PhaseSecretPlanning,
		Round:        1,
		TotalRounds:  8,
		Players:      players,
		Tiles:        map[TileID]*Tile{},
		Declarations: map[PlayerID]Declaration{},
		Orders:       map[PlayerID]OrderSet{},
		Stats:        map[PlayerID]*MatchStats{},
	}
	for _, t := range tiles {
		s.Tiles[t.ID] = t
	}
	for _, p := range players {
		s.Stats[p.ID] = &MatchStats{}
	}
	return s
}

func plr(id PlayerID, energy int) Player {
	return Player{ID: id, Energy: energy, FauxAvailable: true}
}

// plan sets a player's real commands: the first as a (real) declaration, the
// rest as hidden orders.
func plan(s *State, pid PlayerID, cmds ...Command) {
	if len(cmds) == 0 {
		return
	}
	s.Declarations[pid] = Declaration{Command: cmds[0], Submitted: true}
	s.Orders[pid] = OrderSet{Commands: cmds[1:], Submitted: true}
}

// planFaux sets a Faux declaration plus the hidden real commands that actually
// execute.
func planFaux(s *State, pid PlayerID, decl Command, real ...Command) {
	s.Declarations[pid] = Declaration{Command: decl, Submitted: true}
	s.Orders[pid] = OrderSet{Faux: true, Commands: real, Submitted: true}
}

func march(from, to string, armies int) Command {
	return Command{Type: CmdMarch, From: TileID(from), To: TileID(to), Armies: armies}
}

func TestResolve_FriendlyReinforcement(t *testing.T) {
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p1", 1, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	resolveRound(s)
	if s.Tiles["A"].Armies != 1 || s.Tiles["B"].Armies != 3 {
		t.Fatalf("want A=1 B=3, got A=%d B=%d", s.Tiles["A"].Armies, s.Tiles["B"].Armies)
	}
}

func TestResolve_NeutralExpansion(t *testing.T) {
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	resolveRound(s)
	if s.Tiles["B"].Owner != "p1" || s.Tiles["B"].Armies != 2 {
		t.Fatalf("want B owned p1 with 2, got owner=%q armies=%d", s.Tiles["B"].Owner, s.Tiles["B"].Armies)
	}
}

func TestResolve_EnemyCaptureUniqueHighest(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 1, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	resolveRound(s)
	if s.Tiles["B"].Owner != "p1" || s.Tiles["B"].Armies != 1 {
		t.Fatalf("want B captured by p1 with 1, got owner=%q armies=%d", s.Tiles["B"].Owner, s.Tiles["B"].Armies)
	}
}

func TestResolve_DefenderHoldsWhenWeakerAttack(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	resolveRound(s)
	if s.Tiles["B"].Owner != "p2" || s.Tiles["B"].Armies != 1 {
		t.Fatalf("want B held by p2 with 1, got owner=%q armies=%d", s.Tiles["B"].Owner, s.Tiles["B"].Armies)
	}
}

func TestResolve_DefenderTieHolds(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 2, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2)) // attacker 2 == defender 2
	resolveRound(s)
	if s.Tiles["B"].Owner != "p2" {
		t.Fatalf("defender should hold on a tie, got owner=%q", s.Tiles["B"].Owner)
	}
}

func TestResolve_AttackerTieAllFail(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("B", 0, 0, TileNormal, "", 0, StructureNone),
		tl("A1", 1, 0, TileNormal, "p1", 3, StructureNone),
		tl("A2", -1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s, "p1", march("A1", "B", 2))
	plan(s, "p2", march("A2", "B", 2))
	resolveRound(s)
	if s.Tiles["B"].Owner != "" || s.Tiles["B"].Armies != 0 {
		t.Fatalf("tie over neutral should stay neutral empty, got owner=%q armies=%d", s.Tiles["B"].Owner, s.Tiles["B"].Armies)
	}
}

func TestResolve_ThreePlayersOneTile(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0), plr("p3", 0)},
		tl("B", 0, 0, TileNormal, "p3", 1, StructureNone),
		tl("A1", 1, 0, TileNormal, "p1", 4, StructureNone),
		tl("A2", -1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s, "p1", march("A1", "B", 3)) // unique highest 3
	plan(s, "p2", march("A2", "B", 2)) // second highest 2
	resolveRound(s)
	// p1 wins: survivors = max(1, 3 - max(defEff=1, p2=2)) = max(1, 3-2) = 1
	if s.Tiles["B"].Owner != "p1" || s.Tiles["B"].Armies != 1 {
		t.Fatalf("want B captured by p1 with 1, got owner=%q armies=%d", s.Tiles["B"].Owner, s.Tiles["B"].Armies)
	}
}

func TestResolve_NeutralRelicGuardian(t *testing.T) {
	// A weak attack (1) ties the guardian (1) → relic stays neutral.
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("R", 1, 0, TileRelic, "", NeutralRelicGuardian, StructureNone),
	)
	plan(s, "p1", march("A", "R", 1))
	resolveRound(s)
	if s.Tiles["R"].Owner != "" {
		t.Fatalf("guardian should hold a tie, got owner=%q", s.Tiles["R"].Owner)
	}
	// A stronger attack (2 > 1) captures the relic and awards influence.
	s2 := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("R", 1, 0, TileRelic, "", NeutralRelicGuardian, StructureNone),
	)
	plan(s2, "p1", march("A", "R", 2))
	resolveRound(s2)
	if s2.Tiles["R"].Owner != "p1" || s2.Tiles["R"].Armies != 1 {
		t.Fatalf("want relic captured by p1 with 1, got owner=%q armies=%d", s2.Tiles["R"].Owner, s2.Tiles["R"].Armies)
	}
	if p := s2.player("p1"); p.Influence != 1 {
		t.Fatalf("want p1 influence 1 from controlled relic, got %d", p.Influence)
	}
}

func TestResolve_FortressPlusFortify(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 10)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 2, StructureFortress),
	)
	plan(s, "p1", march("A", "B", 3))                 // attacker 3
	plan(s, "p2", Command{Type: CmdFortify, To: "B"}) // def eff = 2 + fortress 1 + fortify 2 = 5
	resolveRound(s)
	if s.Tiles["B"].Owner != "p2" {
		t.Fatalf("fortress+fortify should hold vs 3, got owner=%q", s.Tiles["B"].Owner)
	}
}

func TestResolve_ReciprocalMarchesExchange(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	plan(s, "p2", march("B", "A", 2))
	resolveRound(s)
	// Each origin drops to 1 (outgoing removed), each incoming 2 > 1 → capture.
	if s.Tiles["A"].Owner != "p2" || s.Tiles["B"].Owner != "p1" {
		t.Fatalf("want exchange A->p2 B->p1, got A=%q B=%q", s.Tiles["A"].Owner, s.Tiles["B"].Owner)
	}
}

func TestResolve_DepartedArmiesExecuteEvenIfOriginCaptured(t *testing.T) {
	// A(p1,2) marches 1 to B; C(p2,3) marches 2 into A. A is captured, but
	// p1's 1 army still reaches B (and loses to B's garrison).
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 2, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 3, StructureNone),
		tl("C", -1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s, "p1", march("A", "B", 1))
	plan(s, "p2", march("C", "A", 2))
	resolveRound(s)
	if s.Tiles["A"].Owner != "p2" {
		t.Fatalf("A should be captured by p2, got %q", s.Tiles["A"].Owner)
	}
	if s.Tiles["B"].Owner != "p2" {
		t.Fatalf("B should still be p2's (weak attack failed), got %q", s.Tiles["B"].Owner)
	}
}

func TestResolve_FriendlyReinforcementIntoAttackedTile(t *testing.T) {
	// B(p1,1) is attacked by p2 with 2, but p1 reinforces with 2 from A →
	// defender 3 holds.
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("B", 0, 0, TileNormal, "p1", 1, StructureNone),
		tl("A", 1, 0, TileNormal, "p1", 3, StructureNone),
		tl("C", -1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	plan(s, "p2", march("C", "B", 2))
	resolveRound(s)
	if s.Tiles["B"].Owner != "p1" {
		t.Fatalf("reinforced B should hold, got owner=%q", s.Tiles["B"].Owner)
	}
}

func TestResolve_FauxMarchArmiesReusable(t *testing.T) {
	// p1 publicly declares march A->B (Faux), but really marches A->C.
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
		tl("C", -1, 0, TileNormal, "", 0, StructureNone),
	)
	planFaux(s, "p1", march("A", "B", 2), march("A", "C", 2))
	resolveRound(s)
	if s.Tiles["B"].Owner != "" {
		t.Fatalf("faux declaration must not execute; B should stay neutral, got %q", s.Tiles["B"].Owner)
	}
	if s.Tiles["C"].Owner != "p1" {
		t.Fatalf("real hidden march should capture C, got %q", s.Tiles["C"].Owner)
	}
	if p := s.player("p1"); p.FauxAvailable || p.FauxUsedRound != 1 {
		t.Fatalf("faux should be spent this round, got available=%v usedRound=%d", p.FauxAvailable, p.FauxUsedRound)
	}
}

func TestResolve_BuildSuccessAndRefund(t *testing.T) {
	// Success: p1 builds a fortress on an unattacked owned tile.
	s := mkState([]Player{plr("p1", 10)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
	)
	plan(s, "p1", Command{Type: CmdBuildFortress, To: "A"})
	resolveRound(s)
	if s.Tiles["A"].Structure != StructureFortress {
		t.Fatalf("want fortress built, got %q", s.Tiles["A"].Structure)
	}
	if p := s.player("p1"); p.Energy != 7 {
		t.Fatalf("want energy 10-3=7, got %d", p.Energy)
	}

	// Refund: the build target is captured before the build resolves.
	s2 := mkState([]Player{plr("p1", 10), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 1, StructureNone),
		tl("C", -1, 0, TileNormal, "p2", 3, StructureNone),
	)
	plan(s2, "p1", Command{Type: CmdBuildFortress, To: "A"})
	plan(s2, "p2", march("C", "A", 2))
	resolveRound(s2)
	if s2.Tiles["A"].Owner != "p2" {
		t.Fatalf("A should be captured, got %q", s2.Tiles["A"].Owner)
	}
	if s2.Tiles["A"].Structure != StructureNone {
		t.Fatalf("failed build must not leave a structure, got %q", s2.Tiles["A"].Structure)
	}
	if p := s2.player("p1"); p.Energy != 10 {
		t.Fatalf("failed build must refund fully (want 10), got %d", p.Energy)
	}
}

func TestResolve_RecruitDefendsButCannotBeOutmarched(t *testing.T) {
	// Recruit adds 2 defenders to the capital; a simultaneous attack of 3
	// meets 3(garrison)+2(recruit)=5 → holds.
	s := mkState([]Player{plr("p1", 10), plr("p2", 0)},
		tl("CAP", 0, 0, TileCapital, "p1", 3, StructureNone),
		tl("E", 1, 0, TileNormal, "p2", 4, StructureNone),
	)
	s.Tiles["CAP"].CapitalOwner = "p1"
	// Enemy capital can't be targeted; make CAP a normal tile scenario instead.
	s.Tiles["CAP"].Type = TileNormal
	s.Tiles["CAP"].CapitalOwner = ""
	plan(s, "p1", Command{Type: CmdRecruit, To: "CAP"}) // recruit needs capital/fortress
	// Give it a fortress so recruit is legal on a normal tile.
	s.Tiles["CAP"].Structure = StructureFortress
	plan(s, "p2", march("E", "CAP", 3))
	resolveRound(s)
	if s.Tiles["CAP"].Owner != "p1" {
		t.Fatalf("recruited defenders should hold, got owner=%q armies=%d", s.Tiles["CAP"].Owner, s.Tiles["CAP"].Armies)
	}
}

func TestResolve_DeterministicRegardlessOfInsertionOrder(t *testing.T) {
	build := func(order int) *State {
		var s *State
		p1 := plr("p1", 0)
		p2 := plr("p2", 0)
		A := tl("A", 0, 0, TileNormal, "p1", 3, StructureNone)
		B := tl("B", 1, 0, TileNormal, "p2", 3, StructureNone)
		C := tl("C", 2, 0, TileNormal, "p2", 2, StructureNone)
		if order == 0 {
			s = mkState([]Player{p1, p2}, A, B, C)
			plan(s, "p1", march("A", "B", 2))
			plan(s, "p2", march("B", "C", 1))
		} else {
			s = mkState([]Player{p2, p1}, C, B, A)
			plan(s, "p2", march("B", "C", 1))
			plan(s, "p1", march("A", "B", 2))
		}
		return s
	}
	a := build(0)
	b := build(1)
	resolveRound(a)
	resolveRound(b)
	for _, id := range []TileID{"A", "B", "C"} {
		if a.Tiles[id].Owner != b.Tiles[id].Owner || a.Tiles[id].Armies != b.Tiles[id].Armies {
			t.Fatalf("nondeterministic at %s: a(%q,%d) != b(%q,%d)", id,
				a.Tiles[id].Owner, a.Tiles[id].Armies, b.Tiles[id].Owner, b.Tiles[id].Armies)
		}
	}
}

func TestResolve_NoNegativeValues(t *testing.T) {
	s := mkState([]Player{plr("p1", 3), plr("p2", 3), plr("p3", 3)},
		tl("B", 0, 0, TileNormal, "p3", 1, StructureNone),
		tl("A1", 1, 0, TileNormal, "p1", 2, StructureNone),
		tl("A2", -1, 0, TileNormal, "p2", 2, StructureNone),
	)
	plan(s, "p1", march("A1", "B", 1))
	plan(s, "p2", march("A2", "B", 1))
	resolveRound(s)
	for _, tid := range s.SortedTileIDs() {
		if s.Tiles[tid].Armies < 0 {
			t.Fatalf("tile %s has negative armies %d", tid, s.Tiles[tid].Armies)
		}
	}
	for _, p := range s.Players {
		if p.Energy < 0 {
			t.Fatalf("player %s has negative energy %d", p.ID, p.Energy)
		}
	}
}

func TestResolve_FramesAreProduced(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 1, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	res := resolveRound(s)
	var haveMarch, haveBattle, haveCapture bool
	for _, f := range res.Frames {
		switch f.Kind {
		case FrameMarch:
			haveMarch = true
		case FrameBattle:
			haveBattle = true
		case FrameCapture:
			haveCapture = true
		}
	}
	if !haveMarch || !haveBattle || !haveCapture {
		t.Fatalf("expected march+battle+capture frames, got %v", res.Frames)
	}
	if len(res.Board) != len(s.Tiles) {
		t.Fatalf("resolution board should carry every tile, got %d want %d", len(res.Board), len(s.Tiles))
	}
}

func TestResolve_MissingOrdersBecomeHold(t *testing.T) {
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
	)
	// No declaration, no orders at all.
	res := resolveRound(s)
	if s.Tiles["A"].Armies != 3 {
		t.Fatalf("a player who did nothing should leave the board unchanged, got A=%d", s.Tiles["A"].Armies)
	}
	if res.Round != 1 {
		t.Fatalf("want round 1, got %d", res.Round)
	}
}

// sanity: the resolution frame slice never aliases into engine state after a
// clone.
func TestResolve_CloneIsolation(t *testing.T) {
	s := mkState([]Player{plr("p1", 0), plr("p2", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p2", 1, StructureNone),
	)
	plan(s, "p1", march("A", "B", 2))
	resolveRound(s)
	c := s.clone()
	c.Tiles["B"].Armies = 999
	if s.Tiles["B"].Armies == 999 {
		t.Fatal("clone shares tile pointers with the engine state")
	}
	if !reflect.DeepEqual(c.Resolution.Round, s.Resolution.Round) {
		t.Fatal("clone lost resolution round")
	}
}
