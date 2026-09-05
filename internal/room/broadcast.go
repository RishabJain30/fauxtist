package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// broadcast sends an envelope to every connected client.
func (r *Room) broadcast(env wsproto.Envelope) {
	for _, c := range r.clients {
		c.trySend(env)
	}
}

// sendTo sends an envelope to one client if present.
func (r *Room) sendTo(id game.PlayerID, env wsproto.Envelope) {
	if c, ok := r.clients[id]; ok {
		c.trySend(env)
	}
}

func (r *Room) sendError(id game.PlayerID, msg string) {
	env, err := wsproto.Encode(wsproto.TypeError, wsproto.ErrorPayload{Message: msg})
	if err == nil {
		r.sendTo(id, env)
	}
}

// sendSnapshot sends the full current state to a (re)joining client. The word is
// omitted if the recipient is the impostor.
func (r *Room) sendSnapshot(c *Client) {
	snap := r.stateSnapshot(c.PlayerID)
	if env, err := wsproto.Encode(wsproto.TypeRoomState, snap); err == nil {
		c.trySend(env)
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
				c.trySend(env)
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
			// Compute the guess deadline (if any) before building the reveal
			// payload, so it can be included for clients to show a countdown.
			r.evaluateGuessDeadline()
			r.broadcastReveal()
		}
		r.onPhaseChange(e.Phase)
		r.evaluateDrawTimer()
	case game.VoteRecorded:
		env, _ := wsproto.Encode(wsproto.TypeVoteUpdate, map[string]any{
			"votesCast": e.VotesCast, "votesTotal": e.VotesTotal,
		})
		r.broadcast(env)
	case game.RoundEnded:
		env, _ := wsproto.Encode(wsproto.TypeRoundResult, e.Result)
		r.broadcast(env)
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
	if p == game.PhaseDiscussion {
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

// broadcastReveal tells clients who was caught when entering the reveal phase.
// The word is withheld from the impostor, who still has to guess it.
func (r *Room) broadcastReveal() {
	res := r.engine.State().LastResult
	if res == nil {
		return
	}
	for id, c := range r.clients {
		payload := map[string]any{
			"impostorId": string(res.ImpostorID),
			"caught":     res.Caught,
			"tally":      res.Tally,
		}
		if id != res.ImpostorID {
			payload["word"] = res.Word
		}
		if res.Caught && res.ImpostorGuess == "" && !r.guessDeadline.IsZero() {
			payload["guessDeadlineMs"] = r.guessDeadline.UnixMilli()
		}
		if env, err := wsproto.Encode(wsproto.TypeRoundResult, payload); err == nil {
			c.trySend(env)
		}
	}
}

// broadcastExcept sends to every connected client except one.
func (r *Room) broadcastExcept(except game.PlayerID, env wsproto.Envelope) {
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

// stateSnapshot builds a room_state payload, hiding the word from the impostor.
func (r *Room) stateSnapshot(viewer game.PlayerID) map[string]any {
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
	if s.Phase != game.PhaseLobby {
		snap["youAreImpostor"] = viewer == s.ImpostorID
		if viewer != s.ImpostorID {
			snap["word"] = s.Word
		}
	}
	if s.LastResult != nil {
		snap["lastResult"] = s.LastResult
		if s.Phase == game.PhaseReveal && s.LastResult.Caught && s.LastResult.ImpostorGuess == "" && !r.guessDeadline.IsZero() {
			snap["guessDeadlineMs"] = r.guessDeadline.UnixMilli()
		}
	}
	return snap
}
