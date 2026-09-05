package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
)

// graceExpiredMsg is submitted by a reconnect-grace timer. generation
// guards against a stale fire: if the player reconnected (or disconnected
// again) since this timer was scheduled, their presence generation will
// have moved on and this message is dropped.
type graceExpiredMsg struct {
	playerID   game.PlayerID
	generation uint64
}

// handleGraceExpired runs on the Run goroutine. In the lobby, a still-
// disconnected player is removed from the roster outright; in an active
// game they stay in the roster (removing them mid-game would corrupt turn
// order, roles, and scoring) but the room re-evaluates host migration
// either way, since an active-game host disconnect is exactly the case
// migration exists for.
func (r *Room) handleGraceExpired(m graceExpiredMsg) {
	pres, ok := r.presence[m.playerID]
	if !ok || pres.generation != m.generation || pres.connected {
		return // stale: reconnected (or something else changed) since this was scheduled
	}
	pres.graceExpired = true
	delete(r.graceTimers, m.playerID)
	r.revision++ // grace expiring is always visible: a removal, a host migration, or at minimum a re-broadcast lobby view

	if r.engine.State().Phase == game.PhaseLobby {
		if err := r.engine.RemovePlayer(m.playerID); err == nil {
			delete(r.presence, m.playerID)
			r.broadcastPlayerLeft(m.playerID)
		}
	}

	r.maybeMigrateHost()
	r.broadcastLobby()
}

// drawSkipMsg is submitted by the disconnected-drawer skip timer.
type drawSkipMsg struct {
	playerID   game.PlayerID
	generation uint64
}

// evaluateDrawTimer starts, restarts, or cancels the disconnected-drawer
// skip timer to match the current phase/turn/presence. Called after any
// event that could change which player (if any) is the disconnected
// current drawer: a disconnect, a reconnect, a turn change, or a phase
// change. Idempotent and cheap to call redundantly.
func (r *Room) evaluateDrawTimer() {
	if r.drawSkipTimer != nil {
		r.drawSkipTimer.Stop()
		r.drawSkipTimer = nil
	}
	if len(r.clients) == 0 {
		return // nobody connected at all — wait, don't schedule into a void
	}
	s := r.engine.State()
	if s.Phase != game.PhaseDrawing || s.TurnIndex < 0 || s.TurnIndex >= len(s.Players) {
		return
	}
	drawer := s.Players[s.TurnIndex].ID
	pres, ok := r.presence[drawer]
	if !ok || pres.connected {
		return // current drawer is connected — nothing to do
	}
	gen := pres.generation
	r.drawSkipTimer = time.AfterFunc(r.durations.DisconnectedTurn, func() {
		select {
		case r.drawSkipCh <- drawSkipMsg{playerID: drawer, generation: gen}:
		default:
		}
	})
}

// handleDrawSkip runs on the Run goroutine. It re-validates everything
// against current state before acting, so a late timer can never skip a
// different player's turn or advance a turn that already moved on.
func (r *Room) handleDrawSkip(m drawSkipMsg) {
	pres, ok := r.presence[m.playerID]
	if !ok || pres.generation != m.generation || pres.connected {
		return // stale: reconnected since this was scheduled
	}
	if len(r.clients) == 0 {
		return // nobody connected — do not advance into a void
	}
	s := r.engine.State()
	if s.Phase != game.PhaseDrawing || s.TurnIndex < 0 || s.TurnIndex >= len(s.Players) || s.Players[s.TurnIndex].ID != m.playerID {
		return // stale: no longer this player's turn
	}
	r.apply(r.engine.SkipTurn())
	r.evaluateDrawTimer() // (re)schedule for whoever is now current, if they're also disconnected
}

// guessTimeoutMsg is submitted by the caught-impostor guess-deadline timer.
// roundGen (bumped once per RoundStarted, including across a rematch that
// reuses round numbers) guards against a stale fire from a previous round.
type guessTimeoutMsg struct {
	roundGen int64
}

// evaluateGuessDeadline starts the caught-impostor guess deadline on
// entering the caught-awaiting-guess state, or cancels it once that state
// is no longer active (a guess arrived, or the round moved on). Called
// after entering reveal and after a real guess is applied.
func (r *Room) evaluateGuessDeadline() {
	if r.guessTimer != nil {
		r.guessTimer.Stop()
		r.guessTimer = nil
	}
	r.guessDeadline = time.Time{}

	s := r.engine.State()
	if s.Phase != game.PhaseReveal || s.LastResult == nil || !s.LastResult.Caught || s.LastResult.ImpostorGuess != "" || s.LastResult.ImpostorTimedOut {
		return
	}
	gen := r.roundGeneration
	r.guessDeadline = time.Now().Add(r.durations.ImpostorGuess)
	r.guessTimer = time.AfterFunc(r.durations.ImpostorGuess, func() {
		select {
		case r.guessTimeoutCh <- guessTimeoutMsg{roundGen: gen}:
		default:
		}
	})
}

// handleGuessTimeout runs on the Run goroutine, re-validating against
// current state (round generation, phase, and whether a guess already
// arrived) before resolving — so a valid guess that arrived first always
// wins, and a late timer from an earlier round can never affect a new one.
func (r *Room) handleGuessTimeout(m guessTimeoutMsg) {
	if m.roundGen != r.roundGeneration {
		return
	}
	s := r.engine.State()
	if s.Phase != game.PhaseReveal || s.LastResult == nil || !s.LastResult.Caught || s.LastResult.ImpostorGuess != "" || s.LastResult.ImpostorTimedOut {
		return
	}
	r.apply(r.engine.ResolveImpostorTimeout())
}

// stopAllTimers cancels every outstanding timer. Called when the room stops
// so nothing fires (harmlessly, but pointlessly) after Run has returned.
// Idempotent.
func (r *Room) stopAllTimers() {
	if r.discussionTimer != nil {
		r.discussionTimer.Stop()
	}
	if r.revealTimer != nil {
		r.revealTimer.Stop()
	}
	if r.drawSkipTimer != nil {
		r.drawSkipTimer.Stop()
	}
	if r.guessTimer != nil {
		r.guessTimer.Stop()
	}
	for _, t := range r.graceTimers {
		t.Stop()
	}
}
