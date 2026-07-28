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
	s := r.engine.State()
	snap := stateSnapshot(s, c.PlayerID)
	if env, err := wsproto.Encode(wsproto.TypeRoomState, snap); err == nil {
		c.trySend(env)
	}
}

// broadcastEvent fans one engine event out to clients with per-player filtering.
func (r *Room) broadcastEvent(ev game.Event) {
	switch e := ev.(type) {
	case game.RoundStarted:
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
	case game.TurnChanged:
		env, _ := wsproto.Encode(wsproto.TypeTurnChanged, wsproto.TurnChangedPayload{
			CurrentPlayer: string(e.CurrentPlayer), Lap: e.Lap, TotalLaps: e.TotalLaps,
		})
		r.broadcast(env)
	case game.StrokeAdded:
		env, _ := wsproto.Encode(wsproto.TypeStrokeBroadcast, e.Stroke)
		r.broadcast(env)
	case game.PhaseChanged:
		env, _ := wsproto.Encode(wsproto.TypePhaseChanged, wsproto.PhaseChangedPayload{Phase: string(e.Phase)})
		r.broadcast(env)
		r.onPhaseChange(e.Phase)
	case game.VoteRecorded:
		env, _ := wsproto.Encode(wsproto.TypeVoteUpdate, map[string]any{
			"votesCast": e.VotesCast, "votesTotal": e.VotesTotal,
		})
		r.broadcast(env)
	case game.RoundEnded:
		env, _ := wsproto.Encode(wsproto.TypeRoundResult, e.Result)
		r.broadcast(env)
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

// onPhaseChange starts/stops the discussion timer.
func (r *Room) onPhaseChange(p game.Phase) {
	if r.discussionTimer != nil {
		r.discussionTimer.Stop()
		r.discussionTimer = nil
	}
	if p == game.PhaseDiscussion {
		host := r.engine.State().HostID
		r.discussionTimer = time.AfterFunc(r.discussionDur, func() {
			// Timer fires on its own goroutine; route back through the inbox so
			// the engine is only ever touched by the Run loop.
			r.Submit(host, wsproto.Envelope{Type: wsproto.TypeEndDiscussion})
		})
	}
}

// stateSnapshot builds a room_state payload, hiding the word from the impostor.
func stateSnapshot(s game.State, viewer game.PlayerID) map[string]any {
	snap := map[string]any{
		"phase":       string(s.Phase),
		"players":     s.Players,
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
	}
	return snap
}
