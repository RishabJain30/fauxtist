package room

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeClock is a mutable clock for deterministic expiry tests — advanced
// explicitly rather than sleeping on a real timer.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

// --- Requirement #1/#2: empty inactive rooms expire; connected rooms never do ---

func TestMaybeExpireTrueWhenEmptyAndIdlePastTTL(t *testing.T) {
	clock := newFakeClock()
	r, _, _ := newTestRoomWithDurations(t, "Host", longTestDurations(), WithClock(clock.Now))
	startTestRoom(t, r)

	ttl := 5 * time.Minute
	if r.MaybeExpire(ttl) {
		t.Fatal("must not expire before the TTL has elapsed")
	}
	clock.Advance(ttl + time.Second)
	if !r.MaybeExpire(ttl) {
		t.Fatal("expected the room to expire once idle past the TTL")
	}
}

func TestMaybeExpireFalseWhileConnected(t *testing.T) {
	clock := newFakeClock()
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations(), WithClock(clock.Now))
	startTestRoom(t, r)

	client, conn := dialTestConn(t)
	defer client.CloseNow()
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})

	clock.Advance(time.Hour)
	if r.MaybeExpire(5 * time.Minute) {
		t.Fatal("must never expire a room with a connected client, regardless of idle time")
	}
}

// --- Requirement #3: reconnecting at the expiry boundary must not lose the room ---

func TestReconnectAtExpiryBoundaryWinsTheRace(t *testing.T) {
	clock := newFakeClock()
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations(), WithClock(clock.Now))
	startTestRoom(t, r)

	ttl := time.Minute
	clock.Advance(ttl + time.Second) // eligible for expiry, if still empty by the time it's actually checked

	client, conn := dialTestConn(t)
	defer client.CloseNow()
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})

	if r.MaybeExpire(ttl) {
		t.Fatal("a reconnect that lands before the expiry check must win the race")
	}
}

// --- Requirement #4: room shutdown cancels timers and closes clients ---

func TestShutdownStopsRunAndClosesClients(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runReturned := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(runReturned)
	}()

	client, conn := dialTestConn(t)
	defer client.CloseNow()
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client)

	r.Shutdown()

	select {
	case <-runReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Shutdown")
	}

	// Drain whatever else was already in flight (e.g. the lobby_update the
	// reconnect itself triggers) before the close frame arrives.
	closed := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, readCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, _, err := client.Read(readCtx)
		readCancel()
		if err == nil {
			continue
		}
		if errors.Is(err, context.DeadlineExceeded) {
			continue
		}
		closed = true
		break
	}
	if !closed {
		t.Fatal("expected the client connection to be closed by shutdown")
	}
}

func TestShutdownIsIdempotentAndSafeFromAnyGoroutine(t *testing.T) {
	r, _, _ := newTestRoom(t, "Host")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go r.Run(ctx)

	done := make(chan struct{}, 5)
	for i := 0; i < 5; i++ {
		go func() { r.Shutdown(); done <- struct{}{} }()
	}
	for i := 0; i < 5; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("a concurrent Shutdown call never returned")
		}
	}
}

// --- Requirement: Get-and-removal race degrades to a clean error, not a hang ---

func TestJoinReturnsErrRoomClosedOnceTheRoomHasStopped(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runReturned := make(chan struct{})
	go func() {
		r.Run(ctx)
		close(runReturned)
	}()

	r.Shutdown()
	select {
	case <-runReturned:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after Shutdown")
	}

	_, conn := dialTestConn(t)
	_, err := r.Join(conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	if !errors.Is(err, ErrRoomClosed) {
		t.Fatalf("Join error = %v, want ErrRoomClosed", err)
	}
}
