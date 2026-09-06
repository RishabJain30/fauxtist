package game

// This file defines the structured results the engine produces for a
// completed round and a completed match. They are deliberately data, not an
// event stream: a whole round resolves atomically into ONE Resolution, which
// the room sends as a single compact round_resolved message and the client
// animates locally — never dozens of per-step WebSocket frames.

// FrameKind tags one ordered step of the resolution animation timeline.
type FrameKind string

const (
	FrameFauxRevealed FrameKind = "faux_revealed"   // a declaration was a Faux Order; it dissolves
	FrameRecruit      FrameKind = "recruit"         // armies added at a capital/fortress
	FrameFortify      FrameKind = "fortify"         // a temporary defensive bonus was applied
	FrameMarch        FrameKind = "march"           // armies moved from one tile to an adjacent tile
	FrameBattle       FrameKind = "battle"          // a contested tile was fought over
	FrameCapture      FrameKind = "capture"         // a tile changed owner
	FrameBuild        FrameKind = "build"           // a structure was completed
	FrameBuildFailed  FrameKind = "build_failed"    // a build failed (tile lost); energy refunded
	FrameRelic        FrameKind = "relic_influence" // a controlled relic awarded influence
)

// BattleSide is one participant in a battle: a player's combined incoming
// force (or the defender's garrison), with both the raw army count and the
// effective strength that determined the outcome (armies plus Fortress /
// Fortify bonuses, which help decide a battle but never become armies).
type BattleSide struct {
	Player   PlayerID `json:"player,omitempty"` // empty for a neutral defender
	Armies   int      `json:"armies"`
	Strength int      `json:"strength"`
}

// ResolutionFrame is one animation step. Fields not relevant to a Kind are
// omitted. It carries no secret information — by the time a Resolution
// exists, every Faux and every hidden order has already been resolved into
// public fact.
type ResolutionFrame struct {
	Kind      FrameKind    `json:"kind"`
	Player    PlayerID     `json:"player,omitempty"`
	From      TileID       `json:"from,omitempty"`
	To        TileID       `json:"to,omitempty"`
	Armies    int          `json:"armies,omitempty"`
	Structure Structure    `json:"structure,omitempty"`
	Attackers []BattleSide `json:"attackers,omitempty"`
	Defender  *BattleSide  `json:"defender,omitempty"`
	Winner    PlayerID     `json:"winner,omitempty"` // empty = defender/neutral held, or a tie failed
	Result    int          `json:"result,omitempty"` // armies remaining on the tile after the battle
	Influence int          `json:"influence,omitempty"`
}

// PlayerRoundSummary is one player's per-round deltas, shown on the
// ROUND_SUMMARY screen.
type PlayerRoundSummary struct {
	Player           PlayerID `json:"player"`
	EnergyDelta      int      `json:"energyDelta"`
	InfluenceDelta   int      `json:"influenceDelta"`
	TerritoryDelta   int      `json:"territoryDelta"`
	ArmiesLost       int      `json:"armiesLost"`
	FauxUsed         bool     `json:"fauxUsed"`
	RelicsControlled int      `json:"relicsControlled"`
	DominationStreak int      `json:"dominationStreak"`
}

// RoundSummary is the whole table for one completed round, players sorted by
// id for determinism.
type RoundSummary struct {
	Round   int                  `json:"round"`
	Players []PlayerRoundSummary `json:"players"`
}

// Resolution is one round's complete, atomically-computed outcome: the
// ordered animation timeline, the per-player summary, and the final
// authoritative public board after the round. The client animates Frames and
// arrives at Board; nothing here depends on message ordering or wall-clock
// animation time.
type Resolution struct {
	Round   int               `json:"round"`
	Frames  []ResolutionFrame `json:"frames"`
	Summary RoundSummary      `json:"summary"`
	Board   []Tile            `json:"board"`
}

func (r Resolution) clone() Resolution {
	r.Frames = append([]ResolutionFrame(nil), r.Frames...)
	for i := range r.Frames {
		r.Frames[i].Attackers = append([]BattleSide(nil), r.Frames[i].Attackers...)
		if r.Frames[i].Defender != nil {
			d := *r.Frames[i].Defender
			r.Frames[i].Defender = &d
		}
	}
	r.Summary.Players = append([]PlayerRoundSummary(nil), r.Summary.Players...)
	r.Board = append([]Tile(nil), r.Board...)
	return r
}

// VictoryReason explains why the match ended.
type VictoryReason string

const (
	VictoryDomination VictoryReason = "domination"
	VictoryInfluence  VictoryReason = "influence"
	VictoryForfeit    VictoryReason = "forfeit"
	VictoryShared     VictoryReason = "shared"
	VictoryNoContest  VictoryReason = "no_contest"
)

// Standing is one player's final ranked position and the raw tiebreaker
// values it was decided on.
type Standing struct {
	Player           PlayerID `json:"player"`
	Rank             int      `json:"rank"` // 1-based; ties share a rank
	Influence        int      `json:"influence"`
	RelicsControlled int      `json:"relicsControlled"`
	Territories      int      `json:"territories"` // non-capital tiles owned
	Armies           int      `json:"armies"`
	Energy           int      `json:"energy"`
	Forfeited        bool     `json:"forfeited"`
}

// MatchStats accumulates one player's whole-match totals, for the Game Over
// screen's lightweight highlights.
type MatchStats struct {
	Captures        int `json:"captures"`
	ArmiesLost      int `json:"armiesLost"`
	FortressesBuilt int `json:"fortressesBuilt"`
	MinesBuilt      int `json:"minesBuilt"`
	FauxUsedRound   int `json:"fauxUsedRound"` // 0 = never used
}

// GameResult is the final, authoritative match outcome, set once at
// GAME_OVER.
type GameResult struct {
	Reason           VictoryReason           `json:"reason"`
	Winners          []PlayerID              `json:"winners"` // one, or several for a shared win; empty for no_contest
	Standings        []Standing              `json:"standings"`
	Stats            map[PlayerID]MatchStats `json:"stats"`
	InfluenceHistory map[PlayerID][]int      `json:"influenceHistory"` // influence total at each round end
}

func (g GameResult) clone() GameResult {
	g.Winners = append([]PlayerID(nil), g.Winners...)
	g.Standings = append([]Standing(nil), g.Standings...)
	if g.Stats != nil {
		stats := make(map[PlayerID]MatchStats, len(g.Stats))
		for k, v := range g.Stats {
			stats[k] = v
		}
		g.Stats = stats
	}
	if g.InfluenceHistory != nil {
		hist := make(map[PlayerID][]int, len(g.InfluenceHistory))
		for k, v := range g.InfluenceHistory {
			hist[k] = append([]int(nil), v...)
		}
		g.InfluenceHistory = hist
	}
	return g
}
