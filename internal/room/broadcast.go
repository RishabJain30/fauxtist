package room

import (
	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// stamp fills in the fields only the room knows: the public room id and the
// current revision (stamped as Seq so every recipient of one transition
// observes the same number even with differently-redacted payloads).
func (r *Room) stamp(env wsproto.Envelope) wsproto.Envelope {
	env.RoomID = r.Code
	env.Seq = r.revision
	return env
}

// clientByID finds a connection whether it belongs to an active player or a
// spectator.
func (r *Room) clientByID(id game.PlayerID) *Client {
	if c, ok := r.clients[id]; ok {
		return c
	}
	if c, ok := r.spectators[id]; ok {
		return c
	}
	return nil
}

// broadcast sends an unsequenced envelope to every active-player client.
func (r *Room) broadcast(env wsproto.Envelope) {
	env = r.stamp(env)
	for _, c := range r.clients {
		c.trySend(env)
	}
}

// broadcastToSpectators sends an unsequenced envelope to every spectator.
func (r *Room) broadcastToSpectators(env wsproto.Envelope) {
	env = r.stamp(env)
	for _, c := range r.spectators {
		c.trySend(env)
	}
}

// broadcastSequenced bumps the revision once, then delivers the same public
// envelope to every connected recipient — active players and spectators alike
// — at that revision. This is the room's sequencing rule for every public
// game/lifecycle message that shares one envelope: exactly one bump per
// distinct message, never a batch pre-bump, so two back-to-back messages never
// share a seq and the revision never skips a number nothing was sent at.
func (r *Room) broadcastSequenced(env wsproto.Envelope) {
	r.revision++
	env = r.stamp(env)
	for _, c := range r.clients {
		c.trySend(env)
	}
	for _, c := range r.spectators {
		c.trySend(env)
	}
}

// sendTo sends one stamped (unsequenced) envelope to a single connection.
func (r *Room) sendTo(id game.PlayerID, env wsproto.Envelope) {
	if c := r.clientByID(id); c != nil {
		c.trySend(r.stamp(env))
	}
}

// sendError sends a typed, machine-readable error to one connection, echoing
// the offending command's requestId for client-side correlation.
func (r *Room) sendError(id game.PlayerID, requestID, code, msg string) {
	env, err := wsproto.Encode(wsproto.TypeError, wsproto.ErrorPayload{Message: msg, Code: code})
	if err == nil {
		env.RequestID = requestID
		r.sendTo(id, env)
	}
}

// broadcastExcept sends to every active client except one (voice fan-out).
func (r *Room) broadcastExcept(except game.PlayerID, env wsproto.Envelope) {
	env = r.stamp(env)
	for id, c := range r.clients {
		if id != except {
			c.trySend(env)
		}
	}
}

// sendLeaveAccepted acknowledges an explicit leave so the client can tear down
// cleanly.
func (r *Room) sendLeaveAccepted(c *Client) {
	if env, err := wsproto.Encode(wsproto.TypeLeaveAccepted, map[string]any{}); err == nil {
		c.trySend(r.stamp(env))
	}
}

// sendOrdersSaved privately, unsequenced, confirms a player's accepted draft.
func (r *Room) sendOrdersSaved(c *Client) {
	s := r.engine.State()
	o := s.Orders[c.PlayerID]
	env, err := wsproto.Encode(wsproto.TypeOrdersSaved, map[string]any{
		"faux":     o.Faux,
		"commands": o.Commands,
		"locked":   o.Locked,
	})
	if err == nil {
		c.trySend(r.stamp(env))
	}
}

// ---- Public sequenced game/lifecycle broadcasts ----

func (r *Room) broadcastLobby() {
	s := r.engine.State()
	env, err := wsproto.Encode(wsproto.TypeLobbyUpdate, map[string]any{
		"players":     r.playerViews(),
		"spectators":  r.spectatorViews(),
		"hostId":      string(s.HostID),
		"preset":      string(s.Preset),
		"totalRounds": s.TotalRounds,
	})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastSpectatorUpdate() {
	env, err := wsproto.Encode(wsproto.TypeSpectatorUpdate, map[string]any{
		"spectators": r.spectatorViews(),
	})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastSettingsChanged() {
	s := r.engine.State()
	env, err := wsproto.Encode(wsproto.TypeSettingsChanged, map[string]any{
		"preset":      string(s.Preset),
		"totalRounds": s.TotalRounds,
	})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastPlayerExited(id game.PlayerID, forfeited bool) {
	env, err := wsproto.Encode(wsproto.TypePlayerExited, wsproto.PlayerExitedPayload{ID: string(id), Forfeited: forfeited})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastPhaseChanged() {
	s := r.engine.State()
	payload := map[string]any{
		"phase":       string(s.Phase),
		"round":       s.Round,
		"totalRounds": s.TotalRounds,
		"paused":      r.paused,
	}
	if !r.paused && !r.phaseDeadline.IsZero() {
		payload["phaseDeadlineMs"] = r.phaseDeadline.UnixMilli()
	}
	env, err := wsproto.Encode(wsproto.TypePhaseChanged, payload)
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastDeclarationStatus() {
	submitted, required := r.declarationProgress()
	payload := map[string]any{"submitted": submitted, "required": required}
	if r.earlyCountdownActive && !r.earlyDeadline.IsZero() {
		payload["earlyDeadlineMs"] = r.earlyDeadline.UnixMilli()
	}
	env, err := wsproto.Encode(wsproto.TypeDeclarationStatus, payload)
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastPlanningStatus() {
	submitted, locked, required := r.planningProgress()
	payload := map[string]any{"submitted": submitted, "locked": locked, "required": required}
	if r.earlyCountdownActive && !r.earlyDeadline.IsZero() {
		payload["earlyDeadlineMs"] = r.earlyDeadline.UnixMilli()
	}
	env, err := wsproto.Encode(wsproto.TypePlanningStatus, payload)
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastDeclarationsRevealed() {
	env, err := wsproto.Encode(wsproto.TypeDeclarationsRevealed, map[string]any{
		"declarations": r.revealedDeclarations(),
	})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastRoundResolved(res game.Resolution) {
	env, err := wsproto.Encode(wsproto.TypeRoundResolved, map[string]any{"resolution": res})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastRoundSummary() {
	s := r.engine.State()
	if s.Resolution == nil {
		return
	}
	env, err := wsproto.Encode(wsproto.TypeRoundSummary, map[string]any{"summary": s.Resolution.Summary})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastGameOver() {
	s := r.engine.State()
	if s.Result == nil {
		return
	}
	env, err := wsproto.Encode(wsproto.TypeGameOver, map[string]any{"result": s.Result})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastRematchStatus() {
	ready := make([]string, 0, len(r.rematchOK))
	for id, ok := range r.rematchOK {
		if ok {
			ready = append(ready, string(id))
		}
	}
	env, err := wsproto.Encode(wsproto.TypeRematchStatus, map[string]any{"ready": ready})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

// ---- Voice (unsequenced peer-to-peer signalling relay) ----

func (r *Room) voiceJoin(from game.PlayerID) {
	if _, ok := r.clients[from]; !ok {
		return // spectators have no player voice
	}
	others := []string{}
	for id := range r.voicePresent {
		if id != from {
			others = append(others, string(id))
		}
	}
	r.voicePresent[from] = true
	if env, err := wsproto.Encode(wsproto.TypeVoicePeers, map[string]any{"ids": others}); err == nil {
		r.sendTo(from, env)
	}
	if env, err := wsproto.Encode(wsproto.TypeVoicePeerJoined, map[string]any{"id": string(from)}); err == nil {
		r.broadcastExcept(from, env)
	}
}

func (r *Room) voiceLeave(from game.PlayerID) {
	if !r.voicePresent[from] {
		return
	}
	delete(r.voicePresent, from)
	r.broadcastVoicePeerLeft(from)
}

func (r *Room) broadcastVoicePeerLeft(id game.PlayerID) {
	if env, err := wsproto.Encode(wsproto.TypeVoicePeerLeft, map[string]any{"id": string(id)}); err == nil {
		r.broadcastExcept(id, env)
	}
}

func (r *Room) handleVoiceSignal(c *Client, msg inbound) {
	var p wsproto.VoiceSignalIn
	if err := parsePayload(msg.envelope.Payload, &p); err != nil {
		return
	}
	if err := validateVoiceSignal(p, c.PlayerID, r.connectedSet()); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_voice_signal", err.Error())
		return
	}
	env, err := wsproto.Encode(wsproto.TypeVoiceSignal, map[string]any{
		"from": string(c.PlayerID), "kind": p.Kind, "payload": p.Payload,
	})
	if err == nil {
		r.sendTo(game.PlayerID(p.To), env)
	}
}

func (r *Room) handleVoiceState(c *Client, msg inbound) {
	var p wsproto.VoiceStateIn
	if err := parsePayload(msg.envelope.Payload, &p); err != nil {
		return
	}
	env, err := wsproto.Encode(wsproto.TypeVoiceState, map[string]any{
		"id": string(c.PlayerID), "muted": p.Muted, "speaking": p.Speaking,
	})
	if err == nil {
		r.broadcastExcept(c.PlayerID, env)
	}
}
