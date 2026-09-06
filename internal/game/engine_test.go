package game

import "testing"

// These tests drive the Engine through its authoritative phase machine to
// verify lifecycle, economy, Faux, lock/unlock, and rematch behaviour. They use
// only the public engine API where possible; a few reach into the in-package
// State to set a precondition (energy, Faux availability) that would otherwise
// take a whole round to reach.

// ---- shared drivers (used by engine_test.go and orders_test.go) ----

var extraIDs = []PlayerID{"p2", "p3", "p4", "p5", "p6"}

// startedEngine returns an engine with n players (host "h" plus n-1 others)
// that has just started a match, so it sits in round-1 INCOME.
func startedEngine(t *testing.T, n int) *Engine {
	t.Helper()
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 1)
	for i := 0; i < n-1; i++ {
		if err := e.UpsertPlayer(Player{ID: extraIDs[i], Name: string(extraIDs[i]), Emoji: "🐙"}); err != nil {
			t.Fatalf("UpsertPlayer(%s): %v", extraIDs[i], err)
		}
	}
	if err := e.StartMatch("h"); err != nil {
		t.Fatalf("StartMatch: %v", err)
	}
	if e.Phase() != PhaseIncome {
		t.Fatalf("after StartMatch phase = %q, want %q", e.Phase(), PhaseIncome)
	}
	return e
}

// toDeclaration advances a round from INCOME to DECLARATION.
func toDeclaration(t *testing.T, e *Engine) {
	t.Helper()
	if err := e.ApplyIncome(); err != nil {
		t.Fatalf("ApplyIncome: %v", err)
	}
	if err := e.BeginDeclaration(); err != nil {
		t.Fatalf("BeginDeclaration: %v", err)
	}
}

// toSecretPlanning advances a round from INCOME to SECRET_PLANNING, filling
// declarations with auto-Holds.
func toSecretPlanning(t *testing.T, e *Engine) {
	t.Helper()
	toDeclaration(t, e)
	if err := e.RevealDeclarations(); err != nil {
		t.Fatalf("RevealDeclarations: %v", err)
	}
	if err := e.BeginPlanning(); err != nil {
		t.Fatalf("BeginPlanning: %v", err)
	}
}

// finishRound resolves the current SECRET_PLANNING round and advances to the
// next round's INCOME (or GAME_OVER).
func finishRound(t *testing.T, e *Engine) {
	t.Helper()
	if _, err := e.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if err := e.BeginRoundSummary(); err != nil {
		t.Fatalf("BeginRoundSummary: %v", err)
	}
	if err := e.AdvanceRound(); err != nil {
		t.Fatalf("AdvanceRound: %v", err)
	}
}

// playIdleRound plays a whole round in which nobody submits anything.
func playIdleRound(t *testing.T, e *Engine) {
	t.Helper()
	toSecretPlanning(t, e)
	finishRound(t, e)
}

// driveToGameOver plays idle rounds until the match ends.
func driveToGameOver(t *testing.T, e *Engine) {
	t.Helper()
	for guard := 0; e.Phase() == PhaseIncome; guard++ {
		if guard > 20 {
			t.Fatalf("match did not end after 20 rounds")
		}
		playIdleRound(t, e)
	}
	if e.Phase() != PhaseGameOver {
		t.Fatalf("expected GAME_OVER, got %q", e.Phase())
	}
}

func capitalOf(t *testing.T, s State, pid PlayerID) TileID {
	t.Helper()
	for _, tid := range s.SortedTileIDs() {
		tile := s.Tiles[tid]
		if tile.Type == TileCapital && tile.CapitalOwner == pid {
			return tid
		}
	}
	t.Fatalf("no capital for player %q", pid)
	return ""
}

func nonCapitalOf(t *testing.T, s State, pid PlayerID) TileID {
	t.Helper()
	for _, tid := range s.SortedTileIDs() {
		tile := s.Tiles[tid]
		if tile.Owner == pid && tile.Type != TileCapital {
			return tid
		}
	}
	t.Fatalf("no owned non-capital tile for player %q", pid)
	return ""
}

// ---- phase machine ----

func TestEngine_StartMatchRequiresHost(t *testing.T) {
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 1)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	_ = e.UpsertPlayer(Player{ID: "p3"})
	if err := e.StartMatch("p2"); err != ErrNotHost {
		t.Fatalf("StartMatch by non-host = %v, want ErrNotHost", err)
	}
	if e.Phase() != PhaseLobby {
		t.Fatalf("phase = %q, want lobby after rejected start", e.Phase())
	}
}

func TestEngine_StartMatchTooFewPlayers(t *testing.T) {
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 1)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	if err := e.StartMatch("h"); err != ErrTooFewPlayers {
		t.Fatalf("StartMatch with 2 players = %v, want ErrTooFewPlayers", err)
	}
}

func TestEngine_WrongPhaseCalls(t *testing.T) {
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 1)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	_ = e.UpsertPlayer(Player{ID: "p3"})

	// Before StartMatch (lobby) the round transitions are illegal.
	if err := e.ApplyIncome(); err != ErrWrongPhase {
		t.Fatalf("ApplyIncome in lobby = %v, want ErrWrongPhase", err)
	}
	if _, err := e.Resolve(); err != ErrWrongPhase {
		t.Fatalf("Resolve in lobby = %v, want ErrWrongPhase", err)
	}
	if err := e.BeginDeclaration(); err != ErrWrongPhase {
		t.Fatalf("BeginDeclaration in lobby = %v, want ErrWrongPhase", err)
	}

	// After StartMatch we are in INCOME; Resolve still needs SECRET_PLANNING.
	if err := e.StartMatch("h"); err != nil {
		t.Fatalf("StartMatch: %v", err)
	}
	if _, err := e.Resolve(); err != ErrWrongPhase {
		t.Fatalf("Resolve in income = %v, want ErrWrongPhase", err)
	}
	if err := e.BeginPlanning(); err != ErrWrongPhase {
		t.Fatalf("BeginPlanning in income = %v, want ErrWrongPhase", err)
	}
}

// ---- starting state ----

func TestEngine_StartingState(t *testing.T) {
	for _, n := range []int{3, 4, 5, 6} {
		n := n
		t.Run(mapName(n), func(t *testing.T) {
			e := startedEngine(t, n)
			s := e.State()

			if s.Round != 1 {
				t.Fatalf("round = %d, want 1", s.Round)
			}
			if s.MapID == "" {
				t.Fatalf("MapID not set")
			}

			slots := map[int]bool{}
			factions := map[FactionID]bool{}
			for _, p := range s.Players {
				if p.Energy != StartingEnergy {
					t.Fatalf("%s energy = %d, want %d", p.ID, p.Energy, StartingEnergy)
				}
				if p.Influence != 0 {
					t.Fatalf("%s influence = %d, want 0", p.ID, p.Influence)
				}
				if !p.FauxAvailable {
					t.Fatalf("%s FauxAvailable = false, want true", p.ID)
				}
				if p.DominationStreak != 0 {
					t.Fatalf("%s DominationStreak = %d, want 0", p.ID, p.DominationStreak)
				}
				if slots[p.SpawnSlot] {
					t.Fatalf("duplicate spawn slot %d", p.SpawnSlot)
				}
				slots[p.SpawnSlot] = true
				if p.Faction != FactionOrder[p.SpawnSlot] {
					t.Fatalf("%s faction = %q, want %q for slot %d", p.ID, p.Faction, FactionOrder[p.SpawnSlot], p.SpawnSlot)
				}
				if factions[p.Faction] {
					t.Fatalf("duplicate faction %q", p.Faction)
				}
				factions[p.Faction] = true

				// Exactly one owned capital (3 armies) + one adjacent owned
				// non-capital territory (2 armies).
				var capID, terrID TileID
				capCount, terrCount := 0, 0
				for _, tid := range s.SortedTileIDs() {
					tile := s.Tiles[tid]
					if tile.Owner != p.ID {
						continue
					}
					if tile.Type == TileCapital {
						capCount++
						capID = tid
					} else {
						terrCount++
						terrID = tid
					}
				}
				if capCount != 1 {
					t.Fatalf("%s owns %d capitals, want 1", p.ID, capCount)
				}
				if terrCount != 1 {
					t.Fatalf("%s owns %d non-capital territories, want 1", p.ID, terrCount)
				}
				if s.Tiles[capID].Armies != StartingCapitalArmies {
					t.Fatalf("%s capital armies = %d, want %d", p.ID, s.Tiles[capID].Armies, StartingCapitalArmies)
				}
				if s.Tiles[terrID].Type != TileNormal {
					t.Fatalf("%s territory type = %q, want normal", p.ID, s.Tiles[terrID].Type)
				}
				if s.Tiles[terrID].Armies != StartingAdjacentArmies {
					t.Fatalf("%s territory armies = %d, want %d", p.ID, s.Tiles[terrID].Armies, StartingAdjacentArmies)
				}
				if !hexAdjacent(s.Tiles[capID].Coord, s.Tiles[terrID].Coord) {
					t.Fatalf("%s territory not adjacent to capital", p.ID)
				}
			}

			// Board-wide counts.
			relics, mines := 0, 0
			for _, tid := range s.SortedTileIDs() {
				switch s.Tiles[tid].Type {
				case TileRelic:
					relics++
				case TileMineSite:
					mines++
				}
			}
			if relics != RelicCount {
				t.Fatalf("board relics = %d, want %d", relics, RelicCount)
			}
			if mines != n+1 {
				t.Fatalf("board mine sites = %d, want %d", mines, n+1)
			}
		})
	}
}

// ---- income ----

func TestEngine_IncomeRoundOneGrantsNothing(t *testing.T) {
	e := startedEngine(t, 3)
	before := map[PlayerID]int{}
	for _, p := range e.State().Players {
		before[p.ID] = p.Energy
	}
	if err := e.ApplyIncome(); err != nil {
		t.Fatalf("ApplyIncome: %v", err)
	}
	for _, p := range e.State().Players {
		if p.Energy != before[p.ID] {
			t.Fatalf("%s energy changed on round-1 income: %d -> %d", p.ID, before[p.ID], p.Energy)
		}
	}
}

func TestEngine_IncomeFromRoundTwoWithMineAndCap(t *testing.T) {
	e := startedEngine(t, 3)
	playIdleRound(t, e) // round 1 -> round 2 INCOME
	if e.Phase() != PhaseIncome || e.State().Round != 2 {
		t.Fatalf("expected round-2 INCOME, got phase %q round %d", e.Phase(), e.State().Round)
	}

	// Reach into the state to set up three distinct income scenarios.
	hMine := nonCapitalOf(t, e.State(), "h")
	e.state.Tiles[hMine].Structure = StructureMine
	e.state.player("h").Energy = 0   // base(3) + 1 mine = 4
	e.state.player("p2").Energy = 11 // 11 + 3 = 14, capped to 12
	e.state.player("p3").Energy = 4  // 4 + 3 = 7

	if err := e.ApplyIncome(); err != nil {
		t.Fatalf("ApplyIncome: %v", err)
	}
	s := e.State()
	if got := s.player("h").Energy; got != BaseIncome+MineEnergy {
		t.Fatalf("h energy = %d, want %d (base + 1 mine)", got, BaseIncome+MineEnergy)
	}
	if got := s.player("p2").Energy; got != EnergyCap {
		t.Fatalf("p2 energy = %d, want %d (capped)", got, EnergyCap)
	}
	if got := s.player("p3").Energy; got != 4+BaseIncome {
		t.Fatalf("p3 energy = %d, want %d", got, 4+BaseIncome)
	}
}

// ---- declarations / auto-hold ----

func TestEngine_MissingDeclarationBecomesAutoHold(t *testing.T) {
	e := startedEngine(t, 3)
	toDeclaration(t, e)
	// p2 submits nothing.
	if err := e.RevealDeclarations(); err != nil {
		t.Fatalf("RevealDeclarations: %v", err)
	}
	decl, ok := e.State().Declarations["p2"]
	if !ok {
		t.Fatalf("no auto-declaration filled for p2")
	}
	if decl.Submitted {
		t.Fatalf("auto-Hold declaration should have Submitted=false")
	}
	if decl.Command.Type != CmdHold {
		t.Fatalf("auto declaration = %q, want hold", decl.Command.Type)
	}
	if err := e.BeginPlanning(); err != nil {
		t.Fatalf("BeginPlanning: %v", err)
	}
	if err := e.SetOrders("p2", nil, true); err != ErrFauxOnHold {
		t.Fatalf("Faux on auto-Hold = %v, want ErrFauxOnHold", err)
	}
}

// ---- faux ----

func TestEngine_FauxDeclarationConsumesNoEnergy(t *testing.T) {
	e := startedEngine(t, 3)
	toDeclaration(t, e)
	cap := capitalOf(t, e.State(), "p2")
	if err := e.SubmitDeclaration("p2", Command{Type: CmdRecruit, To: cap}); err != nil {
		t.Fatalf("SubmitDeclaration recruit: %v", err)
	}
	if err := e.RevealDeclarations(); err != nil {
		t.Fatalf("RevealDeclarations: %v", err)
	}
	if err := e.BeginPlanning(); err != nil {
		t.Fatalf("BeginPlanning: %v", err)
	}
	// Declare the (costly) recruit as Faux with no hidden real commands.
	if err := e.SetOrders("p2", nil, true); err != nil {
		t.Fatalf("SetOrders faux: %v", err)
	}
	if _, err := e.Resolve(); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	s := e.State()
	p := s.player("p2")
	if p.Energy != StartingEnergy {
		t.Fatalf("faux recruit declaration changed energy: %d, want %d", p.Energy, StartingEnergy)
	}
	if p.FauxAvailable {
		t.Fatalf("Faux should be spent, FauxAvailable still true")
	}
	if p.FauxUsedRound != 1 {
		t.Fatalf("FauxUsedRound = %d, want 1", p.FauxUsedRound)
	}
}

func TestEngine_FauxUnavailableAfterUse(t *testing.T) {
	e := startedEngine(t, 3)

	// Round 1: p2 spends Faux.
	toDeclaration(t, e)
	cap := capitalOf(t, e.State(), "p2")
	if err := e.SubmitDeclaration("p2", Command{Type: CmdRecruit, To: cap}); err != nil {
		t.Fatalf("round1 SubmitDeclaration: %v", err)
	}
	if err := e.RevealDeclarations(); err != nil {
		t.Fatalf("round1 RevealDeclarations: %v", err)
	}
	if err := e.BeginPlanning(); err != nil {
		t.Fatalf("round1 BeginPlanning: %v", err)
	}
	if err := e.SetOrders("p2", nil, true); err != nil {
		t.Fatalf("round1 SetOrders faux: %v", err)
	}
	finishRound(t, e)

	// Round 2: a second Faux attempt is rejected.
	toDeclaration(t, e)
	cap = capitalOf(t, e.State(), "p2")
	if err := e.SubmitDeclaration("p2", Command{Type: CmdRecruit, To: cap}); err != nil {
		t.Fatalf("round2 SubmitDeclaration: %v", err)
	}
	if err := e.RevealDeclarations(); err != nil {
		t.Fatalf("round2 RevealDeclarations: %v", err)
	}
	if err := e.BeginPlanning(); err != nil {
		t.Fatalf("round2 BeginPlanning: %v", err)
	}
	if err := e.SetOrders("p2", nil, true); err != ErrFauxUnavailable {
		t.Fatalf("round2 SetOrders faux = %v, want ErrFauxUnavailable", err)
	}
}

// ---- lock / unlock ----

func TestEngine_LockUnlockOrders(t *testing.T) {
	e := startedEngine(t, 3)
	toSecretPlanning(t, e)

	if err := e.LockOrders("p2"); err != nil {
		t.Fatalf("LockOrders: %v", err)
	}
	if err := e.SetOrders("p2", nil, false); err != ErrAlreadyLocked {
		t.Fatalf("SetOrders after lock = %v, want ErrAlreadyLocked", err)
	}
	if err := e.UnlockOrders("p2"); err != nil {
		t.Fatalf("UnlockOrders: %v", err)
	}
	if err := e.SetOrders("p2", nil, false); err != nil {
		t.Fatalf("SetOrders after unlock = %v, want nil", err)
	}
}

// ---- full match / rematch ----

func TestEngine_FullQuickMatchReachesGameOver(t *testing.T) {
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 7)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	_ = e.UpsertPlayer(Player{ID: "p3"})
	if err := e.SetPreset("h", PresetQuick); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if err := e.StartMatch("h"); err != nil {
		t.Fatalf("StartMatch: %v", err)
	}
	if e.State().TotalRounds != PresetConfigFor(PresetQuick).Rounds {
		t.Fatalf("TotalRounds = %d, want %d", e.State().TotalRounds, PresetConfigFor(PresetQuick).Rounds)
	}

	driveToGameOver(t, e)
	if e.State().Result == nil {
		t.Fatalf("Result is nil after game over")
	}
}

func TestEngine_RematchResetsMatchState(t *testing.T) {
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 7)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	_ = e.UpsertPlayer(Player{ID: "p3"})
	_ = e.SetPreset("h", PresetQuick)
	_ = e.StartMatch("h")
	driveToGameOver(t, e)

	oldMap := e.State().MapID
	if err := e.StartRematch("h", 99); err != nil {
		t.Fatalf("StartRematch: %v", err)
	}
	s := e.State()
	if s.Phase != PhaseIncome || s.Round != 1 {
		t.Fatalf("after rematch phase=%q round=%d, want income/1", s.Phase, s.Round)
	}
	if s.Result != nil {
		t.Fatalf("rematch did not clear Result")
	}
	if s.Resolution != nil {
		t.Fatalf("rematch did not clear Resolution")
	}
	if len(s.Tiles) == 0 || s.MapID == "" {
		t.Fatalf("rematch did not build a board")
	}
	_ = oldMap // seed differs; board content may or may not differ, only freshness is asserted
	for _, p := range s.Players {
		if p.Energy != StartingEnergy || p.Influence != 0 || !p.FauxAvailable || p.DominationStreak != 0 {
			t.Fatalf("%s not reset: energy=%d influence=%d faux=%v streak=%d",
				p.ID, p.Energy, p.Influence, p.FauxAvailable, p.DominationStreak)
		}
	}
}

func TestEngine_ReturnToLobby(t *testing.T) {
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 7)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	_ = e.UpsertPlayer(Player{ID: "p3"})
	_ = e.SetPreset("h", PresetQuick)
	_ = e.StartMatch("h")
	driveToGameOver(t, e)

	if err := e.ReturnToLobby("h"); err != nil {
		t.Fatalf("ReturnToLobby: %v", err)
	}
	s := e.State()
	if s.Phase != PhaseLobby {
		t.Fatalf("phase = %q, want lobby", s.Phase)
	}
	if s.Round != 0 {
		t.Fatalf("round = %d, want 0", s.Round)
	}
	if len(s.Tiles) != 0 || s.Result != nil {
		t.Fatalf("lobby should have no board and no result")
	}
	if len(s.Players) != 3 {
		t.Fatalf("roster = %d, want 3 preserved", len(s.Players))
	}
}

func TestEngine_RematchDropsForfeitedPlayers(t *testing.T) {
	// 4 players so that dropping one forfeited player still leaves 3 for setup.
	e := NewEngine(Player{ID: "h", Name: "Host", Emoji: "🦊"}, 7)
	_ = e.UpsertPlayer(Player{ID: "p2"})
	_ = e.UpsertPlayer(Player{ID: "p3"})
	_ = e.UpsertPlayer(Player{ID: "p4"})
	_ = e.SetPreset("h", PresetQuick)
	_ = e.StartMatch("h")

	if err := e.Resign("p4"); err != nil {
		t.Fatalf("Resign: %v", err)
	}
	driveToGameOver(t, e)

	if err := e.StartRematch("h", 123); err != nil {
		t.Fatalf("StartRematch: %v", err)
	}
	for _, p := range e.State().Players {
		if p.ID == "p4" {
			t.Fatalf("forfeited player p4 should have been dropped on rematch")
		}
	}
	if len(e.State().Players) != 3 {
		t.Fatalf("roster = %d, want 3 after dropping forfeited", len(e.State().Players))
	}
}
