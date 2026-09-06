package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// presence is transient, room-owned connection state for one active seat. It
// is deliberately not part of game.State: whether a socket is open has no
// bearing on game rules.
type presence struct {
	connected    bool
	disconnectAt time.Time
	generation   uint64 // bumped on every transition; invalidates stale timer callbacks
	joinSeq      int64  // assigned once at first join; stable across reconnects; breaks host-migration ties
	graceExpired bool
}

// connectedSet snapshots which active seats currently have a live connection.
func (r *Room) connectedSet() map[game.PlayerID]bool {
	set := make(map[game.PlayerID]bool, len(r.clients))
	for id := range r.clients {
		set[id] = true
	}
	return set
}

// connectedActiveCount counts connected, non-forfeited active players — the
// set that gates pause/resume and phase completion.
func (r *Room) connectedActiveCount() int {
	n := 0
	for id := range r.clients {
		if p := r.engine.State().PlayerByID(id); p == nil || !p.Forfeited {
			n++
		}
	}
	return n
}

// markConnected records a seat as connected: first join assigns its permanent
// joinSeq; a reconnect cancels the grace timer, clears AFK, and resumes a
// paused phase. Must run on the Run goroutine.
func (r *Room) markConnected(id game.PlayerID) {
	pres, existed := r.presence[id]
	if !existed {
		pres = &presence{}
		r.presence[id] = pres
		pres.joinSeq = r.nextJoinSeq
		r.nextJoinSeq++
	}
	wasConnected := pres.connected
	pres.connected = true
	pres.disconnectAt = time.Time{}
	pres.graceExpired = false
	pres.generation++

	if t, ok := r.graceTimers[id]; ok {
		t.Stop()
		delete(r.graceTimers, id)
	}

	if existed && !wasConnected {
		r.broadcastPresence(id, true)
	}
	r.clearAFK(id)
	r.maybeMigrateHost()
	r.resumeIfPaused()
}

// markDisconnected records a seat as disconnected, starts its reconnect grace
// timer, and pauses the phase if that leaves nobody connected. Must run on the
// Run goroutine.
func (r *Room) markDisconnected(id game.PlayerID) {
	pres, ok := r.presence[id]
	if !ok || !pres.connected {
		return
	}
	pres.connected = false
	pres.disconnectAt = r.clock()
	pres.generation++
	gen := pres.generation

	if t, ok := r.graceTimers[id]; ok {
		t.Stop()
	}
	r.graceTimers[id] = time.AfterFunc(r.durations.Reconnect, func() {
		select {
		case r.graceExpiredCh <- graceExpiredMsg{playerID: id, generation: gen}:
		default:
		}
	})

	r.broadcastPresence(id, false)
	r.pauseIfEmpty()
}

// maybeMigrateHost promotes the earliest-joined connected, non-forfeited
// player to host if the current host is gone from the roster, forfeited, or
// disconnected past grace. A no-op otherwise.
func (r *Room) maybeMigrateHost() {
	s := r.engine.State()
	hostID := s.HostID
	hostInRoster := s.PlayerByID(hostID) != nil
	hostForfeited := hostInRoster && s.PlayerByID(hostID).Forfeited
	hostPres, hostExists := r.presence[hostID]
	hostGone := hostExists && !hostPres.connected && hostPres.graceExpired
	if hostInRoster && !hostForfeited && !hostGone {
		return
	}

	var candidate game.PlayerID
	var candidateSeq int64
	found := false
	for id, pres := range r.presence {
		if !pres.connected {
			continue
		}
		if p := s.PlayerByID(id); p == nil || p.Forfeited {
			continue
		}
		if !found || pres.joinSeq < candidateSeq {
			candidate, candidateSeq, found = id, pres.joinSeq, true
		}
	}
	if !found || candidate == hostID {
		return
	}
	if err := r.engine.SetHostID(candidate); err != nil {
		return
	}
	r.broadcastHostChanged(candidate)
}

// ---- AFK ----

// clearAFK clears a player's AFK flag (broadcasting the change) and records
// this round as their last interaction. Called on connect and on any command.
func (r *Room) clearAFK(id game.PlayerID) {
	if r.afk[id] {
		r.afk[id] = false
		r.broadcastAFK(id, false)
	}
	r.interacted[id] = r.engine.State().Round
}

// checkAFKOnNewRound marks any connected player who has not interacted for two
// complete rounds as AFK.
func (r *Room) checkAFKOnNewRound() {
	round := r.engine.State().Round
	for id := range r.clients {
		if r.afk[id] {
			continue
		}
		if last, ok := r.interacted[id]; ok && round-last >= 2 {
			r.afk[id] = true
			r.broadcastAFK(id, true)
		}
	}
}

func (r *Room) broadcastAFK(id game.PlayerID, afk bool) {
	env, err := wsproto.Encode(wsproto.TypePlayerAFKChanged, wsproto.PlayerAFKChangedPayload{ID: string(id), AFK: afk})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastPresence(id game.PlayerID, connected bool) {
	env, err := wsproto.Encode(wsproto.TypePlayerPresenceChanged, wsproto.PlayerPresenceChangedPayload{
		ID: string(id), Connected: connected,
	})
	if err == nil {
		r.broadcastSequenced(env)
	}
}

func (r *Room) broadcastHostChanged(id game.PlayerID) {
	env, err := wsproto.Encode(wsproto.TypeHostChanged, wsproto.HostChangedPayload{HostID: string(id)})
	if err == nil {
		r.broadcastSequenced(env)
	}
}
