package room

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// testPlayer bundles one player's real client-side connection with its
// resolved identity, so multi-player room-package tests can disconnect and
// reconnect specific seats deterministically via Room.Join/Leave directly.
type testPlayer struct {
	conn     *websocket.Conn
	playerID game.PlayerID
	connID   uint64
	token    string
}

func joinNewPlayer(t *testing.T, r *Room, name string) testPlayer {
	t.Helper()
	client, conn := dialTestConn(t)
	res := joinAndPump(t, r, conn, JoinRequest{Name: name})
	// join_accepted is always sent, but it isn't necessarily the very first
	// frame: if this join also happens to trigger host migration (e.g. this
	// player is the only connected candidate), a host_changed broadcast can
	// be enqueued to their own connection first. Search rather than assume.
	var token string
	found := false
	for i := 0; i < 10; i++ {
		env := readEnvelope(t, client)
		if env.Type == wsproto.TypeJoinAccepted {
			var p map[string]any
			_ = json.Unmarshal(env.Payload, &p)
			token, _ = p["reconnectToken"].(string)
			found = true
			break
		}
	}
	if !found {
		t.Fatal("never saw join_accepted")
	}
	drainUntilRoomState(t, client)
	return testPlayer{conn: client, playerID: res.PlayerID, connID: res.ConnID, token: token}
}

func reconnectPlayer(t *testing.T, r *Room, tp testPlayer) testPlayer {
	t.Helper()
	client, conn := dialTestConn(t)
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: tp.playerID, Token: tp.token})
	drainUntilRoomState(t, client)
	return testPlayer{conn: client, playerID: res.PlayerID, connID: res.ConnID, token: tp.token}
}

// disconnect simulates the seat's connection dropping, exactly as the server
// package's deferred rm.Leave(id, connID) call would on a real socket close.
func disconnect(r *Room, tp testPlayer) {
	r.Leave(tp.playerID, tp.connID)
}

// drainUntilRoomState reads and discards frames until a room_state arrives
// (skipping e.g. host_changed/presence/lobby_update noise), matching the
// readUntil helper already used by internal/server's tests.
func drainUntilRoomState(t *testing.T, c *websocket.Conn) {
	t.Helper()
	for i := 0; i < 20; i++ {
		env := readEnvelope(t, c)
		if env.Type == wsproto.TypeRoomState {
			return
		}
	}
	t.Fatal("never saw room_state")
}

// readUntilType reads frames off c until one of type typ arrives, within an
// overall deadline. Used to observe the effect of a timer that fires after
// a short injected duration, without a fixed sleep tied to the assertion.
func readUntilType(t *testing.T, c *websocket.Conn, typ string, overall time.Duration) wsproto.Envelope {
	t.Helper()
	deadline := time.Now().Add(overall)
	for time.Now().Before(deadline) {
		env := readEnvelopeWithTimeout(c, 300*time.Millisecond)
		if env == nil {
			continue
		}
		if env.Type == typ {
			return *env
		}
	}
	t.Fatalf("never saw %s within %s", typ, overall)
	return wsproto.Envelope{}
}

func readEnvelopeWithTimeout(c *websocket.Conn, d time.Duration) *wsproto.Envelope {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		return nil
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	return &env
}

// shortGraceDurations returns durations with a short, test-friendly grace
// period and long everything else, so only the mechanism under test can fire.
func shortGraceDurations(grace time.Duration) Durations {
	d := longTestDurations()
	d.Reconnect = grace
	return d
}

// joinFourAndStart joins the pre-seeded host (via reconnect) plus three new
// players, starts the game, and drains every player's stream up to the
// first TurnChanged — confirming PhaseDrawing round 1 has begun with the
// host (roster index 0) drawing first. Returns all four in roster order.
func joinFourAndStart(t *testing.T, r *Room, hostID game.PlayerID, hostToken string) []testPlayer {
	t.Helper()
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)
	host := testPlayer{conn: client1, playerID: hostID, connID: res1.ConnID, token: hostToken}

	players := []testPlayer{host, joinNewPlayer(t, r, "P2"), joinNewPlayer(t, r, "P3"), joinNewPlayer(t, r, "P4")}

	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(host.playerID, host.connID, start)

	for _, p := range players {
		readUntilType(t, p.conn, wsproto.TypeTurnChanged, 2*time.Second)
	}
	return players
}

// --- Requirement #1 / #19: disconnect+reconnect within grace ---

func TestReconnectWithinGracePreservesSeatAndHostStatus(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(2*time.Second))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}
	// A bystander who stays connected throughout, to observe broadcasts
	// about the host — the host's own connection is removed from the
	// broadcast set the moment it disconnects, so it can't observe them.
	bob := joinNewPlayer(t, r, "Bob")

	disconnect(r, host)
	presenceEnv := readUntilType(t, bob.conn, wsproto.TypePlayerPresenceChanged, 2*time.Second)
	var pp map[string]any
	_ = json.Unmarshal(presenceEnv.Payload, &pp)
	if pp["id"] != string(hostID) || pp["connected"] != false {
		t.Fatalf("presence event = %+v, want {id:%s connected:false}", pp, hostID)
	}

	host2 := reconnectPlayer(t, r, host)
	if host2.playerID != hostID {
		t.Fatalf("reconnected playerID = %q, want %q", host2.playerID, hostID)
	}
	if got := r.Snapshot().HostID; got != hostID {
		t.Fatalf("hostID = %q, want unchanged %q", got, hostID)
	}
}

// --- Requirement #2: a stale grace timer cannot remove a reconnected player ---

func TestStaleGraceTimerCannotRemoveReconnectedPlayer(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}

	disconnect(r, host)
	// Reconnect well within the grace period.
	reconnectPlayer(t, r, host)

	// Wait past the ORIGINAL grace deadline. The stale timer must not
	// remove the now-reconnected player.
	time.Sleep(grace + 150*time.Millisecond)
	found := false
	for _, p := range r.Snapshot().Players {
		if p.ID == string(hostID) {
			found = true
			if !p.Connected {
				t.Fatal("reconnected host shows as disconnected after the stale grace deadline passed")
			}
		}
	}
	if !found {
		t.Fatal("reconnected host was removed by a stale grace timer")
	}
}

// --- Requirement #3: a disconnected lobby player is removed after grace ---

func TestDisconnectedLobbyPlayerRemovedAfterGrace(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)

	bob := joinNewPlayer(t, r, "Bob")
	disconnect(r, bob)

	env := readUntilType(t, client1, wsproto.TypePlayerLeft, 2*time.Second)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["id"] != string(bob.playerID) {
		t.Fatalf("player_left id = %v, want %s", p["id"], bob.playerID)
	}
	for _, pv := range r.Snapshot().Players {
		if pv.ID == string(bob.playerID) {
			t.Fatal("Bob is still in the roster after his lobby grace expired")
		}
	}
}

// --- Requirement #4: host ownership does not migrate during grace ---

func TestHostDoesNotMigrateDuringGrace(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(2*time.Second))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}

	_ = joinNewPlayer(t, r, "Bob")
	disconnect(r, host)

	// Give the (long) grace timer no chance to fire; host must still be Host.
	time.Sleep(50 * time.Millisecond)
	if got := r.Snapshot().HostID; got != hostID {
		t.Fatalf("hostID = %q, want unchanged %q (grace has not expired)", got, hostID)
	}
}

// --- Requirement #5: host ownership migrates deterministically after grace ---

func TestHostMigratesDeterministicallyAfterGrace(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}

	// Bob joins before Carla — Bob has the earlier joinSeq among the two.
	bob := joinNewPlayer(t, r, "Bob")
	carla := joinNewPlayer(t, r, "Carla")

	disconnect(r, host)

	env := readUntilType(t, bob.conn, wsproto.TypeHostChanged, 2*time.Second)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["hostId"] != string(bob.playerID) {
		t.Fatalf("new hostId = %v, want the earlier-joined Bob (%s), not Carla (%s)", p["hostId"], bob.playerID, carla.playerID)
	}
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("engine hostID = %q, want %q", got, bob.playerID)
	}
}

// --- Requirement #6: a former host reconnecting after migration stays non-host ---

func TestFormerHostReconnectingDoesNotRegainOwnership(t *testing.T) {
	// Run this during an active game: a lobby disconnect's grace expiry
	// would remove the player from the roster entirely (see
	// TestDisconnectedLobbyPlayerRemovedAfterGrace), which would make their
	// old reconnect credentials genuinely invalid rather than exercising
	// "reconnects fine, but as a non-host" — an active-game disconnect
	// keeps them in the roster, which is the case this requirement is
	// actually about.
	grace := 40 * time.Millisecond
	durations := longTestDurations()
	durations.Reconnect = grace
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", durations)
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host, bob := players[0], players[1]

	// Advance the turn away from the host first, so only the
	// reconnect/host-migration mechanism is exercised here, not the
	// (separately tested) disconnected-drawer skip timer.
	stroke, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{Points: []wsproto.Point{{X: 0.1, Y: 0.1}}})
	r.Submit(host.playerID, host.connID, stroke)
	for _, p := range players {
		readUntilType(t, p.conn, wsproto.TypeTurnChanged, 2*time.Second)
	}

	disconnect(r, host)
	readUntilType(t, bob.conn, wsproto.TypeHostChanged, 2*time.Second)
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("expected migration to Bob before testing reconnect, got hostID=%q", got)
	}

	host2 := reconnectPlayer(t, r, host)
	if host2.playerID != hostID {
		t.Fatalf("former host failed to reconnect (should still be in the active game's roster): got playerID %q", host2.playerID)
	}
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("hostID = %q after former host reconnected, want unchanged %q", got, bob.playerID)
	}
}

// --- Requirement #7: no connected replacement does not panic or loop ---

func TestNoConnectedReplacementHostDoesNotPanic(t *testing.T) {
	grace := 30 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilRoomState(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}

	disconnect(r, host) // the ONLY player disconnects; nobody left to promote

	time.Sleep(grace + 150*time.Millisecond) // let the grace timer fire well past its deadline
	if got := r.Snapshot().HostID; got != hostID {
		t.Fatalf("hostID = %q, want unchanged %q (no connected candidate to promote)", got, hostID)
	}

	// The room must still be responsive afterward (no panic, no stuck loop):
	// a fresh join must succeed normally. Since the old host was removed
	// from the (lobby) roster and Bob is now the only connected player,
	// Bob becomes the new host by the same "earliest-joined connected
	// player" rule, re-evaluated on his join rather than looped for.
	bob := joinNewPlayer(t, r, "Bob")
	if bob.playerID == "" {
		t.Fatal("room did not accept a join after the no-replacement-host case")
	}
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("hostID = %q, want %q (the only connected player)", got, bob.playerID)
	}
}
