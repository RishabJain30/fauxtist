package room

import (
	"encoding/json"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func parsePayload(raw json.RawMessage, v any) error {
	return json.Unmarshal(raw, v)
}

// playerViews merges each engine player with room-tracked presence, readiness,
// and AFK — everything public about a player. Never carries private draft data.
func (r *Room) playerViews() []wsproto.PlayerView {
	players := r.engine.State().Players
	views := make([]wsproto.PlayerView, 0, len(players))
	for _, p := range players {
		connected := false
		if pres, ok := r.presence[p.ID]; ok {
			connected = pres.connected
		}
		views = append(views, wsproto.PlayerView{
			ID:               string(p.ID),
			Name:             p.Name,
			Emoji:            p.Emoji,
			Faction:          string(p.Faction),
			SpawnSlot:        p.SpawnSlot,
			Energy:           p.Energy,
			Influence:        p.Influence,
			FauxAvailable:    p.FauxAvailable,
			FauxUsedRound:    p.FauxUsedRound,
			DominationStreak: p.DominationStreak,
			Forfeited:        p.Forfeited,
			Connected:        connected,
			Ready:            r.ready[p.ID],
			AFK:              r.afk[p.ID],
		})
	}
	return views
}

func (r *Room) spectatorViews() []wsproto.SpectatorView {
	out := make([]wsproto.SpectatorView, 0, len(r.specViews))
	for id, info := range r.specViews {
		_, connected := r.spectators[id]
		out = append(out, wsproto.SpectatorView{
			ID:        string(id),
			Name:      info.name,
			Emoji:     info.emoji,
			Connected: connected,
		})
	}
	return out
}

// declarationProgress reports how many required players have submitted a
// declaration this round, and how many are required — an aggregate that leaks
// no individual's choice.
func (r *Room) declarationProgress() (submitted, required int) {
	s := r.engine.State()
	for _, id := range r.requiredPlayers() {
		required++
		if d, ok := s.Declarations[id]; ok && d.Submitted {
			submitted++
		}
	}
	return submitted, required
}

// planningProgress reports how many required players have submitted orders and
// how many have locked, plus how many are required.
func (r *Room) planningProgress() (submitted, locked, required int) {
	s := r.engine.State()
	for _, id := range r.requiredPlayers() {
		required++
		if o, ok := s.Orders[id]; ok {
			if o.Submitted {
				submitted++
			}
			if o.Locked {
				locked++
			}
		}
	}
	return submitted, locked, required
}

// revealedDecl is one publicly-revealed declaration. It carries the declared
// command only — never whether it is Faux, which stays secret until
// resolution.
type revealedDecl struct {
	Player  string       `json:"player"`
	Command game.Command `json:"command"`
}

func (r *Room) revealedDeclarations() []revealedDecl {
	s := r.engine.State()
	out := make([]revealedDecl, 0, len(s.Declarations))
	for _, pid := range s.SortedPlayerIDs() {
		if d, ok := s.Declarations[pid]; ok {
			out = append(out, revealedDecl{Player: string(pid), Command: d.Command})
		}
	}
	return out
}

// declarationsArepublic reports whether opponents' declared commands are
// visible this phase (from the reveal through the round summary).
func declarationsArePublic(phase game.Phase) bool {
	switch phase {
	case game.PhaseDeclarationReveal, game.PhaseSecretPlanning, game.PhaseResolution, game.PhaseRoundSummary:
		return true
	default:
		return false
	}
}

// snapshotPayload is the canonical per-viewer state_snapshot. Optional fields
// are omitted when not relevant to the current phase or viewer. It never
// contains another player's unrevealed declaration, hidden orders, or Faux
// selection.
type snapshotPayload struct {
	Phase           string                  `json:"phase"`
	PhaseDeadlineMs int64                   `json:"phaseDeadlineMs,omitempty"`
	EarlyDeadlineMs int64                   `json:"earlyDeadlineMs,omitempty"`
	Paused          bool                    `json:"paused"`
	Round           int                     `json:"round"`
	TotalRounds     int                     `json:"totalRounds"`
	Preset          string                  `json:"preset"`
	MapID           string                  `json:"mapId,omitempty"`
	HostID          string                  `json:"hostId,omitempty"`
	Role            string                  `json:"role"`
	Me              string                  `json:"me,omitempty"`
	You             *wsproto.PlayerView     `json:"you,omitempty"`
	Players         []wsproto.PlayerView    `json:"players"`
	Spectators      []wsproto.SpectatorView `json:"spectators"`
	Board           []game.Tile             `json:"board,omitempty"`
	Chat            []chatEntry             `json:"chat"`

	MyDeclaration        *game.Command  `json:"myDeclaration,omitempty"`
	MyOrders             *game.OrderSet `json:"myOrders,omitempty"`
	DeclarationsIn       int            `json:"declarationsIn"`
	OrdersSubmitted      int            `json:"ordersSubmitted"`
	OrdersLocked         int            `json:"ordersLocked"`
	RequiredCount        int            `json:"requiredCount"`
	RevealedDeclarations []revealedDecl `json:"revealedDeclarations,omitempty"`

	Resolution   *game.Resolution `json:"resolution,omitempty"`
	Result       *game.GameResult `json:"result,omitempty"`
	RematchReady []string         `json:"rematchReady,omitempty"`
}

// buildSnapshot assembles the canonical, per-viewer redacted snapshot. It is
// the single authoritative builder so redaction can never drift between join,
// reconnect, resync, and the match-start/game-over broadcasts.
func (r *Room) buildSnapshot(viewer game.PlayerID, spectator bool) snapshotPayload {
	s := r.engine.State()
	snap := snapshotPayload{
		Phase:       string(s.Phase),
		Paused:      r.paused,
		Round:       s.Round,
		TotalRounds: s.TotalRounds,
		Preset:      string(s.Preset),
		MapID:       s.MapID,
		HostID:      string(s.HostID),
		Me:          string(viewer),
		Players:     r.playerViews(),
		Spectators:  r.spectatorViews(),
		Chat:        append([]chatEntry(nil), r.chatHistory...),
		Role:        "player",
	}
	if spectator {
		snap.Role = "spectator"
	}
	if !r.paused && !r.phaseDeadline.IsZero() {
		snap.PhaseDeadlineMs = r.phaseDeadline.UnixMilli()
	}
	if r.earlyCountdownActive && !r.earlyDeadline.IsZero() {
		snap.EarlyDeadlineMs = r.earlyDeadline.UnixMilli()
	}

	// Board (public).
	if len(s.Tiles) > 0 {
		board := make([]game.Tile, 0, len(s.Tiles))
		for _, tid := range s.SortedTileIDs() {
			board = append(board, *s.Tiles[tid])
		}
		snap.Board = board
	}

	// Viewer's own player view.
	if !spectator {
		for i := range snap.Players {
			if snap.Players[i].ID == string(viewer) {
				pv := snap.Players[i]
				snap.You = &pv
				break
			}
		}
	}

	// Aggregate completion (no individual leak).
	snap.DeclarationsIn, snap.RequiredCount = r.declarationProgress()
	subm, locked, _ := r.planningProgress()
	snap.OrdersSubmitted, snap.OrdersLocked = subm, locked

	// Viewer's OWN private draft only.
	if !spectator {
		if d, ok := s.Declarations[viewer]; ok {
			cmd := d.Command
			snap.MyDeclaration = &cmd
		}
		if o, ok := s.Orders[viewer]; ok {
			oc := o
			snap.MyOrders = &oc
		}
	}

	// Publicly revealed declarations (command only, never the Faux flag).
	if declarationsArePublic(s.Phase) {
		snap.RevealedDeclarations = r.revealedDeclarations()
	}

	// Results.
	if s.Phase == game.PhaseResolution || s.Phase == game.PhaseRoundSummary {
		snap.Resolution = s.Resolution
	}
	if s.Phase == game.PhaseGameOver {
		snap.Result = s.Result
		ready := make([]string, 0, len(r.rematchOK))
		for id, ok := range r.rematchOK {
			if ok {
				ready = append(ready, string(id))
			}
		}
		snap.RematchReady = ready
	}
	return snap
}

// sendSnapshotTo sends one connection its full redacted snapshot (join,
// reconnect, resync). Unsequenced-in-effect: it does not bump the revision, and
// a snapshot is always safe for the client to fully replace state with.
func (r *Room) sendSnapshotTo(c *Client) {
	snap := r.buildSnapshot(c.PlayerID, c.Spectator)
	if env, err := wsproto.Encode(wsproto.TypeStateSnapshot, snap); err == nil {
		c.trySend(r.stamp(env))
	}
}

// broadcastSnapshotToAll bumps the revision once and delivers every connected
// recipient their own redacted snapshot at that revision — used on wholesale
// board changes (match start, resign cleanup, game over, back to lobby).
func (r *Room) broadcastSnapshotToAll() {
	r.revision++
	for id, c := range r.clients {
		snap := r.buildSnapshot(id, false)
		if env, err := wsproto.Encode(wsproto.TypeStateSnapshot, snap); err == nil {
			c.trySend(r.stamp(env))
		}
	}
	for id, c := range r.spectators {
		snap := r.buildSnapshot(id, true)
		if env, err := wsproto.Encode(wsproto.TypeStateSnapshot, snap); err == nil {
			c.trySend(r.stamp(env))
		}
	}
}
