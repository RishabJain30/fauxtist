package game

import "testing"

// These tests validate every authored board template against the structural and
// fairness invariants the engine depends on at match start. They exercise the
// exported map API (AllMapTemplates, MapTemplateFor, Validate, ValidateAllMaps)
// plus the hex geometry helpers, and independently re-derive the counts and
// graph properties rather than trusting Validate alone.

func TestValidateAllMaps(t *testing.T) {
	if err := ValidateAllMaps(); err != nil {
		t.Fatalf("ValidateAllMaps() = %v, want nil", err)
	}
}

func TestAllMapTemplates_CoverEveryPlayerCount(t *testing.T) {
	got := AllMapTemplates()
	if len(got) != MaxPlayers-MinPlayers+1 {
		t.Fatalf("AllMapTemplates() returned %d templates, want %d", len(got), MaxPlayers-MinPlayers+1)
	}
	wantPC := MinPlayers
	for _, tmpl := range got {
		if tmpl.PlayerCount != wantPC {
			t.Fatalf("template order broken: got player count %d, want %d", tmpl.PlayerCount, wantPC)
		}
		wantPC++
	}
}

func TestMapTemplates_StructureAndCounts(t *testing.T) {
	for _, n := range []int{3, 4, 5, 6} {
		n := n
		t.Run(mapName(n), func(t *testing.T) {
			tmpl, ok := MapTemplateFor(n)
			if !ok {
				t.Fatalf("MapTemplateFor(%d) not found", n)
			}
			if err := tmpl.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}

			var capitals, relics, mines int
			ids := map[TileID]bool{}
			coords := map[Axial]bool{}
			slots := map[int]bool{}
			typeAt := map[Axial]TileType{}

			for _, td := range tmpl.Tiles {
				if ids[td.ID] {
					t.Fatalf("duplicate tile id %q", td.ID)
				}
				ids[td.ID] = true
				if coords[td.Coord] {
					t.Fatalf("duplicate coordinate %+v", td.Coord)
				}
				coords[td.Coord] = true
				typeAt[td.Coord] = td.Type

				switch td.Type {
				case TileCapital:
					capitals++
					if td.SpawnSlot < 0 || td.SpawnSlot >= n {
						t.Fatalf("capital %q spawn slot %d out of range [0,%d)", td.ID, td.SpawnSlot, n)
					}
					if slots[td.SpawnSlot] {
						t.Fatalf("duplicate spawn slot %d", td.SpawnSlot)
					}
					slots[td.SpawnSlot] = true
				case TileRelic:
					relics++
				case TileMineSite:
					mines++
				}
			}

			if capitals != n {
				t.Fatalf("got %d capitals, want %d", capitals, n)
			}
			if len(slots) != n {
				t.Fatalf("got %d distinct spawn slots, want %d (0..%d)", len(slots), n, n-1)
			}
			for slot := 0; slot < n; slot++ {
				if !slots[slot] {
					t.Fatalf("missing spawn slot %d", slot)
				}
			}
			if relics != RelicCount {
				t.Fatalf("got %d relics, want %d", relics, RelicCount)
			}
			if mines != n+1 {
				t.Fatalf("got %d mine sites, want %d", mines, n+1)
			}

			// Graph connectivity via BFS over the coordinate set using Neighbors.
			set := tmpl.coordSet()
			seen := map[Axial]bool{}
			start := tmpl.Tiles[0].Coord
			queue := []Axial{start}
			seen[start] = true
			for len(queue) > 0 {
				c := queue[0]
				queue = queue[1:]
				for _, nb := range Neighbors(c) {
					if set[nb] && !seen[nb] {
						seen[nb] = true
						queue = append(queue, nb)
					}
				}
			}
			if len(seen) != len(tmpl.Tiles) {
				t.Fatalf("map not connected: reached %d of %d tiles", len(seen), len(tmpl.Tiles))
			}

			// Every capital has at least one adjacent normal tile.
			for _, td := range tmpl.Tiles {
				if td.Type != TileCapital {
					continue
				}
				hasNormal := false
				for _, nb := range Neighbors(td.Coord) {
					if typeAt[nb] == TileNormal {
						hasNormal = true
						break
					}
				}
				if !hasNormal {
					t.Fatalf("capital %q has no adjacent normal tile", td.ID)
				}
			}
		})
	}
}

func TestMapTemplates_SpawnFairness(t *testing.T) {
	for _, n := range []int{3, 4, 5, 6} {
		n := n
		t.Run(mapName(n), func(t *testing.T) {
			tmpl, ok := MapTemplateFor(n)
			if !ok {
				t.Fatalf("MapTemplateFor(%d) not found", n)
			}
			var capitals, relics, mines []TileDef
			for _, td := range tmpl.Tiles {
				switch td.Type {
				case TileCapital:
					capitals = append(capitals, td)
				case TileRelic:
					relics = append(relics, td)
				case TileMineSite:
					mines = append(mines, td)
				}
			}

			nearest := func(from Axial, ts []TileDef, self Axial, skipSelf bool) int {
				best := -1
				for _, t := range ts {
					if skipSelf && t.Coord == self {
						continue
					}
					d := HexDistance(from, t.Coord)
					if best < 0 || d < best {
						best = d
					}
				}
				return best
			}
			spread := func(vs []int) int {
				lo, hi := vs[0], vs[0]
				for _, v := range vs {
					if v < lo {
						lo = v
					}
					if v > hi {
						hi = v
					}
				}
				return hi - lo
			}

			var relicD, mineD, capD []int
			for _, cap := range capitals {
				relicD = append(relicD, nearest(cap.Coord, relics, Axial{}, false))
				mineD = append(mineD, nearest(cap.Coord, mines, Axial{}, false))
				capD = append(capD, nearest(cap.Coord, capitals, cap.Coord, true))
			}
			if s := spread(relicD); s > 1 {
				t.Fatalf("nearest-relic distance spread %d > 1: %v", s, relicD)
			}
			if s := spread(mineD); s > 1 {
				t.Fatalf("nearest-mine distance spread %d > 1: %v", s, mineD)
			}
			if s := spread(capD); s > 1 {
				t.Fatalf("nearest-opponent-capital distance spread %d > 1: %v", s, capD)
			}
		})
	}
}

func TestMapTemplates_ClosedUnderRotationAndMirror(t *testing.T) {
	for _, n := range []int{3, 4, 5, 6} {
		n := n
		t.Run(mapName(n), func(t *testing.T) {
			tmpl, ok := MapTemplateFor(n)
			if !ok {
				t.Fatalf("MapTemplateFor(%d) not found", n)
			}
			set := tmpl.coordSet()
			for c := range set {
				if !set[rot60(c)] {
					t.Fatalf("field not closed under rot60 at %+v (rot60 -> %+v missing)", c, rot60(c))
				}
				if !set[mirrorAxial(c)] {
					t.Fatalf("field not closed under mirror at %+v (mirror -> %+v missing)", c, mirrorAxial(c))
				}
			}
		})
	}
}

func TestHexDistance_Sanity(t *testing.T) {
	origin := Axial{Q: 0, R: 0}
	if d := HexDistance(origin, origin); d != 0 {
		t.Fatalf("HexDistance(self) = %d, want 0", d)
	}
	for _, nb := range Neighbors(origin) {
		if d := HexDistance(origin, nb); d != 1 {
			t.Fatalf("HexDistance(origin, neighbor %+v) = %d, want 1", nb, d)
		}
	}
	if d := HexDistance(Axial{Q: 0, R: 0}, Axial{Q: 2, R: 0}); d != 2 {
		t.Fatalf("HexDistance((0,0),(2,0)) = %d, want 2", d)
	}
}

func TestNeighbors_AllDistinctAndAdjacent(t *testing.T) {
	origin := Axial{Q: 0, R: 0}
	nbs := Neighbors(origin)
	if len(nbs) != 6 {
		t.Fatalf("Neighbors returned %d coords, want 6", len(nbs))
	}
	seen := map[Axial]bool{}
	for _, nb := range nbs {
		if seen[nb] {
			t.Fatalf("Neighbors returned duplicate %+v", nb)
		}
		seen[nb] = true
	}
}

func mapName(n int) string {
	switch n {
	case 3:
		return "atlas-3"
	case 4:
		return "atlas-4"
	case 5:
		return "atlas-5"
	case 6:
		return "atlas-6"
	default:
		return "unknown"
	}
}
