package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// stamp fills in the envelope fields only the room knows: the protocol
// version was already set by wsproto.Encode, so this adds the room's public
// id and its current revision. Every outbound envelope must pass through
// here exactly once, right before it reaches a Client's send channel — the
// one place a message's seq is fixed, so every recipient of the same
// broadcast (even with different redacted payloads) observes the same
// number.
func (r *Room) stamp(env wsproto.Envelope) wsproto.Envelope {
	env.RoomID = r.Code
	env.Seq = r.revision
	return env
}

// broadcast sends an envelope to every connected client.
func (r *Room) broadcast(env wsproto.Envelope) {
	env = r.stamp(env)
	for _, c := range r.clients {
		c.trySend(env)
	}
}

// sendTo sends an envelope to one client if present.
func (r *Room) sendTo(id game.PlayerID, env wsproto.Envelope) {
	if c, ok := r.clients[id]; ok {
		c.trySend(r.stamp(env))
	}
}

// sendError sends a typed, machine-readable error to one client. requestID
// (from the offending command's envelope, if any) is echoed back so the
// client can correlate the rejection with the specific command it sent.
func (r *Room) sendError(id game.PlayerID, requestID, code, msg string) {
	env, err := wsproto.Encode(wsproto.TypeError, wsproto.ErrorPayload{Message: msg, Code: code})
	if err == nil {
		env.RequestID = requestID
		r.sendTo(id, env)
	}
}

// sendSnapshot sends the canonical, recipient-specific state_snapshot to
// one client: on a fresh join, a reconnect, or an explicit resync request.
// It is the only message a client needs to fully reconstruct its UI from
// scratch, and always replaces (never merges into) the client's local
// state.
func (r *Room) sendSnapshot(c *Client) {
	snap := r.buildSnapshot(c.PlayerID)
	if env, err := wsproto.Encode(wsproto.TypeStateSnapshot, snap); err == nil {
		c.trySend(r.stamp(env))
	}
}

// broadcastEvent fans one engine event out to clients with per-player filtering.
func (r *Room) broadcastEvent(ev game.Event) {
	switch e := ev.(type) {
	case game.RoundStarted:
		r.roundGeneration++
		// Reveal the word to everyone EXCEPT the impostor; the impostor gets the
		// category only.
		for id, c := range r.clients {
			payload := map[string]any{
				"round":          e.Round,
				"category":       e.Category,
				"order":          e.Order,
				"youAreImpostor": id == e.ImpostorID,
			}
			if id != e.ImpostorID {
				payload["word"] = e.Word
			}
			if env, err := wsproto.Encode(wsproto.TypeRoundStarted, payload); err == nil {
				c.trySend(r.stamp(env))
			}
		}
		r.evaluateDrawTimer()
	case game.TurnChanged:
		env, _ := wsproto.Encode(wsproto.TypeTurnChanged, wsproto.TurnChangedPayload{
			CurrentPlayer: string(e.CurrentPlayer), Lap: e.Lap, TotalLaps: e.TotalLaps,
		})
		r.broadcast(env)
		r.evaluateDrawTimer()
	case game.StrokeAdded:
		env, _ := wsproto.Encode(wsproto.TypeStrokeBroadcast, e.Stroke)
		r.broadcast(env)
	case game.PhaseChanged:
		env, _ := wsproto.Encode(wsproto.TypePhaseChanged, wsproto.PhaseChangedPayload{Phase: string(e.Phase)})
		r.broadcast(env)
		if e.Phase == game.PhaseReveal {
			// Compute the guess deadline (if any) before building the round
			// result payload, so it can be included for clients to show a
			// countdown. Only announce here when the round is caught (and so
			// awaiting a guess with no RoundEnded coming immediately after,
			// in this same event cascade) — an uncaught round's RoundEnded
			// event, built from finishVoting in the same call, always
			// follows right behind this one and announces the (identical,
			// now-final) result itself, so announcing it twice here would
			// just be a redundant duplicate send.
			r.evaluateGuessDeadline()
			if res := r.engine.State().LastResult; res != nil && res.Caught {
				r.broadcastRoundResult(*res)
			}
		}
		r.onPhaseChange(e.Phase)
		r.evaluateDrawTimer()
	case game.VoteRecorded:
		env, _ := wsproto.Encode(wsproto.TypeVoteUpdate, map[string]any{
			"votesCast": e.VotesCast, "votesTotal": e.VotesTotal,
		})
		r.broadcast(env)
	case game.RoundEnded:
		r.broadcastRoundResult(e.Result)
		r.startRevealTimer()
	case game.GameEnded:
		env, _ := wsproto.Encode(wsproto.TypeGameOver, map[string]any{"finalScores": e.FinalScores})
		r.broadcast(env)
	}
}

func (r *Room) broadcastChat(from game.PlayerID, text string) {
	env, err := wsproto.Encode(wsproto.TypeChatBroadcast, map[string]any{
		"from": string(from), "text": text,
	})
	if err == nil {
		r.broadcast(env)
	}
}

// startRevealTimer holds on the reveal phase, then signals the room to advance
// to the next round. Timer fires on its own goroutine and routes through the
// advance channel so the engine is only touched by the Run loop.
func (r *Room) startRevealTimer() {
	if r.revealTimer != nil {
		r.revealTimer.Stop()
	}
	r.revealTimer = time.AfterFunc(r.durations.Reveal, func() {
		select {
		case r.advance <- struct{}{}:
		default:
		}
	})
}

// onPhaseChange starts/stops the discussion timer.
func (r *Room) onPhaseChange(p game.Phase) {
	if r.discussionTimer != nil {
		r.discussionTimer.Stop()
		r.discussionTimer = nil
	}
	r.discussionDeadline = time.Time{}
	if p == game.PhaseDiscussion {
		r.discussionDeadline = time.Now().Add(r.durations.Discussion)
		r.discussionTimer = time.AfterFunc(r.durations.Discussion, func() {
			// Timer fires on its own goroutine; route back through a dedicated
			// channel (not Submit/an inbox message) since this is a
			// server-initiated action with no connection to authenticate — the
			// Run loop applies it directly, using whoever is host at the time.
			select {
			case r.discussionTimeout <- struct{}{}:
			default:
			}
		})
	}
}

func (r *Room) broadcastLobby() {
	s := r.engine.State()
	env, err := wsproto.Encode(wsproto.TypeLobbyUpdate, map[string]any{
		"players": r.playerViews(),
		"hostId":  string(s.HostID),
	})
	if err == nil {
		r.broadcast(env)
	}
}

// broadcastPlayerLeft announces that a player was permanently removed from
// the roster (lobby-only, via a reconnect grace expiring). This is distinct
// from player_presence_changed, which fires on every connect/disconnect
// without implying removal.
func (r *Room) broadcastPlayerLeft(id game.PlayerID) {
	env, err := wsproto.Encode(wsproto.TypePlayerLeft, map[string]any{"id": string(id)})
	if err == nil {
		r.broadcast(env)
	}
}

// redactedResult returns a copy of res with the secret word blanked out for
// the round's own impostor, who never learns it — whether they evaded
// detection, were caught and guessed (right or wrong), or timed out. This
// is the one place that rule is enforced, shared by every place a round
// result is sent (the live announcement and the full-state snapshot) so it
// can't drift between them.
func redactedResult(res game.RoundResult, viewer game.PlayerID) game.RoundResult {
	if viewer == res.ImpostorID {
		res.Word = ""
	}
	return res
}

// broadcastRoundResult announces this round's result to every client,
// individually redacted via redactedResult. Used both when reveal begins
// (announcing caught/not-caught, immediately final unless caught) and when
// a caught impostor's guess or timeout later finalizes the round — the
// single path either way, so the announcement and the redaction rule can
// never disagree with each other.
func (r *Room) broadcastRoundResult(res game.RoundResult) {
	for id, c := range r.clients {
		redacted := redactedResult(res, id)
		payload := map[string]any{
			"impostorId": string(redacted.ImpostorID),
			"caught":     redacted.Caught,
			"tally":      redacted.Tally,
		}
		if redacted.Word != "" {
			payload["word"] = redacted.Word
		}
		// A guess or timeout resolution: only present once the caught
		// impostor's fate is actually settled, never on the initial
		// "entering reveal, awaiting a guess" announcement.
		resolved := redacted.ImpostorGuess != "" || redacted.ImpostorTimedOut
		if resolved {
			payload["impostorGuess"] = redacted.ImpostorGuess
			payload["impostorGuessedRight"] = redacted.ImpostorGuessedRight
			payload["impostorTimedOut"] = redacted.ImpostorTimedOut
		}
		if redacted.Caught && !resolved && !r.guessDeadline.IsZero() {
			payload["guessDeadlineMs"] = r.guessDeadline.UnixMilli()
		}
		if env, err := wsproto.Encode(wsproto.TypeRoundResult, payload); err == nil {
			c.trySend(r.stamp(env))
		}
	}
}

// broadcastExcept sends to every connected client except one.
func (r *Room) broadcastExcept(except game.PlayerID, env wsproto.Envelope) {
	env = r.stamp(env)
	for id, c := range r.clients {
		if id != except {
			c.trySend(env)
		}
	}
}

func (r *Room) voiceJoin(from game.PlayerID) {
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

func (r *Room) relayVoiceSignal(from game.PlayerID, p wsproto.VoiceSignalIn) {
	env, err := wsproto.Encode(wsproto.TypeVoiceSignal, map[string]any{
		"from": string(from), "kind": p.Kind, "payload": p.Payload,
	})
	if err == nil {
		r.sendTo(game.PlayerID(p.To), env)
	}
}

func (r *Room) broadcastVoiceState(from game.PlayerID, p wsproto.VoiceStateIn) {
	env, err := wsproto.Encode(wsproto.TypeVoiceState, map[string]any{
		"id": string(from), "muted": p.Muted, "speaking": p.Speaking,
	})
	if err == nil {
		r.broadcastExcept(from, env)
	}
}

// evaluateVoting recomputes and (re)broadcasts the vote requirement against
// current presence, resolving voting if every connected voter has now
// voted. A no-op outside the voting phase. Called whenever presence changes
// during voting: a disconnect may complete the requirement without any new
// vote being cast, and a reconnect may reopen it.
func (r *Room) evaluateVoting() {
	if r.engine.State().Phase != game.PhaseVoting {
		return
	}
	connected := r.connectedSet()
	cast, total := r.engine.VotingProgress(connected)
	if env, err := wsproto.Encode(wsproto.TypeVoteUpdate, map[string]any{"votesCast": cast, "votesTotal": total}); err == nil {
		r.broadcast(env)
	}
	for _, ev := range r.engine.CheckVotingResolution(connected) {
		r.broadcastEvent(ev)
	}
}

// buildSnapshot is the single authoritative builder for the canonical
// state_snapshot payload: every join, reconnect, and explicit resync sends
// exactly what this returns for that viewer, so redaction can never drift
// between call sites. It carries everything the UI needs to reconstruct
// its screen after a full refresh, for every phase, without depending on
// any previously-received incremental event.
func (r *Room) buildSnapshot(viewer game.PlayerID) map[string]any {
	s := r.engine.State()
	snap := map[string]any{
		"phase":       string(s.Phase),
		"players":     r.playerViews(),
		"hostId":      string(s.HostID),
		"round":       s.Round,
		"totalRounds": s.TotalRounds,
		"category":    s.Category,
		"strokes":     s.Strokes,
		"turnIndex":   s.TurnIndex,
		"lap":         s.Lap,
		"totalLaps":   s.TotalLaps,
	}
	if you := r.playerView(viewer); you != nil {
		snap["you"] = *you
	}
	if s.Phase == game.PhaseDrawing && s.TurnIndex >= 0 && s.TurnIndex < len(s.Players) {
		snap["currentPlayer"] = string(s.Players[s.TurnIndex].ID)
	}
	if s.Phase == game.PhaseDiscussion && !r.discussionDeadline.IsZero() {
		snap["discussionDeadlineMs"] = r.discussionDeadline.UnixMilli()
	}
	if s.Phase != game.PhaseLobby {
		snap["youAreImpostor"] = viewer == s.ImpostorID
		if viewer != s.ImpostorID {
			snap["word"] = s.Word
		}
	}
	if s.Phase == game.PhaseVoting {
		_, voted := s.Votes[viewer]
		cast, total := r.engine.VotingProgress(r.connectedSet())
		targets := make([]string, 0, len(s.Players))
		for _, p := range s.Players {
			if p.ID != viewer {
				targets = append(targets, string(p.ID))
			}
		}
		snap["hasVoted"] = voted
		snap["votesCast"] = cast
		snap["votesTotal"] = total
		snap["voteTargets"] = targets
	}
	if s.LastResult != nil {
		redacted := redactedResult(*s.LastResult, viewer)
		snap["lastResult"] = redacted
		if s.Phase == game.PhaseReveal && redacted.Caught && redacted.ImpostorGuess == "" && !redacted.ImpostorTimedOut && !r.guessDeadline.IsZero() {
			snap["guessDeadlineMs"] = r.guessDeadline.UnixMilli()
		}
	}
	if s.Phase == game.PhaseGameOver {
		snap["finalScores"] = r.playerViews()
	}
	return snap
}
