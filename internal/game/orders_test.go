package game

import "testing"

// These tests exercise the server-side command validator (validateSingleCommand
// / validateRealCommands) on hand-built boards, plus the small pure helpers on
// Command / OrderSet, and a few engine-level submission paths (SubmitDeclaration
// and SetOrders) for the Faux and command-count rules. Combat resolution itself
// is covered by resolution_test.go and is not repeated here.

// ---- single-command structural validation ----

func TestValidateMarch_Accepted(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "p1", 1, StructureNone),
	)
	if err := s.validateSingleCommand("p1", march("A", "B", 2)); err != nil {
		t.Fatalf("valid march = %v, want nil", err)
	}
}

func TestValidateMarch_NotAdjacent(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 2, 0, TileNormal, "p1", 1, StructureNone), // distance 2
	)
	if err := s.validateSingleCommand("p1", march("A", "B", 1)); err != ErrNotAdjacent {
		t.Fatalf("non-adjacent march = %v, want ErrNotAdjacent", err)
	}
}

func TestValidateMarch_NotOwned(t *testing.T) {
	s := mkState([]Player{plr("p1", 4), plr("p2", 4)},
		tl("A", 0, 0, TileNormal, "p2", 3, StructureNone), // owned by p2
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
	)
	if err := s.validateSingleCommand("p1", march("A", "B", 1)); err != ErrNotOwned {
		t.Fatalf("march from unowned tile = %v, want ErrNotOwned", err)
	}
}

func TestValidateMarch_NotEnoughArmies(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 1, StructureNone), // only 1 army
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
	)
	if err := s.validateSingleCommand("p1", march("A", "B", 1)); err != ErrNotEnoughArmies {
		t.Fatalf("march leaving <1 army = %v, want ErrNotEnoughArmies", err)
	}
}

func TestValidateMarch_BadArmyCount(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
	)
	if err := s.validateSingleCommand("p1", march("A", "B", 4)); err != ErrBadArmyCount {
		t.Fatalf("march of 4 armies = %v, want ErrBadArmyCount", err)
	}
	if err := s.validateSingleCommand("p1", march("A", "B", 0)); err != ErrBadArmyCount {
		t.Fatalf("march of 0 armies = %v, want ErrBadArmyCount", err)
	}
}

func TestValidateMarch_EnemyCapitalTargeted(t *testing.T) {
	s := mkState([]Player{plr("p1", 4), plr("p2", 4)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("CAP", 1, 0, TileCapital, "p2", 3, StructureNone),
	)
	s.Tiles["CAP"].CapitalOwner = "p2"
	if err := s.validateSingleCommand("p1", march("A", "CAP", 2)); err != ErrCapitalTargeted {
		t.Fatalf("march into enemy capital = %v, want ErrCapitalTargeted", err)
	}
}

func TestValidateMarch_UnknownTile(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
	)
	if err := s.validateSingleCommand("p1", march("A", "GHOST", 1)); err != ErrUnknownTile {
		t.Fatalf("march to unknown tile = %v, want ErrUnknownTile", err)
	}
}

// ---- recruit / fortify / build target rules ----

func TestValidateRecruit_TargetRules(t *testing.T) {
	s := mkState([]Player{plr("p1", 6)},
		tl("CAP", 0, 0, TileCapital, "p1", 3, StructureNone),
		tl("FORT", 1, 0, TileNormal, "p1", 1, StructureFortress),
		tl("N", 2, 0, TileNormal, "p1", 1, StructureNone),
	)
	s.Tiles["CAP"].CapitalOwner = "p1"
	if err := s.validateSingleCommand("p1", Command{Type: CmdRecruit, To: "CAP"}); err != nil {
		t.Fatalf("recruit on own capital = %v, want nil", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdRecruit, To: "FORT"}); err != nil {
		t.Fatalf("recruit on own fortress = %v, want nil", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdRecruit, To: "N"}); err != ErrBadTarget {
		t.Fatalf("recruit on plain tile = %v, want ErrBadTarget", err)
	}
}

func TestValidateFortify_TargetRules(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("CAP", 0, 0, TileCapital, "p1", 3, StructureNone),
		tl("N", 1, 0, TileNormal, "p1", 1, StructureNone),
	)
	s.Tiles["CAP"].CapitalOwner = "p1"
	if err := s.validateSingleCommand("p1", Command{Type: CmdFortify, To: "N"}); err != nil {
		t.Fatalf("fortify own non-capital = %v, want nil", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdFortify, To: "CAP"}); err != ErrBadTarget {
		t.Fatalf("fortify own capital = %v, want ErrBadTarget", err)
	}
}

func TestValidateBuildFortress_TargetRules(t *testing.T) {
	s := mkState([]Player{plr("p1", 10)},
		tl("N", 0, 0, TileNormal, "p1", 1, StructureNone),
		tl("MINE", 1, 0, TileMineSite, "p1", 1, StructureNone),
		tl("HAS", 2, 0, TileNormal, "p1", 1, StructureFortress),
	)
	if err := s.validateSingleCommand("p1", Command{Type: CmdBuildFortress, To: "N"}); err != nil {
		t.Fatalf("fortress on normal = %v, want nil", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdBuildFortress, To: "MINE"}); err != nil {
		t.Fatalf("fortress on unused mine site = %v, want nil", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdBuildFortress, To: "HAS"}); err != ErrBadTarget {
		t.Fatalf("fortress on tile with structure = %v, want ErrBadTarget", err)
	}
}

func TestValidateBuildMine_TargetRules(t *testing.T) {
	s := mkState([]Player{plr("p1", 10)},
		tl("MINE", 0, 0, TileMineSite, "p1", 1, StructureNone),
		tl("N", 1, 0, TileNormal, "p1", 1, StructureNone),
		tl("USED", 2, 0, TileMineSite, "p1", 1, StructureMine),
	)
	if err := s.validateSingleCommand("p1", Command{Type: CmdBuildMine, To: "MINE"}); err != nil {
		t.Fatalf("mine on unused mine site = %v, want nil", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdBuildMine, To: "N"}); err != ErrBadTarget {
		t.Fatalf("mine on normal tile = %v, want ErrBadTarget", err)
	}
	if err := s.validateSingleCommand("p1", Command{Type: CmdBuildMine, To: "USED"}); err != ErrBadTarget {
		t.Fatalf("mine on used mine site = %v, want ErrBadTarget", err)
	}
}

// ---- draft-wide (validateRealCommands) rules ----

func TestValidateRealCommands_MarchOriginLimit(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
		tl("C", 0, 1, TileNormal, "", 0, StructureNone),
	)
	cmds := []Command{march("A", "B", 1), march("A", "C", 1)}
	if err := s.validateRealCommands("p1", cmds); err != ErrMarchOriginLimit {
		t.Fatalf("two marches from same origin = %v, want ErrMarchOriginLimit", err)
	}
}

func TestValidateRealCommands_ArmyOverspend(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("A", 0, 0, TileNormal, "p1", 2, StructureNone),
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
	)
	// Marching all 2 armies out of a 2-army tile leaves 0 (< 1).
	if err := s.validateRealCommands("p1", []Command{march("A", "B", 2)}); err != ErrNotEnoughArmies {
		t.Fatalf("army overspend = %v, want ErrNotEnoughArmies", err)
	}
}

func TestValidateRealCommands_EnergyOverspend(t *testing.T) {
	s := mkState([]Player{plr("p1", 2)}, // recruit costs 3
		tl("CAP", 0, 0, TileCapital, "p1", 3, StructureNone),
	)
	s.Tiles["CAP"].CapitalOwner = "p1"
	if err := s.validateRealCommands("p1", []Command{{Type: CmdRecruit, To: "CAP"}}); err != ErrNotEnoughEnergy {
		t.Fatalf("energy overspend = %v, want ErrNotEnoughEnergy", err)
	}
}

func TestValidateRealCommands_RecruitLimit(t *testing.T) {
	s := mkState([]Player{plr("p1", 6)},
		tl("CAP", 0, 0, TileCapital, "p1", 3, StructureNone),
	)
	s.Tiles["CAP"].CapitalOwner = "p1"
	cmds := []Command{{Type: CmdRecruit, To: "CAP"}, {Type: CmdRecruit, To: "CAP"}}
	if err := s.validateRealCommands("p1", cmds); err != ErrRecruitLimit {
		t.Fatalf("two recruits = %v, want ErrRecruitLimit", err)
	}
}

func TestValidateRealCommands_BuildLimit(t *testing.T) {
	s := mkState([]Player{plr("p1", 10)},
		tl("N", 0, 0, TileNormal, "p1", 1, StructureNone),
		tl("MINE", 1, 0, TileMineSite, "p1", 1, StructureNone),
	)
	cmds := []Command{{Type: CmdBuildFortress, To: "N"}, {Type: CmdBuildMine, To: "MINE"}}
	if err := s.validateRealCommands("p1", cmds); err != ErrBuildLimit {
		t.Fatalf("fortress + mine in one round = %v, want ErrBuildLimit", err)
	}
}

func TestValidateRealCommands_DuplicateFortify(t *testing.T) {
	s := mkState([]Player{plr("p1", 4)},
		tl("N", 0, 0, TileNormal, "p1", 2, StructureNone),
	)
	cmds := []Command{{Type: CmdFortify, To: "N"}, {Type: CmdFortify, To: "N"}}
	if err := s.validateRealCommands("p1", cmds); err != ErrDuplicateCommand {
		t.Fatalf("duplicate fortify = %v, want ErrDuplicateCommand", err)
	}
}

func TestValidateRealCommands_TooMany(t *testing.T) {
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
	)
	cmds := []Command{HoldCommand(), HoldCommand(), HoldCommand(), HoldCommand()} // 4 > RealCommandSlots
	if err := s.validateRealCommands("p1", cmds); err != ErrTooManyCommands {
		t.Fatalf("too many commands = %v, want ErrTooManyCommands", err)
	}
}

func TestValidateRealCommands_UnknownPlayer(t *testing.T) {
	s := mkState([]Player{plr("p1", 0)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
	)
	if err := s.validateRealCommands("ghost", nil); err != ErrUnknownPlayer {
		t.Fatalf("unknown player = %v, want ErrUnknownPlayer", err)
	}
}

// ---- pure helpers ----

func TestCommand_EnergyCost(t *testing.T) {
	cases := []struct {
		cmd  Command
		want int
	}{
		{Command{Type: CmdMarch}, CostMarch},
		{Command{Type: CmdHold}, CostHold},
		{Command{Type: CmdFortify}, CostFortify},
		{Command{Type: CmdRecruit}, CostRecruit},
		{Command{Type: CmdBuildFortress}, CostBuildFortress},
		{Command{Type: CmdBuildMine}, CostBuildMine},
	}
	for _, c := range cases {
		if got := c.cmd.EnergyCost(); got != c.want {
			t.Fatalf("%s EnergyCost = %d, want %d", c.cmd.Type, got, c.want)
		}
	}
	// Explicit literal expectations, per the spec table.
	if CostMarch != 0 || CostHold != 0 || CostFortify != 1 || CostRecruit != 3 ||
		CostBuildFortress != 3 || CostBuildMine != 4 {
		t.Fatalf("energy cost constants drifted from the spec table")
	}
}

func TestHiddenCommandCount(t *testing.T) {
	if got := HiddenCommandCount(false); got != 2 {
		t.Fatalf("HiddenCommandCount(false) = %d, want 2", got)
	}
	if got := HiddenCommandCount(true); got != 3 {
		t.Fatalf("HiddenCommandCount(true) = %d, want 3", got)
	}
}

// realCommands assembly: a real declaration is the first command; a Faux
// declaration contributes nothing and all slots come from hidden orders. Both
// pad to exactly RealCommandSlots with Holds.
func TestState_RealCommands_Assembly(t *testing.T) {
	s := mkState([]Player{plr("p1", 10)},
		tl("A", 0, 0, TileNormal, "p1", 3, StructureNone),
		tl("B", 1, 0, TileNormal, "", 0, StructureNone),
		tl("C", -1, 0, TileNormal, "", 0, StructureNone),
	)

	// Real declaration + one hidden order + pad.
	plan(s, "p1", march("A", "B", 1), march("A", "C", 1))
	got := s.realCommands("p1")
	if len(got) != RealCommandSlots {
		t.Fatalf("real command count = %d, want %d", len(got), RealCommandSlots)
	}
	if got[0].To != "B" || got[1].To != "C" || got[2].Type != CmdHold {
		t.Fatalf("declaration-first assembly wrong: %+v", got)
	}

	// Faux declaration: the public declaration executes nothing.
	planFaux(s, "p1", march("A", "B", 1), march("A", "C", 1))
	got = s.realCommands("p1")
	if len(got) != RealCommandSlots {
		t.Fatalf("faux real command count = %d, want %d", len(got), RealCommandSlots)
	}
	if got[0].To != "C" {
		t.Fatalf("faux declaration should not execute; want first real march to C, got %+v", got[0])
	}

	// Auto-Hold declaration (not submitted): first slot is a Hold.
	s.Declarations["p1"] = Declaration{Command: march("A", "B", 1), Submitted: false}
	s.Orders["p1"] = OrderSet{Commands: []Command{march("A", "C", 1)}, Submitted: true}
	got = s.realCommands("p1")
	if got[0].Type != CmdHold {
		t.Fatalf("unsubmitted declaration should become Hold, got %+v", got[0])
	}
}

// ---- engine submission paths ----

func TestSubmitDeclaration_ValidatesCommand(t *testing.T) {
	e := startedEngine(t, 3)
	toDeclaration(t, e)

	// Non-adjacent march between two capitals is rejected by the validator.
	capH := capitalOf(t, e.State(), "h")
	capP2 := capitalOf(t, e.State(), "p2")
	if err := e.SubmitDeclaration("h", march(string(capH), string(capP2), 1)); err == nil {
		t.Fatalf("expected an error submitting an illegal declaration")
	}

	// A recruit on the player's own capital is accepted.
	if err := e.SubmitDeclaration("h", Command{Type: CmdRecruit, To: capH}); err != nil {
		t.Fatalf("valid recruit declaration = %v, want nil", err)
	}
	if !e.State().Declarations["h"].Submitted {
		t.Fatalf("declaration not recorded as submitted")
	}
}

func TestSubmitDeclaration_WrongPhase(t *testing.T) {
	e := startedEngine(t, 3) // INCOME, not DECLARATION
	if err := e.SubmitDeclaration("h", HoldCommand()); err != ErrWrongPhase {
		t.Fatalf("SubmitDeclaration in income = %v, want ErrWrongPhase", err)
	}
}

func TestSetOrders_TooManyCommands(t *testing.T) {
	e := startedEngine(t, 3)
	toSecretPlanning(t, e)
	// faux=false allows only 2 hidden commands.
	cmds := []Command{HoldCommand(), HoldCommand(), HoldCommand()}
	if err := e.SetOrders("p2", cmds, false); err != ErrTooManyCommands {
		t.Fatalf("SetOrders with 3 hidden commands = %v, want ErrTooManyCommands", err)
	}
}

func TestSetOrders_FauxOnAutoHold(t *testing.T) {
	e := startedEngine(t, 3)
	toSecretPlanning(t, e) // p2 has an auto-Hold declaration
	if err := e.SetOrders("p2", nil, true); err != ErrFauxOnHold {
		t.Fatalf("Faux on auto-Hold = %v, want ErrFauxOnHold", err)
	}
}

func TestSetOrders_FauxUnavailable(t *testing.T) {
	e := startedEngine(t, 3)
	toSecretPlanning(t, e)
	e.state.player("p2").FauxAvailable = false
	if err := e.SetOrders("p2", nil, true); err != ErrFauxUnavailable {
		t.Fatalf("Faux when unavailable = %v, want ErrFauxUnavailable", err)
	}
}
