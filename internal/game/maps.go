package game

import (
	"fmt"
	"sort"
)

// This file holds Fauxlands' authored, data-driven board templates: one
// balanced layout per active-player count (3–6). The tile FIELD (which hexes
// exist) is built deterministically from a symmetric design so the engine can
// freely rotate or mirror it at match start; the DESIGN decisions — which
// hexes are capitals, relics, and mine sites, and which spawn slot each
// capital is — are authored coordinate tables below. Nothing here is
// procedurally generated at runtime: MapTemplateFor returns the same authored
// layout every time, and StartMatch only ever applies a fixed symmetry
// transform to it.

// TileDef is one authored tile in a template. SpawnSlot is the 0-based slot
// for a capital tile and -1 for every other tile. Decoration is an optional
// cosmetic variant hint for the client and never affects rules.
type TileDef struct {
	ID         TileID
	Coord      Axial
	Type       TileType
	SpawnSlot  int
	Decoration string
}

// MapTemplate is a full authored board for one active-player count.
type MapTemplate struct {
	ID          string
	PlayerCount int
	Tiles       []TileDef
}

// mapDesign is the authored, human-readable design for one map: the hex field
// plus the special-tile coordinate lists. build() expands it into a
// MapTemplate.
type mapDesign struct {
	id          string
	playerCount int
	field       []Axial // every hex that exists (must be closed under rot60 + mirror)
	capitals    []Axial // one per spawn slot, in slot order
	relics      []Axial
	mineSites   []Axial
}

// rot60 rotates an axial coordinate 60° about the origin.
func rot60(a Axial) Axial { return Axial{Q: -a.R, R: a.Q + a.R} }

// mirrorAxial reflects an axial coordinate across the q-axis.
func mirrorAxial(a Axial) Axial { return Axial{Q: a.Q, R: -a.Q - a.R} }

// hexagon returns every axial coordinate within radius of the origin.
func hexagon(radius int) []Axial {
	var out []Axial
	for q := -radius; q <= radius; q++ {
		for r := -radius; r <= radius; r++ {
			if HexDistance(Axial{}, Axial{Q: q, R: r}) <= radius {
				out = append(out, Axial{Q: q, R: r})
			}
		}
	}
	return out
}

// ring returns every axial coordinate at exactly the given distance from the
// origin.
func ring(radius int) []Axial {
	if radius == 0 {
		return []Axial{{}}
	}
	var out []Axial
	for q := -radius; q <= radius; q++ {
		for r := -radius; r <= radius; r++ {
			if HexDistance(Axial{}, Axial{Q: q, R: r}) == radius {
				out = append(out, Axial{Q: q, R: r})
			}
		}
	}
	return out
}

// axisTips returns the six "corner" coordinates of a ring — the ones lying on
// the main axes, forming a single rot60 orbit.
func axisTips(radius int) []Axial {
	return []Axial{
		{Q: radius, R: 0}, {Q: 0, R: radius}, {Q: -radius, R: radius},
		{Q: -radius, R: 0}, {Q: 0, R: -radius}, {Q: radius, R: -radius},
	}
}

// ringCorners is an alias for axisTips at ring radius (the 6 cells at the
// ring's corners).
func ringCorners(radius int) []Axial { return axisTips(radius) }

// ringEdges returns the ring's non-corner cells (the 6 corners removed).
func ringEdges(radius int) []Axial {
	corners := map[Axial]bool{}
	for _, c := range ringCorners(radius) {
		corners[c] = true
	}
	var out []Axial
	for _, c := range ring(radius) {
		if !corners[c] {
			out = append(out, c)
		}
	}
	return out
}

func mergeCoords(sets ...[]Axial) []Axial {
	seen := map[Axial]bool{}
	var out []Axial
	for _, s := range sets {
		for _, c := range s {
			if !seen[c] {
				seen[c] = true
				out = append(out, c)
			}
		}
	}
	return out
}

// coordID builds a stable tile id from a coordinate.
func coordID(c Axial) TileID {
	return TileID(fmt.Sprintf("t_%d_%d", c.Q, c.R))
}

// designs is the authored table of all four maps. The field builders keep the
// hex set symmetric; the capital/relic/mineSite lists are the balance-tuned
// authored content.
var designs = []mapDesign{
	{
		id:          "atlas-3",
		playerCount: 3,
		field:       mergeCoords(hexagon(2), ringCorners(3)),
		capitals:    []Axial{{Q: 3, R: 0}, {Q: -3, R: 3}, {Q: 0, R: -3}},
		relics:      []Axial{{Q: 0, R: 0}, {Q: 1, R: 0}, {Q: -1, R: 1}, {Q: 0, R: -1}, {Q: 1, R: -1}},
		mineSites:   []Axial{{Q: 0, R: 2}, {Q: -2, R: 0}, {Q: 2, R: -2}, {Q: -1, R: 0}},
	},
	{
		id:          "atlas-4",
		playerCount: 4,
		field:       mergeCoords(hexagon(2), ringEdges(3)),
		capitals:    []Axial{{Q: 2, R: 1}, {Q: -2, R: -1}, {Q: 2, R: -3}, {Q: -2, R: 3}},
		relics:      []Axial{{Q: 0, R: 0}, {Q: 1, R: -1}, {Q: -1, R: 1}, {Q: 1, R: 0}, {Q: -1, R: 0}},
		mineSites:   []Axial{{Q: 2, R: -1}, {Q: -2, R: 1}, {Q: 0, R: 2}, {Q: 0, R: -2}, {Q: 2, R: -2}},
	},
	{
		id:          "atlas-5",
		playerCount: 5,
		field:       hexagon(3),
		capitals:    []Axial{{Q: 3, R: 0}, {Q: 0, R: 3}, {Q: -3, R: 3}, {Q: -3, R: 0}, {Q: 0, R: -3}},
		relics:      []Axial{{Q: 0, R: 0}, {Q: 1, R: -1}, {Q: -1, R: 1}, {Q: 1, R: 0}, {Q: -1, R: 0}},
		mineSites:   []Axial{{Q: 2, R: -1}, {Q: -1, R: 2}, {Q: -2, R: 1}, {Q: 1, R: 1}, {Q: 2, R: -2}, {Q: -1, R: -1}},
	},
	{
		id:          "atlas-6",
		playerCount: 6,
		field:       mergeCoords(hexagon(3), axisTips(4)),
		capitals:    axisTips(4),
		relics:      []Axial{{Q: 0, R: 0}, {Q: 1, R: -1}, {Q: -1, R: 1}, {Q: 1, R: 0}, {Q: -1, R: 0}},
		mineSites:   []Axial{{Q: 2, R: 0}, {Q: 0, R: 2}, {Q: -2, R: 2}, {Q: -2, R: 0}, {Q: 0, R: -2}, {Q: 2, R: -2}, {Q: 1, R: 1}},
	},
}

// build expands a design into a MapTemplate, assigning tile types and spawn
// slots from the coordinate lists.
func (d mapDesign) build() MapTemplate {
	capSlot := map[Axial]int{}
	for i, c := range d.capitals {
		capSlot[c] = i
	}
	relic := map[Axial]bool{}
	for _, c := range d.relics {
		relic[c] = true
	}
	mine := map[Axial]bool{}
	for _, c := range d.mineSites {
		mine[c] = true
	}

	// Sort the field for a stable tile order in the template.
	field := append([]Axial(nil), d.field...)
	sort.Slice(field, func(i, j int) bool {
		if field[i].Q != field[j].Q {
			return field[i].Q < field[j].Q
		}
		return field[i].R < field[j].R
	})

	tiles := make([]TileDef, 0, len(field))
	for _, c := range field {
		td := TileDef{ID: coordID(c), Coord: c, Type: TileNormal, SpawnSlot: -1}
		switch {
		case capSlot[c] >= 0 && isCapital(capSlot, c):
			td.Type = TileCapital
			td.SpawnSlot = capSlot[c]
		case relic[c]:
			td.Type = TileRelic
		case mine[c]:
			td.Type = TileMineSite
		}
		tiles = append(tiles, td)
	}
	return MapTemplate{ID: d.id, PlayerCount: d.playerCount, Tiles: tiles}
}

// isCapital guards the capSlot zero-value ambiguity (slot 0 vs. absent).
func isCapital(capSlot map[Axial]int, c Axial) bool {
	_, ok := capSlot[c]
	return ok
}

// templates is the built, validated set of all maps, keyed by player count.
var templates = func() map[int]MapTemplate {
	m := map[int]MapTemplate{}
	for _, d := range designs {
		m[d.playerCount] = d.build()
	}
	return m
}()

// MapTemplateFor returns the authored template for an active-player count.
func MapTemplateFor(playerCount int) (MapTemplate, bool) {
	t, ok := templates[playerCount]
	return t, ok
}

// AllMapTemplates returns every authored template, ordered by player count.
func AllMapTemplates() []MapTemplate {
	out := make([]MapTemplate, 0, len(templates))
	for pc := MinPlayers; pc <= MaxPlayers; pc++ {
		if t, ok := templates[pc]; ok {
			out = append(out, t)
		}
	}
	return out
}

// ValidateAllMaps validates every authored template. Called at startup so a
// broken map fails the process immediately rather than at match time.
func ValidateAllMaps() error {
	for _, t := range AllMapTemplates() {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("map %q: %w", t.ID, err)
		}
	}
	return nil
}

// coordSet builds a lookup set of a template's coordinates.
func (m MapTemplate) coordSet() map[Axial]bool {
	set := map[Axial]bool{}
	for _, t := range m.Tiles {
		set[t.Coord] = true
	}
	return set
}

// Validate checks every structural and fairness invariant a template must
// satisfy. It is the authoritative definition of a "valid map" — the startup
// check and the map test both call it.
func (m MapTemplate) Validate() error {
	if m.PlayerCount < MinPlayers || m.PlayerCount > MaxPlayers {
		return fmt.Errorf("player count %d out of range", m.PlayerCount)
	}

	ids := map[TileID]bool{}
	coords := map[Axial]bool{}
	var capitals, relics, mines []TileDef
	slots := map[int]bool{}
	for _, t := range m.Tiles {
		if ids[t.ID] {
			return fmt.Errorf("duplicate tile id %q", t.ID)
		}
		ids[t.ID] = true
		if coords[t.Coord] {
			return fmt.Errorf("duplicate coordinate %+v", t.Coord)
		}
		coords[t.Coord] = true
		switch t.Type {
		case TileCapital:
			capitals = append(capitals, t)
			if t.SpawnSlot < 0 || t.SpawnSlot >= m.PlayerCount {
				return fmt.Errorf("capital %q has out-of-range spawn slot %d", t.ID, t.SpawnSlot)
			}
			if slots[t.SpawnSlot] {
				return fmt.Errorf("duplicate spawn slot %d", t.SpawnSlot)
			}
			slots[t.SpawnSlot] = true
		case TileRelic:
			relics = append(relics, t)
		case TileMineSite:
			mines = append(mines, t)
		case TileNormal:
		default:
			return fmt.Errorf("tile %q has unknown type %q", t.ID, t.Type)
		}
	}

	if len(capitals) != m.PlayerCount {
		return fmt.Errorf("want %d capitals, got %d", m.PlayerCount, len(capitals))
	}
	if len(relics) != RelicCount {
		return fmt.Errorf("want %d relics, got %d", RelicCount, len(relics))
	}
	if len(mines) != m.PlayerCount+1 {
		return fmt.Errorf("want %d mine sites, got %d", m.PlayerCount+1, len(mines))
	}

	if err := m.validateSymmetry(); err != nil {
		return err
	}
	if err := m.validateConnected(); err != nil {
		return err
	}
	if err := m.validateCapitalTerritories(capitals); err != nil {
		return err
	}
	if err := m.validateOuterCapitals(capitals, relics); err != nil {
		return err
	}
	return m.validateSpawnFairness(capitals, relics, mines)
}

// validateSymmetry confirms the tile field is closed under 60° rotation and
// mirroring, so any dihedral transform the engine applies maps the board onto
// itself.
func (m MapTemplate) validateSymmetry() error {
	set := m.coordSet()
	for c := range set {
		if !set[rot60(c)] {
			return fmt.Errorf("field not closed under rotation at %+v", c)
		}
		if !set[mirrorAxial(c)] {
			return fmt.Errorf("field not closed under mirror at %+v", c)
		}
	}
	return nil
}

// validateConnected confirms every tile is reachable from the first via hex
// adjacency.
func (m MapTemplate) validateConnected() error {
	if len(m.Tiles) == 0 {
		return fmt.Errorf("empty map")
	}
	set := m.coordSet()
	seen := map[Axial]bool{}
	start := m.Tiles[0].Coord
	queue := []Axial{start}
	seen[start] = true
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		for _, n := range Neighbors(c) {
			if set[n] && !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	if len(seen) != len(m.Tiles) {
		return fmt.Errorf("map is not fully connected (%d of %d reachable)", len(seen), len(m.Tiles))
	}
	return nil
}

// validateCapitalTerritories confirms every capital has at least one adjacent
// normal tile to seed as its starting territory.
func (m MapTemplate) validateCapitalTerritories(capitals []TileDef) error {
	typeAt := map[Axial]TileType{}
	for _, t := range m.Tiles {
		typeAt[t.Coord] = t.Type
	}
	for _, cap := range capitals {
		ok := false
		for _, n := range Neighbors(cap.Coord) {
			if typeAt[n] == TileNormal {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("capital %q has no adjacent normal starting territory", cap.ID)
		}
	}
	return nil
}

// validateOuterCapitals confirms capitals sit in the outer region and relics
// nearer the centre: the mean capital distance from the origin must exceed the
// mean relic distance.
func (m MapTemplate) validateOuterCapitals(capitals, relics []TileDef) error {
	meanDist := func(ts []TileDef) float64 {
		sum := 0
		for _, t := range ts {
			sum += HexDistance(Axial{}, t.Coord)
		}
		return float64(sum) / float64(len(ts))
	}
	if meanDist(capitals) <= meanDist(relics) {
		return fmt.Errorf("capitals are not outer relative to relics (cap %.2f <= relic %.2f)",
			meanDist(capitals), meanDist(relics))
	}
	return nil
}

// validateSpawnFairness confirms that, across all spawn slots, the distance to
// the nearest relic, the nearest mine site, and the nearest other capital each
// vary by at most one hex.
func (m MapTemplate) validateSpawnFairness(capitals, relics, mines []TileDef) error {
	nearest := func(from Axial, ts []TileDef, skip Axial, hasSkip bool) int {
		best := -1
		for _, t := range ts {
			if hasSkip && t.Coord == skip {
				continue
			}
			d := HexDistance(from, t.Coord)
			if best < 0 || d < best {
				best = d
			}
		}
		return best
	}
	check := func(name string, values []int) error {
		lo, hi := values[0], values[0]
		for _, v := range values {
			if v < lo {
				lo = v
			}
			if v > hi {
				hi = v
			}
		}
		if hi-lo > 1 {
			return fmt.Errorf("unfair nearest-%s distance: spread %d..%d = %v", name, lo, hi, values)
		}
		return nil
	}

	var relicD, mineD, oppD []int
	for _, cap := range capitals {
		relicD = append(relicD, nearest(cap.Coord, relics, Axial{}, false))
		mineD = append(mineD, nearest(cap.Coord, mines, Axial{}, false))
		oppD = append(oppD, nearest(cap.Coord, capitals, cap.Coord, true))
	}
	if err := check("relic", relicD); err != nil {
		return err
	}
	if err := check("mine", mineD); err != nil {
		return err
	}
	return check("opponent", oppD)
}
