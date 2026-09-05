package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// presence is transient, room-owned connection-tracking state for one seat.
// It is deliberately not part of game.State: whether someone's socket is
// currently open has no bearing on game rules, so keeping it out of the
// engine keeps the engine pure and easy to test in isolation.
type presence struct {
	connected    bool
	disconnectAt time.Time
	generation   uint64 // bumped on every connect/disconnect transition; invalidates stale timer callbacks
	joinSeq      int64  // assigned once, at first join; stable across reconnects; breaks host-migration ties deterministically
	graceExpired bool   // set once the reconnect-grace timer has fired while still disconnected
}

// connectedSet snapshots which seats currently have a live connection, for
// engine calls that need to know who's eligible (voting) without the engine
// itself tracking presence. Always non-nil, even when empty — an engine
// caller distinguishes "no one connected" (empty map) from "presence not
// tracked, assume everyone eligible" (nil) by that nil-ness.
func (r *Room) connectedSet() map[game.PlayerID]bool {
	set := make(map[game.PlayerID]bool, len(r.clients))
	for id := range r.clients {
		set[id] = true
	}
	return set
}

// playerViews merges the engine's authoritative player list with room-level
// presence, for every outgoing message that lists players.
func (r *Room) playerViews() []wsproto.PlayerView {
	players := r.engine.State().Players
	views := make([]wsproto.PlayerView, len(players))
	for i, p := range players {
		connected := true
		if pres, ok := r.presence[p.ID]; ok {
			connected = pres.connected
		}
		views[i] = wsproto.PlayerView{
			ID:        string(p.ID),
			Name:      p.Name,
			Emoji:     p.Emoji,
			Score:     p.Score,
			Connected: connected,
		}
	}
	return views
}

// playerView returns one player's current merged view (engine identity +
// room presence), or nil if they're not on the roster. Used to build the
// snapshot's "you" field.
func (r *Room) playerView(id game.PlayerID) *wsproto.PlayerView {
	for _, v := range r.playerViews() {
		if v.ID == string(id) {
			return &v
		}
	}
	return nil
}

// markConnected records a seat as connected: on its first-ever join, this
// assigns its permanent joinSeq; on a later reconnect, it cancels any
// pending grace timer and restores full participation. Must only run on the
// Run goroutine.
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
	r.maybeMigrateHost()
	r.evaluateDrawTimer()
	r.evaluateVoting()
}

// markDisconnected records a seat as disconnected and starts its reconnect
// grace timer. Called only after the caller has confirmed the closing
// connection is still the seat's current one. Must only run on the Run
// goroutine.
func (r *Room) markDisconnected(id game.PlayerID) {
	pres, ok := r.presence[id]
	if !ok || !pres.connected {
		return
	}
	pres.connected = false
	pres.disconnectAt = time.Now()
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
	r.evaluateDrawTimer()
	r.evaluateVoting()
}

// maybeMigrateHost promotes the earliest-joined currently connected player
// to host if the current host is gone (removed from the lobby roster) or
// has been disconnected past their reconnect grace period. A no-op if the
// host is still in the roster and either connected or still within grace —
// which also covers a freshly created room whose host hasn't made its
// first WS connection yet: no presence entry exists for them yet, but they
// are still in the roster, so they get a chance to connect rather than
// being treated as needing replacement. If nobody is currently connected
// to promote, this is called again on the next reconnect, so the room
// re-checks rather than looping.
func (r *Room) maybeMigrateHost() {
	hostID := r.engine.State().HostID
	hostInRoster := false
	for _, p := range r.engine.State().Players {
		if p.ID == hostID {
			hostInRoster = true
			break
		}
	}
	hostPres, hostExists := r.presence[hostID]
	eligible := !hostInRoster || (hostExists && !hostPres.connected && hostPres.graceExpired)
	if !eligible {
		return
	}

	var candidate game.PlayerID
	var candidateSeq int64
	found := false
	for id, pres := range r.presence {
		if !pres.connected {
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
	r.broadcastLobby()
}

func (r *Room) broadcastPresence(id game.PlayerID, connected bool) {
	env, err := wsproto.Encode(wsproto.TypePlayerPresenceChanged, wsproto.PlayerPresenceChangedPayload{
		ID: string(id), Connected: connected,
	})
	if err == nil {
		r.broadcast(env)
	}
}

func (r *Room) broadcastHostChanged(id game.PlayerID) {
	env, err := wsproto.Encode(wsproto.TypeHostChanged, wsproto.HostChangedPayload{HostID: string(id)})
	if err == nil {
		r.broadcast(env)
	}
}
