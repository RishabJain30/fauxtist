package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
)

// RoomOption configures optional Room construction-time behavior.
type RoomOption func(*Room)

// WithClock overrides the room's notion of "now" for activity tracking and
// expiry decisions. Tests use this to advance time deterministically
// instead of sleeping; production never sets it, leaving the default of
// time.Now.
func WithClock(clock func() time.Time) RoomOption {
	return func(r *Room) { r.clock = clock }
}

// WithPhaseDuration overrides how long each timed phase lasts, so tests can
// drive a whole match in milliseconds instead of the preset's real timings.
// Production never sets it. Return a non-positive duration for a phase to make
// it effectively instant.
func WithPhaseDuration(fn func(game.Phase) time.Duration) RoomOption {
	return func(r *Room) { r.phaseDurOverride = fn }
}

// touch records that something meaningful just happened in this room —
// creation, a join/reconnect, a disconnect, or a dispatched client command
// (see handle) — resetting the empty-room idle clock the hub's sweeper
// checks in MaybeExpire. Grace-timer expiry deliberately does not call
// this: a still-disconnected seat's timer firing is not evidence anyone is
// present, so it must never keep an otherwise-abandoned room alive.
func (r *Room) touch() {
	r.lastActivity = r.clock()
}

// MaybeExpire is called by the hub's sweeper to atomically check and, if
// eligible, self-terminate: eligibility (no connected clients, idle for at
// least ttl) and the termination itself happen in the same step on the
// Run goroutine, so a concurrent reconnect can never race with this
// decision — either the reconnect's processJoin already ran first (and
// this sees a connected client, so it declines), or this runs first and
// returns before any later join is processed. Returns true if the room
// expired itself; the hub is then responsible for removing it from its
// map (see Hub.Sweep).
func (r *Room) MaybeExpire(ttl time.Duration) bool {
	resp := make(chan bool, 1)
	select {
	case r.expireCh <- expireReq{ttl: ttl, resp: resp}:
	case <-r.done:
		return false // already stopped by some other path; nothing left for the hub to remove twice
	}
	select {
	case expired := <-resp:
		return expired
	case <-r.done:
		return true // stopped while the request was in flight — the hub should still remove its map entry
	}
}

type expireReq struct {
	ttl  time.Duration
	resp chan bool
}

// handleExpireCheck runs on the Run goroutine. Must only be called from
// there.
func (r *Room) handleExpireCheck(req expireReq) bool {
	return len(r.clients) == 0 && r.clock().Sub(r.lastActivity) >= req.ttl
}

// Stopped returns a channel that's closed once the room's actor has fully
// stopped, for callers that need to wait on it (tests, and the hub during
// graceful shutdown) without touching any actor-owned state directly.
func (r *Room) Stopped() <-chan struct{} { return r.done }

// Shutdown idempotently stops the room's Run loop, if it hasn't already
// stopped. Safe to call from any goroutine, any number of times,
// concurrently. It only signals — Run's own exit path (running on its own
// goroutine) is what actually stops timers and closes client connections,
// since those must never be touched from outside the actor.
func (r *Room) Shutdown() {
	r.shutdownOnce.Do(func() { close(r.done) })
}

// closeAllClients closes every currently registered client connection.
// Only called from Run's exit path, so it never races with processJoin
// registering a new one. The close handshake runs in each client's own
// goroutine (see Client.close) so it can never stall the room from
// finishing its own shutdown.
func (r *Room) closeAllClients() {
	for _, c := range r.clients {
		c.close(closeRoomClosed, "room closed")
	}
}
