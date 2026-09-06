package game

import "sort"

// ---- Hex geometry (axial coordinates) ----
//
// All adjacency and distance is derived from axial (q, r) coordinates on the
// server. The frontend's own geometry (honeycomb-grid) is for rendering and
// interaction only and is never trusted for rule enforcement.

// hexDirections are the six axial unit steps to a hex's neighbors.
var hexDirections = [6]Axial{
	{Q: 1, R: 0}, {Q: 1, R: -1}, {Q: 0, R: -1},
	{Q: -1, R: 0}, {Q: -1, R: 1}, {Q: 0, R: 1},
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// HexDistance is the number of steps between two axial coordinates.
func HexDistance(a, b Axial) int {
	dq := a.Q - b.Q
	dr := a.R - b.R
	return (absInt(dq) + absInt(dq+dr) + absInt(dr)) / 2
}

// Neighbors returns the six axial coordinates adjacent to a (regardless of
// whether they exist on any given map).
func Neighbors(a Axial) []Axial {
	out := make([]Axial, 6)
	for i, d := range hexDirections {
		out[i] = Axial{Q: a.Q + d.Q, R: a.R + d.R}
	}
	return out
}

// hexAdjacent reports whether two coordinates are exactly one step apart.
func hexAdjacent(a, b Axial) bool { return HexDistance(a, b) == 1 }

// tileAt returns the tile at a coordinate, or nil. Linear scan is fine for a
// board of at most a few dozen hexes.
func (s *State) tileAt(c Axial) *Tile {
	for _, tid := range s.SortedTileIDs() {
		if s.Tiles[tid].Coord == c {
			return s.Tiles[tid]
		}
	}
	return nil
}

// adjacent reports whether two tiles (by id) are neighbors on the board.
func (s *State) adjacent(a, b TileID) bool {
	ta, tb := s.Tiles[a], s.Tiles[b]
	if ta == nil || tb == nil {
		return false
	}
	return hexAdjacent(ta.Coord, tb.Coord)
}

// PlayerID is a stable, per-match identifier for a player. Minted by the
// identity package (a secure random id); the engine only ever compares them.
type PlayerID string

// Phase is the current stage of the match's round loop. The authoritative
// state machine is:
//
//	LOBBY → INCOME → NEGOTIATION → DECLARATION → DECLARATION_REVEAL →
//	SECRET_PLANNING → RESOLUTION → ROUND_SUMMARY →
//	(next round's INCOME | GAME_OVER) → REMATCH_LOBBY
//
// Every timed phase's absolute deadline is owned by the room, not the
// engine — the engine only knows which phase it is in and validates that
// each action is legal for that phase.
type Phase string

const (
	PhaseLobby             Phase = "lobby"
	PhaseIncome            Phase = "income"
	PhaseNegotiation       Phase = "negotiation"
	PhaseDeclaration       Phase = "declaration"
	PhaseDeclarationReveal Phase = "declaration_reveal"
	PhaseSecretPlanning    Phase = "secret_planning"
	PhaseResolution        Phase = "resolution"
	PhaseRoundSummary      Phase = "round_summary"
	PhaseGameOver          Phase = "game_over"
)

// TileType classifies what a hex is. It never changes over a match — a relic
// stays a relic even while owned; ownership and structures are tracked
// separately.
type TileType string

const (
	TileNormal   TileType = "normal"
	TileCapital  TileType = "capital"
	TileRelic    TileType = "relic"
	TileMineSite TileType = "mine_site"
)

// Structure is a player-built (or absent) improvement on a territory. Only
// one per territory; relics and capitals can never hold a player structure.
type Structure string

const (
	StructureNone     Structure = "none"
	StructureFortress Structure = "fortress"
	StructureMine     Structure = "mine"
)

// Axial is a hex coordinate in axial (q, r) space. Adjacency and distance
// are derived from these on the server; the client's geometry is for
// rendering only and never trusted for rule enforcement.
type Axial struct {
	Q int `json:"q"`
	R int `json:"r"`
}

// Tile is one hex of the board. Owner is empty for a neutral tile. Armies is
// the garrison currently standing on it. CapitalOwner is set only for a
// capital tile and never changes — a capital always belongs to (and can only
// be recruited from / marched out of by) its original owner, and can never
// be captured or entered by an enemy.
type Tile struct {
	ID           TileID    `json:"id"`
	Coord        Axial     `json:"coord"`
	Type         TileType  `json:"type"`
	Owner        PlayerID  `json:"owner,omitempty"`
	Armies       int       `json:"armies"`
	Structure    Structure `json:"structure"`
	CapitalOwner PlayerID  `json:"capitalOwner,omitempty"`
}

// TileID is a stable per-tile identifier, unique within a map template.
type TileID string

// clone returns a value copy of the tile — safe because Tile holds no
// reference types.
func (t *Tile) clone() *Tile {
	c := *t
	return &c
}

// Player is one participant's authoritative, engine-owned match state.
// Connection presence, readiness, and spectator status are tracked by the
// room, not here — the engine has no notion of a socket.
type Player struct {
	ID               PlayerID  `json:"id"`
	Name             string    `json:"name"`
	Emoji            string    `json:"emoji"`
	Faction          FactionID `json:"faction,omitempty"`
	SpawnSlot        int       `json:"spawnSlot"`
	Energy           int       `json:"energy"`
	Influence        int       `json:"influence"`
	FauxAvailable    bool      `json:"fauxAvailable"`
	FauxUsedRound    int       `json:"fauxUsedRound"` // 0 = unused; else the round it was spent
	DominationStreak int       `json:"dominationStreak"`
	Forfeited        bool      `json:"forfeited"`
}

// State is the full authoritative match state. The Engine is its sole
// mutator; the room reads copy-safe snapshots via Engine.State and applies
// per-viewer redaction before anything leaves the process.
//
// Declarations and Orders hold secret, per-round planning data. They are
// exported so the room can read a viewer's OWN private draft when building
// that viewer's snapshot, exactly as it already reads (and redacts) other
// players' secrets — every recipient-facing redaction rule lives in the
// room, never here.
type State struct {
	Phase       Phase
	Preset      Preset
	Round       int // 1-based; 0 while in lobby
	TotalRounds int
	Players     []Player
	HostID      PlayerID
	MapID       string
	Tiles       map[TileID]*Tile

	// Declarations submitted this round, keyed by player. Populated during
	// DECLARATION and revealed publicly by the room at DECLARATION_REVEAL.
	Declarations map[PlayerID]Declaration
	// Orders submitted this round during SECRET_PLANNING, keyed by player.
	// Always secret until the resolution timeline reveals what actually
	// executed.
	Orders map[PlayerID]OrderSet

	// Resolution is the most recently completed round's timeline + summary,
	// retained so a client joining during ROUND_SUMMARY can still render it.
	Resolution *Resolution
	// Result is the final standings, set once and only once at GAME_OVER.
	Result *GameResult
	// pendingGameOver is set by Resolve when the round that just resolved
	// ends the match (domination win or final round); AdvanceRound reads it
	// to decide GAME_OVER vs. the next round.
	pendingGameOver bool

	// Stats accumulates whole-match per-player totals for the Game Over
	// highlights. InfluenceHistory records each player's influence total at
	// every round end, for the results chart. Both are match-scoped and
	// reset on a rematch.
	Stats            map[PlayerID]*MatchStats
	InfluenceHistory map[PlayerID][]int
}

// SortedTileIDs returns every tile id in ascending order. Every rule that
// iterates tiles uses this rather than ranging the map directly, so Go's
// randomized map iteration order can never affect a resolution outcome.
func (s *State) SortedTileIDs() []TileID {
	ids := make([]TileID, 0, len(s.Tiles))
	for id := range s.Tiles {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// SortedPlayerIDs returns every player's id in ascending order, for the same
// determinism reason as SortedTileIDs.
func (s *State) SortedPlayerIDs() []PlayerID {
	ids := make([]PlayerID, 0, len(s.Players))
	for _, p := range s.Players {
		ids = append(ids, p.ID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// playerIndex returns the index of id in Players, or -1.
func (s *State) playerIndex(id PlayerID) int {
	for i, p := range s.Players {
		if p.ID == id {
			return i
		}
	}
	return -1
}

// player returns a pointer to the player row for id, or nil.
func (s *State) player(id PlayerID) *Player {
	if i := s.playerIndex(id); i >= 0 {
		return &s.Players[i]
	}
	return nil
}

// PlayerByID returns a pointer to the player row for id, or nil. A value
// receiver so it is callable directly on the temporary Engine.State returns;
// the returned pointer aliases that copy and is safe to read. Exported for the
// room, which merges engine identity with room-tracked presence/readiness.
func (s State) PlayerByID(id PlayerID) *Player {
	if i := s.playerIndex(id); i >= 0 {
		return &s.Players[i]
	}
	return nil
}

// tilesOwnedBy returns the ids of tiles currently owned by id, sorted.
func (s *State) tilesOwnedBy(id PlayerID) []TileID {
	var out []TileID
	for _, tid := range s.SortedTileIDs() {
		if s.Tiles[tid].Owner == id {
			out = append(out, tid)
		}
	}
	return out
}

// relicsControlledBy counts relic tiles currently owned by id.
func (s *State) relicsControlledBy(id PlayerID) int {
	n := 0
	for _, tid := range s.SortedTileIDs() {
		t := s.Tiles[tid]
		if t.Type == TileRelic && t.Owner == id {
			n++
		}
	}
	return n
}

// clone deep-copies the state so a caller (the room, tests) can read or
// serialize it without any risk of aliasing engine-internal mutable data.
// Every reference-typed field is copied into fresh storage.
func (s *State) clone() State {
	c := *s
	c.Players = append([]Player(nil), s.Players...)

	c.Tiles = make(map[TileID]*Tile, len(s.Tiles))
	for id, t := range s.Tiles {
		c.Tiles[id] = t.clone()
	}

	if s.Declarations != nil {
		c.Declarations = make(map[PlayerID]Declaration, len(s.Declarations))
		for id, d := range s.Declarations {
			c.Declarations[id] = d
		}
	}
	if s.Orders != nil {
		c.Orders = make(map[PlayerID]OrderSet, len(s.Orders))
		for id, o := range s.Orders {
			c.Orders[id] = o.clone()
		}
	}
	if s.Resolution != nil {
		rc := s.Resolution.clone()
		c.Resolution = &rc
	}
	if s.Result != nil {
		gr := s.Result.clone()
		c.Result = &gr
	}
	if s.Stats != nil {
		c.Stats = make(map[PlayerID]*MatchStats, len(s.Stats))
		for id, st := range s.Stats {
			cp := *st
			c.Stats[id] = &cp
		}
	}
	if s.InfluenceHistory != nil {
		c.InfluenceHistory = make(map[PlayerID][]int, len(s.InfluenceHistory))
		for id, h := range s.InfluenceHistory {
			c.InfluenceHistory[id] = append([]int(nil), h...)
		}
	}
	return c
}
