package room

import (
	"encoding/json"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// cmdFromWire converts a wire command to a validated-type game.Command. It
// only checks the type enum; full legality is the engine's job.
func cmdFromWire(w wsproto.CommandWire) (game.Command, bool) {
	t := game.CommandType(w.Type)
	switch t {
	case game.CmdMarch, game.CmdFortify, game.CmdRecruit, game.CmdBuildFortress, game.CmdBuildMine, game.CmdHold:
		return game.Command{Type: t, From: game.TileID(w.From), To: game.TileID(w.To), Armies: w.Armies}, true
	default:
		return game.Command{}, false
	}
}

func (r *Room) handleSetReady(c *Client, msg inbound) {
	if r.engine.Phase() != game.PhaseLobby {
		return
	}
	var p wsproto.SetReadyPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		return
	}
	r.ready[c.PlayerID] = p.Ready
	r.broadcastLobby()
}

func (r *Room) handleUpdateSettings(c *Client, msg inbound) {
	var p wsproto.UpdateSettingsPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "bad_payload", "bad settings payload")
		return
	}
	if err := r.engine.SetPreset(c.PlayerID, game.Preset(p.Preset)); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_settings", err.Error())
		return
	}
	// Changing settings clears every player's ready state.
	for id := range r.ready {
		r.ready[id] = false
	}
	r.broadcastSettingsChanged()
	r.broadcastLobby()
}

func (r *Room) handleStartMatch(c *Client, msg inbound) {
	s := r.engine.State()
	if s.HostID != c.PlayerID {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "not_host", "only the host can start the match")
		return
	}
	if len(s.Players) < game.MinPlayers {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "not_enough_players", "need at least three players")
		return
	}
	// Every connected human must be ready.
	for id := range r.clients {
		if !r.ready[id] {
			r.sendError(c.PlayerID, msg.envelope.RequestID, "not_all_ready", "all connected players must be ready")
			return
		}
	}
	if err := r.engine.StartMatch(c.PlayerID); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_start", err.Error())
		return
	}
	for id := range r.interacted {
		delete(r.interacted, id)
	}
	r.beginMatch()
}

func (r *Room) handleSubmitDecl(c *Client, msg inbound) {
	var p wsproto.SubmitDeclPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "bad_payload", "bad declaration payload")
		return
	}
	cmd, ok := cmdFromWire(p.Command)
	if !ok {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_command", "unknown command type")
		return
	}
	if err := r.engine.SubmitDeclaration(c.PlayerID, cmd); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_declaration", err.Error())
		return
	}
	r.clearAFK(c.PlayerID)
	r.broadcastDeclarationStatus()
	r.checkEarlyCompletion()
}

func (r *Room) handleSetOrders(c *Client, msg inbound) {
	var p wsproto.SetOrdersPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "bad_payload", "bad orders payload")
		return
	}
	cmds := make([]game.Command, 0, len(p.Commands))
	for _, w := range p.Commands {
		cmd, ok := cmdFromWire(w)
		if !ok {
			r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_command", "unknown command type")
			return
		}
		cmds = append(cmds, cmd)
	}
	if err := r.engine.SetOrders(c.PlayerID, cmds, p.Faux); err != nil {
		// Private, unsequenced validation error.
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_orders", err.Error())
		return
	}
	r.clearAFK(c.PlayerID)
	r.sendOrdersSaved(c) // private, unsequenced ack
	r.broadcastPlanningStatus()
}

func (r *Room) handleLockOrders(c *Client, msg inbound) {
	if err := r.engine.LockOrders(c.PlayerID); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_lock", err.Error())
		return
	}
	r.clearAFK(c.PlayerID)
	r.broadcastPlanningStatus()
	r.checkEarlyCompletion()
}

func (r *Room) handleUnlockOrders(c *Client, msg inbound) {
	if r.earlyCountdownActive {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "locked_countdown", "the all-locked countdown has begun; orders are final")
		return
	}
	if err := r.engine.UnlockOrders(c.PlayerID); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_unlock", err.Error())
		return
	}
	r.clearAFK(c.PlayerID)
	r.broadcastPlanningStatus()
}

func (r *Room) handleMapPing(c *Client, msg inbound) {
	var p wsproto.MapPingPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		return
	}
	if _, ok := r.engine.State().Tiles[game.TileID(p.Tile)]; !ok {
		return
	}
	r.clearAFK(c.PlayerID)
	env, err := wsproto.Encode(wsproto.TypeMapPing, map[string]any{"from": string(c.PlayerID), "tile": p.Tile})
	if err == nil {
		r.broadcast(env) // unsequenced
	}
}

func (r *Room) handleProposalArrow(c *Client, msg inbound) {
	if r.engine.Phase() != game.PhaseNegotiation {
		return
	}
	var p wsproto.ProposalArrowPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		return
	}
	tiles := r.engine.State().Tiles
	if _, ok := tiles[game.TileID(p.From)]; !ok {
		return
	}
	if _, ok := tiles[game.TileID(p.To)]; !ok {
		return
	}
	r.clearAFK(c.PlayerID)
	env, err := wsproto.Encode(wsproto.TypeProposalArrow, map[string]any{
		"from": string(c.PlayerID), "fromTile": p.From, "toTile": p.To,
	})
	if err == nil {
		r.broadcast(env) // unsequenced
	}
}

func (r *Room) handleChat(c *Client, msg inbound) {
	var p wsproto.ChatPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		return
	}
	text, err := validateChatText(p.Text)
	if err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_chat", err.Error())
		return
	}
	entry := chatEntry{From: string(c.PlayerID), Name: c.Name, Text: text}
	r.chatHistory = append(r.chatHistory, entry)
	if len(r.chatHistory) > maxChatHistory {
		r.chatHistory = r.chatHistory[len(r.chatHistory)-maxChatHistory:]
	}
	env, encErr := wsproto.Encode(wsproto.TypeChatBroadcast, entry)
	if encErr == nil {
		r.broadcast(env)             // active players
		r.broadcastToSpectators(env) // spectators can read public chat
	}
}

func (r *Room) handleLeaveForNow(c *Client, msg inbound) {
	// Leave for now keeps the seat and credential; it is just an early
	// disconnect. Their faction auto-Holds via the phase deadline.
	r.sendLeaveAccepted(c)
	c.close(closeNormal, "left for now")
}

func (r *Room) handleResign(c *Client, msg inbound) {
	if err := r.engine.Resign(c.PlayerID); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_resign", err.Error())
		return
	}
	delete(r.seats, c.PlayerID) // invalidate the reconnect credential
	delete(r.ready, c.PlayerID)
	delete(r.presence, c.PlayerID)
	if t, ok := r.graceTimers[c.PlayerID]; ok {
		t.Stop()
		delete(r.graceTimers, c.PlayerID)
	}
	if r.voicePresent[c.PlayerID] {
		delete(r.voicePresent, c.PlayerID)
		r.broadcastVoicePeerLeft(c.PlayerID)
	}
	r.broadcastPlayerExited(c.PlayerID, true)
	r.maybeMigrateHost()
	r.sendLeaveAccepted(c)
	c.close(closeNormal, "resigned")

	if r.engine.EndForfeitIfAlone() {
		r.endMatchNow()
		return
	}
	// The board changed (their territories went neutral): resync everyone.
	if r.engine.Phase() != game.PhaseLobby {
		r.broadcastSnapshotToAll()
	} else {
		r.broadcastLobby()
	}
}

func (r *Room) handleEndNoContest(c *Client, msg inbound) {
	if r.connectedActiveCount() > 1 {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "others_present", "other players are still here")
		return
	}
	if err := r.engine.EndNoContest(); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_end", err.Error())
		return
	}
	r.endMatchNow()
}

func (r *Room) handleKeepWaiting(c *Client, msg inbound) {
	// Purely a client-side dismissal of the solo prompt; nothing to change
	// server-side. Acknowledged implicitly by taking no destructive action.
}

func (r *Room) handleRematchReady(c *Client, msg inbound) {
	if r.engine.Phase() != game.PhaseGameOver {
		return
	}
	r.rematchOK[c.PlayerID] = !r.rematchOK[c.PlayerID]
	r.broadcastRematchStatus()
}

func (r *Room) handleStartRematch(c *Client, msg inbound) {
	s := r.engine.State()
	if s.Phase != game.PhaseGameOver {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "wrong_phase", "no match to rematch")
		return
	}
	if s.HostID != c.PlayerID {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "not_host", "only the host can start a rematch")
		return
	}
	// Count connected, non-forfeited, ready players.
	active := 0
	for id := range r.clients {
		if p := s.PlayerByID(id); p != nil && !p.Forfeited && r.rematchOK[id] {
			active++
		}
	}
	if active < game.MinPlayers {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "not_enough_ready", "need at least three ready players")
		return
	}
	r.rematchCount++
	newSeed := r.seed + int64(r.rematchCount)*2654435761
	if err := r.engine.StartRematch(c.PlayerID, newSeed); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_rematch", err.Error())
		return
	}
	for id := range r.interacted {
		delete(r.interacted, id)
	}
	r.beginMatch()
}

func (r *Room) handleReturnToLobby(c *Client, msg inbound) {
	s := r.engine.State()
	if s.Phase != game.PhaseGameOver {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "wrong_phase", "no match to leave")
		return
	}
	if s.HostID != c.PlayerID {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "not_host", "only the host can return to the lobby")
		return
	}
	if err := r.engine.ReturnToLobby(c.PlayerID); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "cannot_return", err.Error())
		return
	}
	r.stopPhaseTimer()
	r.stopEarlyTimer()
	r.earlyCountdownActive = false
	r.paused = false
	r.ready = map[game.PlayerID]bool{}
	r.rematchOK = map[game.PlayerID]bool{}
	r.matchGen++
	r.broadcastSnapshotToAll()
}

func (r *Room) handleClaimSeat(c *Client, msg inbound) {
	if r.engine.Phase() != game.PhaseLobby {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "wrong_phase", "seats can only be claimed in the lobby")
		return
	}
	var p wsproto.ClaimSeatPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "bad_payload", "bad claim payload")
		return
	}
	name, verr := ValidatePlayerName(p.Name)
	if verr != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "invalid_name", "invalid name")
		return
	}
	emoji, _ := validateEmoji(p.Emoji)
	playerID, perr := identity.NewPlayerID()
	if perr != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "internal_error", "could not claim seat")
		return
	}
	token, terr := identity.NewReconnectToken()
	if terr != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "internal_error", "could not claim seat")
		return
	}
	player := game.Player{ID: game.PlayerID(playerID), Name: name, Emoji: emoji}
	if err := r.engine.UpsertPlayer(player); err != nil {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "room_full", "no open seats")
		return
	}
	r.seats[player.ID] = seatCredential{tokenHash: identity.Hash(token)}
	r.ready[player.ID] = false
	// Hand the spectator their new active credentials; their client reconnects
	// as the active player and their spectator seat is dropped on disconnect.
	env, err := wsproto.Encode(wsproto.TypeJoinAccepted, wsproto.JoinAcceptedPayload{
		PlayerID: string(player.ID), ReconnectToken: token, Spectator: false,
	})
	if err == nil {
		c.trySend(env)
	}
	r.broadcastLobby()
}

func (r *Room) handleRemovePlayer(c *Client, msg inbound) {
	if r.engine.State().HostID != c.PlayerID {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "not_host", "only the host can remove players")
		return
	}
	if r.engine.Phase() != game.PhaseLobby {
		r.sendError(c.PlayerID, msg.envelope.RequestID, "wrong_phase", "players can only be removed in the lobby")
		return
	}
	var p wsproto.RemovePlayerPayload
	if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
		return
	}
	target := game.PlayerID(p.PlayerID)
	if target == c.PlayerID {
		return // the host cannot remove themselves this way
	}
	// Spectator?
	if _, ok := r.specSeats[target]; ok {
		delete(r.specSeats, target)
		delete(r.specViews, target)
		if sc, ok := r.spectators[target]; ok {
			sc.close(closeNormal, "removed by host")
			delete(r.spectators, target)
		}
		r.broadcastSpectatorUpdate()
		return
	}
	if err := r.engine.RemovePlayer(target); err != nil {
		return
	}
	delete(r.seats, target)
	delete(r.ready, target)
	delete(r.presence, target)
	if t, ok := r.graceTimers[target]; ok {
		t.Stop()
		delete(r.graceTimers, target)
	}
	if tc, ok := r.clients[target]; ok {
		tc.close(closeNormal, "removed by host")
		delete(r.clients, target)
	}
	r.broadcastPlayerExited(target, false)
	r.maybeMigrateHost()
	r.broadcastLobby()
}
