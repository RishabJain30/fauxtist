package room

import "github.com/RishabJain30/fauxtist/internal/game"

// graceExpiredMsg is submitted by a reconnect-grace timer. generation guards
// against a stale fire: if the player reconnected (or disconnected again)
// since this timer was scheduled, their presence generation will have moved on
// and this message is dropped.
type graceExpiredMsg struct {
	playerID   game.PlayerID
	generation uint64
}

// handleGraceExpired runs on the Run goroutine when a disconnected seat's
// reconnect grace elapses. In the lobby, a still-disconnected player is removed
// from the roster; in an active match they keep their seat (removing them would
// corrupt the board and turn order) but the room re-checks host migration —
// an active-game host disconnect is exactly what migration exists for.
func (r *Room) handleGraceExpired(m graceExpiredMsg) {
	pres, ok := r.presence[m.playerID]
	if !ok || pres.generation != m.generation || pres.connected {
		return
	}
	pres.graceExpired = true
	delete(r.graceTimers, m.playerID)

	if r.engine.Phase() == game.PhaseLobby {
		if err := r.engine.RemovePlayer(m.playerID); err == nil {
			delete(r.presence, m.playerID)
			delete(r.seats, m.playerID)
			delete(r.ready, m.playerID)
			r.broadcastPlayerExited(m.playerID, false)
		}
	}

	r.maybeMigrateHost()
	if r.engine.Phase() == game.PhaseLobby {
		r.broadcastLobby()
	}
}

// stopAllTimers cancels every outstanding timer. Called from Run's exit path,
// so nothing fires after the actor has returned. Idempotent.
func (r *Room) stopAllTimers() {
	r.stopPhaseTimer()
	r.stopEarlyTimer()
	for _, t := range r.graceTimers {
		t.Stop()
	}
}
