package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
)

// This file is the room's server-authoritative phase driver. Every timed
// phase has an absolute deadline; a generation guard (phaseGen) ignores any
// timer callback fired after a transition, and the whole clock pauses when no
// active player is connected so a match can never play itself to completion in
// an empty room.

// isTimedPhase reports whether the current phase runs on a deadline.
func (r *Room) isTimedPhase() bool {
	switch r.engine.Phase() {
	case game.PhaseLobby, game.PhaseGameOver:
		return false
	default:
		return true
	}
}

// phaseDurationFor returns how long a phase runs: the preset's timing, unless
// a test override is installed.
func (r *Room) phaseDurationFor(p game.Phase) time.Duration {
	if r.phaseDurOverride != nil {
		return r.phaseDurOverride(p)
	}
	t := game.PresetConfigFor(r.engine.Preset()).Timings
	switch p {
	case game.PhaseIncome:
		return t.Income
	case game.PhaseNegotiation:
		return t.Negotiation
	case game.PhaseDeclaration:
		return t.Declaration
	case game.PhaseDeclarationReveal:
		return t.DeclarationReveal
	case game.PhaseSecretPlanning:
		return t.SecretPlanning
	case game.PhaseResolution:
		return t.ResolutionMax
	case game.PhaseRoundSummary:
		return t.RoundSummary
	default:
		return 0
	}
}

// beginMatch starts a freshly set-up match (or rematch): the engine is already
// at round 1 INCOME. It bumps the match generation and sends everyone a full
// snapshot of the new board.
func (r *Room) beginMatch() {
	r.matchGen++
	r.paused = false
	r.rematchOK = map[game.PlayerID]bool{}
	r.enterPhase(true)
}

// enterPhase is called after every engine phase transition. It (re)starts the
// phase timer, announces the new phase, and fires any phase-specific public
// event. fullSnapshot sends every client a complete redacted snapshot instead
// of a lightweight phase_changed — used when the board itself changed
// wholesale (match start, game over, back to lobby).
func (r *Room) enterPhase(fullSnapshot bool) {
	r.phaseGen++
	r.earlyCountdownActive = false
	r.stopEarlyTimer()
	r.stopPhaseTimer()

	phase := r.engine.Phase()
	if phase == game.PhaseIncome {
		r.checkAFKOnNewRound()
	}
	if dur := r.phaseDurationFor(phase); dur > 0 {
		r.startPhaseTimer(dur)
	} else {
		r.phaseDeadline = time.Time{}
	}

	if fullSnapshot {
		r.broadcastSnapshotToAll()
	} else {
		r.broadcastPhaseChanged()
	}

	switch phase {
	case game.PhaseDeclarationReveal:
		r.broadcastDeclarationsRevealed()
	case game.PhaseRoundSummary:
		r.broadcastRoundSummary()
	case game.PhaseGameOver:
		r.broadcastGameOver()
	}

	if phase == game.PhaseDeclaration || phase == game.PhaseSecretPlanning {
		r.checkEarlyCompletion()
	}
}

// advancePhase performs the engine transition for the current phase, then
// enters the next one. Called by a phase deadline, the early-lock countdown,
// or (for RESOLUTION) the server itself.
func (r *Room) advancePhase() {
	switch r.engine.Phase() {
	case game.PhaseIncome:
		_ = r.engine.ApplyIncome()
		r.enterPhase(false)
	case game.PhaseNegotiation:
		_ = r.engine.BeginDeclaration()
		r.enterPhase(false)
	case game.PhaseDeclaration:
		_ = r.engine.RevealDeclarations()
		r.enterPhase(false)
	case game.PhaseDeclarationReveal:
		_ = r.engine.BeginPlanning()
		r.enterPhase(false)
	case game.PhaseSecretPlanning:
		res, err := r.engine.Resolve()
		if err != nil {
			return
		}
		r.enterPhase(false)
		r.broadcastRoundResolved(res)
	case game.PhaseResolution:
		_ = r.engine.BeginRoundSummary()
		r.enterPhase(false)
	case game.PhaseRoundSummary:
		_ = r.engine.AdvanceRound()
		r.enterPhase(r.engine.Phase() == game.PhaseGameOver)
	}
}

// startPhaseTimer arms the phase deadline with a generation-guarded callback.
func (r *Room) startPhaseTimer(dur time.Duration) {
	r.stopPhaseTimer()
	r.phaseDeadline = r.clock().Add(dur)
	gen := r.phaseGen
	r.phaseTimer = time.AfterFunc(dur, func() {
		select {
		case r.phaseFireCh <- phaseFireMsg{gen: gen}:
		default:
		}
	})
}

func (r *Room) stopPhaseTimer() {
	if r.phaseTimer != nil {
		r.phaseTimer.Stop()
		r.phaseTimer = nil
	}
}

func (r *Room) stopEarlyTimer() {
	if r.earlyTimer != nil {
		r.earlyTimer.Stop()
		r.earlyTimer = nil
	}
}

// onPhaseFire advances the phase if the deadline that fired is still current
// and the match is not paused.
func (r *Room) onPhaseFire(gen int64) {
	if gen != r.phaseGen || r.paused {
		return
	}
	r.advancePhase()
}

// onEarlyFire advances the phase when the all-locked early countdown elapses.
func (r *Room) onEarlyFire(gen int64) {
	if gen != r.phaseGen || r.paused || !r.earlyCountdownActive {
		return
	}
	r.advancePhase()
}

// onSoloFire is reserved: the one-player-remaining choice is offered by the
// client and resolved by the end_no_contest / keep_waiting commands, so no
// server timer currently drives it.
func (r *Room) onSoloFire(int64) {}

// requiredPlayers returns the connected, non-forfeited active players whose
// completion a phase waits on for early advance.
func (r *Room) requiredPlayers() []game.PlayerID {
	var out []game.PlayerID
	s := r.engine.State()
	for id := range r.clients {
		if p := s.PlayerByID(id); p != nil && !p.Forfeited {
			out = append(out, id)
		}
	}
	return out
}

// checkEarlyCompletion begins the visible all-locked countdown once every
// required player has completed the current phase early.
func (r *Room) checkEarlyCompletion() {
	if r.earlyCountdownActive {
		return
	}
	phase := r.engine.Phase()
	if phase != game.PhaseDeclaration && phase != game.PhaseSecretPlanning {
		return
	}
	required := r.requiredPlayers()
	if len(required) == 0 {
		return
	}
	s := r.engine.State()
	for _, id := range required {
		if phase == game.PhaseDeclaration {
			if d, ok := s.Declarations[id]; !ok || !d.Submitted {
				return
			}
		} else {
			if o, ok := s.Orders[id]; !ok || !o.Locked {
				return
			}
		}
	}
	r.startEarlyCountdown()
}

// startEarlyCountdown arms the short, irreversible countdown and re-broadcasts
// the phase's status with the countdown deadline.
func (r *Room) startEarlyCountdown() {
	r.earlyCountdownActive = true
	r.stopEarlyTimer()
	dur := r.durations.EarlyCountdown
	r.earlyDeadline = r.clock().Add(dur)
	gen := r.phaseGen
	r.earlyTimer = time.AfterFunc(dur, func() {
		select {
		case r.earlyFireCh <- phaseFireMsg{gen: gen}:
		default:
		}
	})
	if r.engine.Phase() == game.PhaseDeclaration {
		r.broadcastDeclarationStatus()
	} else {
		r.broadcastPlanningStatus()
	}
}

// pauseIfEmpty freezes the phase clock when no active player is connected,
// storing the remaining time so it resumes where it left off.
func (r *Room) pauseIfEmpty() {
	if r.paused || !r.isTimedPhase() {
		return
	}
	if r.connectedActiveCount() > 0 {
		return
	}
	remaining := r.phaseDeadline.Sub(r.clock())
	if remaining < r.durations.ResumeMinRemaining {
		remaining = r.durations.ResumeMinRemaining
	}
	r.pauseRemaining = remaining
	r.paused = true
	r.stopPhaseTimer()
	r.stopEarlyTimer()
	r.earlyCountdownActive = false
}

// resumeIfPaused restarts a paused phase clock with its stored remaining time
// once an active player reconnects.
func (r *Room) resumeIfPaused() {
	if !r.paused || r.connectedActiveCount() == 0 {
		return
	}
	r.paused = false
	dur := r.pauseRemaining
	r.pauseRemaining = 0
	if dur <= 0 {
		dur = r.durations.ResumeMinRemaining
	}
	r.startPhaseTimer(dur)
	r.broadcastPhaseChanged()
	r.checkEarlyCompletion()
}

// endMatchNow drives the engine to GAME_OVER (already set by the engine) and
// announces it. Used after a forfeit-to-one or an explicit no-contest.
func (r *Room) endMatchNow() {
	r.stopPhaseTimer()
	r.stopEarlyTimer()
	r.earlyCountdownActive = false
	r.paused = false
	r.enterPhase(true)
}
