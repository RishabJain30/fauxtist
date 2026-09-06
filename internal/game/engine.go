package game

import "math/rand"

// Engine owns and mutates the authoritative match State. It is pure and
// networking-independent — it has no notion of a socket, a timer, or wall-
// clock time — and is not safe for concurrent use: the owning Room goroutine
// serializes every call. The room drives phase transitions (on server
// deadlines), submits validated player commands, and reads copy-safe
// snapshots via State; the engine enforces every rule.
type Engine struct {
	state State
	rng   *rand.Rand
}

// NewEngine returns a lobby-phase engine seeded with its host. Seed drives
// only spawn assignment and map orientation at match start; it is never
// exposed as a client-controlled field.
func NewEngine(host Player, seed int64) *Engine {
	return &Engine{
		rng: rand.New(rand.NewSource(seed)),
		state: State{
			Phase:       PhaseLobby,
			Preset:      DefaultPreset,
			TotalRounds: PresetConfigFor(DefaultPreset).Rounds,
			Players:     []Player{host},
			HostID:      host.ID,
		},
	}
}

// State returns a deep, copy-safe snapshot of the match state.
func (e *Engine) State() State { return e.state.clone() }

// Phase returns the current phase.
func (e *Engine) Phase() Phase { return e.state.Phase }

// Preset returns the current match preset.
func (e *Engine) Preset() Preset { return e.state.Preset }

// ---- Lobby ----

// UpsertPlayer adds a new player during the lobby, or updates the name/emoji
// of an existing one in any phase (for reconnect metadata refreshes). A new
// player is rejected once the match has started or the roster is full.
func (e *Engine) UpsertPlayer(p Player) error {
	if i := e.state.playerIndex(p.ID); i >= 0 {
		if p.Name != "" {
			e.state.Players[i].Name = p.Name
		}
		if p.Emoji != "" {
			e.state.Players[i].Emoji = p.Emoji
		}
		return nil
	}
	if e.state.Phase != PhaseLobby {
		return ErrGameStarted
	}
	if len(e.state.Players) >= MaxPlayers {
		return ErrRoomFull
	}
	e.state.Players = append(e.state.Players, p)
	return nil
}

// RemovePlayer removes a player from the roster. Lobby-only: an in-match seat
// is handled by presence and resign rules, never removed outright.
func (e *Engine) RemovePlayer(id PlayerID) error {
	if e.state.Phase != PhaseLobby {
		return ErrNotInLobby
	}
	i := e.state.playerIndex(id)
	if i < 0 {
		return ErrUnknownPlayer
	}
	e.state.Players = append(e.state.Players[:i:i], e.state.Players[i+1:]...)
	e.ensureHostValid()
	return nil
}

// SetHostID transitions host ownership (deterministic host migration). The
// caller has already decided this is warranted; there is no permission check.
func (e *Engine) SetHostID(id PlayerID) error {
	if e.state.playerIndex(id) < 0 {
		return ErrUnknownPlayer
	}
	e.state.HostID = id
	return nil
}

// SetPreset chooses the match length/timing profile. Host-only, lobby-only.
func (e *Engine) SetPreset(by PlayerID, p Preset) error {
	if e.state.Phase != PhaseLobby {
		return ErrWrongPhase
	}
	if by != e.state.HostID {
		return ErrNotHost
	}
	if !ValidPreset(p) {
		return ErrInvalidPreset
	}
	e.state.Preset = p
	e.state.TotalRounds = PresetConfigFor(p).Rounds
	return nil
}

// ensureHostValid points HostID at a real player if the current host is gone,
// so the roster never references a missing host.
func (e *Engine) ensureHostValid() {
	if e.state.playerIndex(e.state.HostID) >= 0 {
		return
	}
	ids := e.state.SortedPlayerIDs()
	if len(ids) > 0 {
		e.state.HostID = ids[0]
	} else {
		e.state.HostID = ""
	}
}

// ---- Match start / rematch ----

// StartMatch begins a new match from the lobby. Host-only; requires 3–6
// players.
func (e *Engine) StartMatch(by PlayerID) error {
	if e.state.Phase != PhaseLobby {
		return ErrWrongPhase
	}
	if by != e.state.HostID {
		return ErrNotHost
	}
	return e.setupMatch()
}

// StartRematch begins a fresh match from the Game Over / rematch lobby with a
// new seed (new spawns and orientation). Host-only. Previously forfeited
// players are dropped from the roster — they must rejoin as a fresh seat.
func (e *Engine) StartRematch(by PlayerID, seed int64) error {
	if e.state.Phase != PhaseGameOver {
		return ErrWrongPhase
	}
	if by != e.state.HostID {
		return ErrNotHost
	}
	e.dropForfeited()
	e.ensureHostValid()
	e.rng = rand.New(rand.NewSource(seed))
	return e.setupMatch()
}

// ReturnToLobby resets the room to a normal lobby after a match, preserving
// the roster (minus permanently-resigned players) and clearing all
// match-private state. Host-only.
func (e *Engine) ReturnToLobby(by PlayerID) error {
	if e.state.Phase != PhaseGameOver {
		return ErrWrongPhase
	}
	if by != e.state.HostID {
		return ErrNotHost
	}
	e.dropForfeited()
	e.ensureHostValid()
	e.state.Phase = PhaseLobby
	e.state.Round = 0
	e.state.TotalRounds = PresetConfigFor(e.state.Preset).Rounds
	e.state.Tiles = nil
	e.state.MapID = ""
	e.state.Declarations = nil
	e.state.Orders = nil
	e.state.Resolution = nil
	e.state.Result = nil
	e.state.Stats = nil
	e.state.InfluenceHistory = nil
	e.state.pendingGameOver = false
	for i := range e.state.Players {
		e.resetPlayerMatchFields(&e.state.Players[i])
	}
	return nil
}

// dropForfeited removes permanently-resigned players from the roster.
func (e *Engine) dropForfeited() {
	kept := make([]Player, 0, len(e.state.Players))
	for _, p := range e.state.Players {
		if !p.Forfeited {
			kept = append(kept, p)
		}
	}
	e.state.Players = kept
}

func (e *Engine) resetPlayerMatchFields(p *Player) {
	p.Faction = ""
	p.SpawnSlot = 0
	p.Energy = 0
	p.Influence = 0
	p.FauxAvailable = false
	p.FauxUsedRound = 0
	p.DominationStreak = 0
	p.Forfeited = false
}

// setupMatch builds the board and initial player state for a new match, then
// enters the first round's INCOME phase. Uses the seeded RNG for spawn
// assignment and a random dihedral orientation of the authored map.
func (e *Engine) setupMatch() error {
	n := len(e.state.Players)
	if n < MinPlayers {
		return ErrTooFewPlayers
	}
	if n > MaxPlayers {
		return ErrTooManyPlayers
	}
	tmpl, ok := MapTemplateFor(n)
	if !ok {
		return ErrTooFewPlayers
	}

	// Random spawn assignment: player at index perm[i] takes slot i.
	perm := e.rng.Perm(n)
	rot := e.rng.Intn(6)
	mir := e.rng.Intn(2)
	transform := func(c Axial) Axial {
		for k := 0; k < rot; k++ {
			c = rot60(c)
		}
		if mir == 1 {
			c = mirrorAxial(c)
		}
		return c
	}

	slotPlayer := make(map[int]PlayerID, n)
	for i, pi := range perm {
		slotPlayer[i] = e.state.Players[pi].ID
		e.state.Players[pi].SpawnSlot = i
		e.state.Players[pi].Faction = FactionOrder[i]
	}

	e.state.Tiles = map[TileID]*Tile{}
	e.state.MapID = tmpl.ID
	capitals := map[int]TileID{}
	for _, td := range tmpl.Tiles {
		t := &Tile{ID: td.ID, Coord: transform(td.Coord), Type: td.Type, Structure: StructureNone}
		switch td.Type {
		case TileCapital:
			owner := slotPlayer[td.SpawnSlot]
			t.Owner = owner
			t.CapitalOwner = owner
			t.Armies = StartingCapitalArmies
			capitals[td.SpawnSlot] = td.ID
		case TileRelic:
			t.Armies = NeutralRelicGuardian
		}
		e.state.Tiles[t.ID] = t
	}

	// Seed each capital's adjacent starting territory: the nearest-to-centre
	// free normal neighbour, deterministically.
	for slot := 0; slot < n; slot++ {
		capID := capitals[slot]
		owner := slotPlayer[slot]
		var chosen TileID
		best := 1 << 30
		for _, tid := range e.state.SortedTileIDs() {
			t := e.state.Tiles[tid]
			if t.Type != TileNormal || t.Owner != "" || !e.state.adjacent(capID, tid) {
				continue
			}
			if d := HexDistance(Axial{}, t.Coord); d < best {
				best = d
				chosen = tid
			}
		}
		if chosen != "" {
			e.state.Tiles[chosen].Owner = owner
			e.state.Tiles[chosen].Armies = StartingAdjacentArmies
		}
	}

	e.state.Stats = map[PlayerID]*MatchStats{}
	e.state.InfluenceHistory = map[PlayerID][]int{}
	for i := range e.state.Players {
		p := &e.state.Players[i]
		p.Energy = StartingEnergy
		p.Influence = 0
		p.FauxAvailable = true
		p.FauxUsedRound = 0
		p.DominationStreak = 0
		p.Forfeited = false
		e.state.Stats[p.ID] = &MatchStats{}
		e.state.InfluenceHistory[p.ID] = nil
	}

	e.state.TotalRounds = PresetConfigFor(e.state.Preset).Rounds
	e.state.Round = 1
	e.state.Declarations = map[PlayerID]Declaration{}
	e.state.Orders = map[PlayerID]OrderSet{}
	e.state.Resolution = nil
	e.state.Result = nil
	e.state.pendingGameOver = false
	e.state.Phase = PhaseIncome
	return nil
}

// ---- Phase transitions (driven by the room's phase timers) ----

// ApplyIncome grants each active player their round income (from round two
// onward: base plus one per completed Mine, capped) and advances to
// NEGOTIATION. Income is applied exactly once per round.
func (e *Engine) ApplyIncome() error {
	if e.state.Phase != PhaseIncome {
		return ErrWrongPhase
	}
	if e.state.Round >= 2 {
		for i := range e.state.Players {
			p := &e.state.Players[i]
			if p.Forfeited {
				continue
			}
			gain := BaseIncome + e.minesControlledBy(p.ID)
			p.Energy += gain
			if p.Energy > EnergyCap {
				p.Energy = EnergyCap
			}
		}
	}
	e.state.Phase = PhaseNegotiation
	return nil
}

func (e *Engine) minesControlledBy(id PlayerID) int {
	n := 0
	for _, tid := range e.state.SortedTileIDs() {
		t := e.state.Tiles[tid]
		if t.Owner == id && t.Structure == StructureMine {
			n++
		}
	}
	return n
}

// BeginDeclaration advances NEGOTIATION → DECLARATION.
func (e *Engine) BeginDeclaration() error {
	if e.state.Phase != PhaseNegotiation {
		return ErrWrongPhase
	}
	e.state.Phase = PhaseDeclaration
	return nil
}

// SubmitDeclaration records a player's public-later declaration. It must be a
// legal, self-affordable command against the current board.
func (e *Engine) SubmitDeclaration(by PlayerID, cmd Command) error {
	if e.state.Phase != PhaseDeclaration {
		return ErrWrongPhase
	}
	p := e.state.player(by)
	if p == nil {
		return ErrUnknownPlayer
	}
	if p.Forfeited {
		return ErrForfeited
	}
	if err := e.state.validateSingleCommand(by, cmd); err != nil {
		return err
	}
	if cmd.EnergyCost() > p.Energy {
		return ErrNotEnoughEnergy
	}
	e.state.Declarations[by] = Declaration{Command: cmd, Submitted: true}
	return nil
}

// RevealDeclarations advances DECLARATION → DECLARATION_REVEAL, filling any
// missing declaration with an auto-Hold (which can never become Faux).
func (e *Engine) RevealDeclarations() error {
	if e.state.Phase != PhaseDeclaration {
		return ErrWrongPhase
	}
	for _, pid := range e.state.SortedPlayerIDs() {
		if e.state.player(pid).Forfeited {
			continue
		}
		if _, ok := e.state.Declarations[pid]; !ok {
			e.state.Declarations[pid] = Declaration{Command: HoldCommand(), Submitted: false}
		}
	}
	e.state.Phase = PhaseDeclarationReveal
	return nil
}

// BeginPlanning advances DECLARATION_REVEAL → SECRET_PLANNING.
func (e *Engine) BeginPlanning() error {
	if e.state.Phase != PhaseDeclarationReveal {
		return ErrWrongPhase
	}
	e.state.Phase = PhaseSecretPlanning
	return nil
}

// SetOrders atomically replaces a player's private planning draft: their
// hidden real commands and whether their public declaration is Faux. The
// whole set must be legal and affordable together.
func (e *Engine) SetOrders(by PlayerID, commands []Command, faux bool) error {
	if e.state.Phase != PhaseSecretPlanning {
		return ErrWrongPhase
	}
	p := e.state.player(by)
	if p == nil {
		return ErrUnknownPlayer
	}
	if p.Forfeited {
		return ErrForfeited
	}
	if o, ok := e.state.Orders[by]; ok && o.Locked {
		return ErrAlreadyLocked
	}
	decl, hasDecl := e.state.Declarations[by]
	if faux {
		if !p.FauxAvailable {
			return ErrFauxUnavailable
		}
		if !hasDecl || !decl.Submitted || decl.Command.Type == CmdHold {
			return ErrFauxOnHold
		}
	}
	if len(commands) > HiddenCommandCount(faux) {
		return ErrTooManyCommands
	}

	real := make([]Command, 0, RealCommandSlots)
	if !faux {
		if hasDecl && decl.Submitted && decl.Command.Type != CmdHold {
			real = append(real, decl.Command)
		} else {
			real = append(real, HoldCommand())
		}
	}
	real = append(real, commands...)
	if err := e.state.validateRealCommands(by, real); err != nil {
		return err
	}

	e.state.Orders[by] = OrderSet{
		Faux:      faux,
		Commands:  append([]Command(nil), commands...),
		Submitted: true,
		Locked:    false,
	}
	return nil
}

// LockOrders marks a player's draft final for this round. Locking with no
// submitted orders is allowed — missing slots become Hold at resolution.
func (e *Engine) LockOrders(by PlayerID) error {
	if e.state.Phase != PhaseSecretPlanning {
		return ErrWrongPhase
	}
	p := e.state.player(by)
	if p == nil {
		return ErrUnknownPlayer
	}
	if p.Forfeited {
		return ErrForfeited
	}
	o := e.state.Orders[by]
	o.Locked = true
	e.state.Orders[by] = o
	return nil
}

// UnlockOrders reopens a player's draft for editing during the active
// deadline. (The room refuses this once the all-locked countdown has begun.)
func (e *Engine) UnlockOrders(by PlayerID) error {
	if e.state.Phase != PhaseSecretPlanning {
		return ErrWrongPhase
	}
	p := e.state.player(by)
	if p == nil {
		return ErrUnknownPlayer
	}
	o := e.state.Orders[by]
	o.Locked = false
	e.state.Orders[by] = o
	return nil
}

// Resolve computes the whole round atomically and advances SECRET_PLANNING →
// RESOLUTION, returning the animation timeline the room broadcasts and the
// client animates.
func (e *Engine) Resolve() (Resolution, error) {
	if e.state.Phase != PhaseSecretPlanning {
		return Resolution{}, ErrWrongPhase
	}
	e.state.Phase = PhaseResolution
	return resolveRound(&e.state), nil
}

// BeginRoundSummary advances RESOLUTION → ROUND_SUMMARY.
func (e *Engine) BeginRoundSummary() error {
	if e.state.Phase != PhaseResolution {
		return ErrWrongPhase
	}
	e.state.Phase = PhaseRoundSummary
	return nil
}

// AdvanceRound advances ROUND_SUMMARY to the next round's INCOME phase, or to
// GAME_OVER if the match ended (Domination or the final round).
func (e *Engine) AdvanceRound() error {
	if e.state.Phase != PhaseRoundSummary {
		return ErrWrongPhase
	}
	if e.state.pendingGameOver {
		e.state.Phase = PhaseGameOver
		return nil
	}
	e.state.Round++
	e.state.Declarations = map[PlayerID]Declaration{}
	e.state.Orders = map[PlayerID]OrderSet{}
	e.state.Phase = PhaseIncome
	return nil
}

// ---- Resign ----

// Resign permanently removes a player from the active match: they forfeit,
// their pending declaration/orders are cancelled, and their territories are
// cleaned up (non-capital tiles go neutral, armies and structures removed,
// neutral relics regain a guardian, their capital goes inactive but stays
// untargetable). Idempotent. This never mutates the board mid-resolution — the
// room only calls it at a safe phase boundary.
func (e *Engine) Resign(by PlayerID) error {
	p := e.state.player(by)
	if p == nil {
		return ErrUnknownPlayer
	}
	if p.Forfeited {
		return nil
	}
	p.Forfeited = true
	delete(e.state.Declarations, by)
	delete(e.state.Orders, by)
	for _, tid := range e.state.SortedTileIDs() {
		t := e.state.Tiles[tid]
		if t.Owner != by {
			continue
		}
		if t.Type == TileCapital {
			t.Armies = 0 // inactive; still owned + untargetable
			continue
		}
		wasRelic := t.Type == TileRelic
		t.Owner = ""
		t.Structure = StructureNone
		if wasRelic {
			t.Armies = NeutralRelicGuardian
		} else {
			t.Armies = 0
		}
	}
	e.ensureHostValid()
	return nil
}

// EndForfeitIfAlone ends the match immediately (Forfeit, or No Contest) if
// only one (or zero) active player remains after a resign. Reports whether the
// match ended.
func (e *Engine) EndForfeitIfAlone() bool {
	if e.state.Phase == PhaseLobby || e.state.Phase == PhaseGameOver {
		return false
	}
	if e.state.endForfeitIfAlone() {
		e.state.Phase = PhaseGameOver
		return true
	}
	return false
}
