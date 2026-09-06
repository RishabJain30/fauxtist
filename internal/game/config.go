package game

import "time"

// This file is the single source of truth for every tunable balance number
// and timing preset in Fauxlands. Nothing else in the engine hardcodes a
// cost, cap, or bonus — they all reference the constants here so a designer
// can retune the game from one place and every rule, snapshot, and test
// follows.

// Player-count bounds. Fauxlands is a 3–6 player game; spectators are a
// separate, larger cap enforced by the room, not the engine.
const (
	MinPlayers    = 3
	MaxPlayers    = 6
	MaxSpectators = 10
)

// RealCommandSlots is how many real commands every player resolves each
// round. A normal declaration is the first of these; a Faux declaration is
// not, so a Faux player submits all three as hidden orders.
const RealCommandSlots = 3

// Economy.
const (
	StartingEnergy = 4
	BaseIncome     = 3
	EnergyCap      = 12
	MineEnergy     = 1 // extra energy per completed Mine, from the round after it completes
)

// Starting armies.
const (
	StartingCapitalArmies  = 3
	StartingAdjacentArmies = 2
)

// March bounds.
const (
	MarchMinArmies = 1
	MarchMaxArmies = 3
)

// Command energy costs.
const (
	CostMarch         = 0
	CostFortify       = 1
	CostRecruit       = 3
	CostBuildFortress = 3
	CostBuildMine     = 4
	CostHold          = 0
)

// Combat / structure bonuses.
const (
	FortifyBonus         = 2 // temporary, this resolution only
	FortressBonus        = 1 // permanent, while the Fortress stands
	RecruitArmies        = 2 // armies added by one Recruit
	NeutralRelicGuardian = 1 // a neutral relic begins guarded by this many armies
)

// Victory.
const (
	RelicCount            = 5
	DominationThreshold   = 3 // relics controlled to begin/continue a Domination streak
	DominationConsecutive = 2 // consecutive round-end checks at threshold to win
)

// FactionID is a cosmetic, symmetric faction identity. Factions differ only
// in colour/sigil/pattern/label — never in ability (see the non-goals: no
// asymmetric powers in this release).
type FactionID string

const (
	FactionEmber FactionID = "ember"
	FactionTide  FactionID = "tide"
	FactionGrove FactionID = "grove"
	FactionDusk  FactionID = "dusk"
	FactionSun   FactionID = "sun"
	FactionFrost FactionID = "frost"
)

// FactionOrder is the stable assignment order: the player in spawn slot i
// gets FactionOrder[i]. Never randomized independently of spawn assignment,
// so a faction always matches its spawn slot within one match.
var FactionOrder = []FactionID{
	FactionEmber, FactionTide, FactionGrove, FactionDusk, FactionSun, FactionFrost,
}

// Preset selects the match length and per-phase timing profile. Chosen by
// the host in the lobby; immutable once the match starts.
type Preset string

const (
	PresetQuick    Preset = "quick"
	PresetStandard Preset = "standard"
	PresetRelaxed  Preset = "relaxed"
)

// PhaseTimings is the server-authoritative duration of each timed phase for
// one preset. The engine's pure rules never read these — only the room uses
// them to set absolute phase deadlines — but they live here so all balance
// numbers stay in one file.
type PhaseTimings struct {
	Income            time.Duration
	Negotiation       time.Duration
	Declaration       time.Duration
	DeclarationReveal time.Duration
	SecretPlanning    time.Duration
	ResolutionMax     time.Duration // maximum animation window; never affects authoritative combat
	RoundSummary      time.Duration
}

// PresetConfig fully describes one preset: how many rounds it lasts and how
// long each phase runs.
type PresetConfig struct {
	Rounds  int
	Timings PhaseTimings
}

// presets is the authoritative timing table (see the product spec's
// "Timing presets"). Standard is the default and targets ~15–20 minutes.
var presets = map[Preset]PresetConfig{
	PresetQuick: {
		Rounds: 6,
		Timings: PhaseTimings{
			Income:            2 * time.Second,
			Negotiation:       20 * time.Second,
			Declaration:       10 * time.Second,
			DeclarationReveal: 3 * time.Second,
			SecretPlanning:    25 * time.Second,
			ResolutionMax:     12 * time.Second,
			RoundSummary:      6 * time.Second,
		},
	},
	PresetStandard: {
		Rounds: 8,
		Timings: PhaseTimings{
			Income:            3 * time.Second,
			Negotiation:       35 * time.Second,
			Declaration:       15 * time.Second,
			DeclarationReveal: 3 * time.Second,
			SecretPlanning:    35 * time.Second,
			ResolutionMax:     15 * time.Second,
			RoundSummary:      8 * time.Second,
		},
	},
	PresetRelaxed: {
		Rounds: 8,
		Timings: PhaseTimings{
			Income:            3 * time.Second,
			Negotiation:       50 * time.Second,
			Declaration:       20 * time.Second,
			DeclarationReveal: 4 * time.Second,
			SecretPlanning:    50 * time.Second,
			ResolutionMax:     18 * time.Second,
			RoundSummary:      10 * time.Second,
		},
	},
}

// DefaultPreset is used when a host starts a match without having explicitly
// chosen one.
const DefaultPreset = PresetStandard

// PresetConfigFor returns the configuration for a preset, falling back to the
// default for an unrecognized value so a malformed setting can never leave a
// match with a zero-round, zero-timing profile.
func PresetConfigFor(p Preset) PresetConfig {
	if cfg, ok := presets[p]; ok {
		return cfg
	}
	return presets[DefaultPreset]
}

// ValidPreset reports whether p is one of the three known presets.
func ValidPreset(p Preset) bool {
	_, ok := presets[p]
	return ok
}
