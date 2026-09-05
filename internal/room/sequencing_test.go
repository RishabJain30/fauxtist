package room

import (
	"encoding/json"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// These tests guard against a real production bug found by code review: a
// single accepted command can cascade into several ordered engine events
// (e.g. StartGame's RoundStarted followed by TurnChanged), and the room
// used to bump its revision exactly once for the whole cascade, stamping
// every event in it with the SAME seq. The frontend sequencer
// (web/src/sequencing.js) treats a seq at or behind what's already applied
// as a duplicate and drops it — so the second (and any later) event in a
// cascade was silently discarded by every real client, even though these
// low-level tests, which read raw envelopes off the wire without ever
// running them through decideSequence, kept passing throughout. See
// roomConnection.test.js for the client-side half of this regression
// coverage, which does run the real sequencer.

// --- StartGame: RoundStarted followed by TurnChanged ---

func TestStartGameCascadeEventsGetDistinctIncreasingSeq(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	joinNewPlayer(t, r, "P2")
	joinNewPlayer(t, r, "P3")
	joinNewPlayer(t, r, "P4")

	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(host.playerID, host.connID, start)

	roundStarted := readUntilType(t, host.conn, wsproto.TypeRoundStarted, twoSeconds)
	turnChanged := readUntilType(t, host.conn, wsproto.TypeTurnChanged, twoSeconds)

	if turnChanged.Seq <= roundStarted.Seq {
		t.Fatalf("turn_changed.seq = %d, round_started.seq = %d; want turn_changed strictly greater so a real client's sequencer does not drop it as a duplicate",
			turnChanged.Seq, roundStarted.Seq)
	}
	if turnChanged.Seq != roundStarted.Seq+1 {
		t.Fatalf("turn_changed.seq = %d, want exactly round_started.seq+1 = %d (no gap, or a resync would be triggered)",
			turnChanged.Seq, roundStarted.Seq+1)
	}
}

// --- AddStroke: StrokeAdded followed by TurnChanged ---

func TestStrokeCascadeEventsGetDistinctIncreasingSeq(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0]

	stroke, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{
		Points: []wsproto.Point{{X: 0.2, Y: 0.2}}, Color: "#111", Width: 3,
	})
	r.Submit(host.playerID, host.connID, stroke)

	strokeBroadcast := readUntilType(t, host.conn, wsproto.TypeStrokeBroadcast, twoSeconds)
	turnChanged := readUntilType(t, host.conn, wsproto.TypeTurnChanged, twoSeconds)

	if turnChanged.Seq <= strokeBroadcast.Seq {
		t.Fatalf("turn_changed.seq = %d, stroke_broadcast.seq = %d; want turn_changed strictly greater so a real client's sequencer does not drop it as a duplicate",
			turnChanged.Seq, strokeBroadcast.Seq)
	}
}

// --- Voting resolving via a disconnect (evaluateVoting/CheckVotingResolution, not apply()) ---

func TestVotingResolutionViaDisconnectGetsDistinctIncreasingSeq(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	// Three of the four vote, leaving the fourth as the only one still
	// required. Disconnecting them removes them from the requirement
	// entirely, so voting resolves purely from evaluateVoting's
	// CheckVotingResolution path (never through apply()) — the second
	// cascade-producing code path the fix needed to cover.
	target := players[0].playerID
	for _, p := range players[:3] {
		v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(target)})
		r.Submit(p.playerID, p.connID, v)
	}
	watcher := players[0]
	disconnect(r, players[3])

	phaseChanged := readUntilType(t, watcher.conn, wsproto.TypePhaseChanged, twoSeconds)
	roundResult := readUntilType(t, watcher.conn, wsproto.TypeRoundResult, twoSeconds)

	if roundResult.Seq <= phaseChanged.Seq {
		t.Fatalf("round_result.seq = %d, phase_changed.seq = %d; want round_result strictly greater so a real client's sequencer does not drop it as a duplicate",
			roundResult.Seq, phaseChanged.Seq)
	}
}

// --- Lifecycle broadcasts outside applyEvents: presence, host migration, lobby ---
//
// The tests above cover engine-event cascades, all routed through
// applyEvents. This section covers everything else that broadcasts a
// SEQUENCED_TYPES message: processJoin, processLeave, markConnected,
// markDisconnected, evaluateVoting, maybeMigrateHost, and
// handleGraceExpired. These used to share a manually-pre-bumped revision
// across several distinct broadcasts in one call (e.g.
// player_presence_changed and the vote_update right after it, or
// player_left, host_changed, and lobby_update all in one grace-expiry),
// for the exact same reason the engine-cascade bug existed: a real
// client's sequencer drops the second (and any later) message in such a
// run as a duplicate. Fixed by broadcastSequenced (see broadcast.go),
// which every such call site now routes through instead of a bare
// r.broadcast plus a shared pre-bump.

// sequencedWireTypes mirrors web/src/protocol.js's SEQUENCED_TYPES
// exactly. Kept here, test-only, so a scenario test can collect every
// message a real client's sequencer would gap-check without needing a
// running frontend.
var sequencedWireTypes = map[string]bool{
	wsproto.TypeStateSnapshot:         true,
	wsproto.TypeLobbyUpdate:           true,
	wsproto.TypePlayerLeft:            true,
	wsproto.TypePlayerPresenceChanged: true,
	wsproto.TypeHostChanged:           true,
	wsproto.TypeRoundStarted:          true,
	wsproto.TypeStrokeBroadcast:       true,
	wsproto.TypeTurnChanged:           true,
	wsproto.TypePhaseChanged:          true,
	wsproto.TypeVoteUpdate:            true,
	wsproto.TypeRoundResult:           true,
	wsproto.TypeGameOver:              true,
}

// isType returns an until-predicate for collectSequencedEnvelopes.
func isType(want string) func(wsproto.Envelope) bool {
	return func(e wsproto.Envelope) bool { return e.Type == want }
}

// collectSequencedEnvelopes reads frames off c, keeping every one whose
// type is in sequencedWireTypes, until until reports true (inclusive) or
// overall elapses.
func collectSequencedEnvelopes(t *testing.T, c *websocket.Conn, until func(wsproto.Envelope) bool, overall time.Duration) []wsproto.Envelope {
	t.Helper()
	var out []wsproto.Envelope
	deadline := time.Now().Add(overall)
	for time.Now().Before(deadline) {
		env := readEnvelopeWithTimeout(c, 300*time.Millisecond)
		if env == nil {
			continue
		}
		if sequencedWireTypes[env.Type] {
			out = append(out, *env)
		}
		if until(*env) {
			return out
		}
	}
	t.Fatal("timed out collecting sequenced envelopes")
	return nil
}

// drainExactly reads and discards exactly n already-pending envelopes off
// c. Room setup (a join, in particular) always leaves a trailing
// lobby_update on every already-connected client's queue, sent after the
// snapshot the join helpers (joinNewPlayer, joinAndPumpHost,
// reconnectPlayer) already drained through — without draining it first, a
// later collectSequencedEnvelopes call keyed on lobby_update would
// terminate immediately on that leftover setup noise instead of the
// scenario actually under test.
//
// This must read an exact, known count via the plain (non-timeout)
// readEnvelope rather than loop on readEnvelopeWithTimeout until one call
// times out: nhooyr's Conn.Read closes the underlying connection for good
// the moment a read's context deadline is actually reached while still
// blocked (see nhooyr.io/websocket's timeoutLoop) — a deliberate
// wait-for-timeout drain would permanently kill the very connection the
// test still needs to read the real scenario from afterward.
func drainExactly(t *testing.T, c *websocket.Conn, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		readEnvelope(t, c)
	}
}

// assertNoGapOrDuplicate replicates decideSequence's incremental-message
// rule against a real connection's observed sequenced envelopes: every
// non-snapshot entry must be exactly one greater than the previous, and a
// snapshot (exempt from that rule, same as decideSequence's
// apply-snapshot branch) resets the baseline to its own seq. This is
// exactly what a real frontend client's sequencer enforces — proving it
// here at the wire level means no real client would ever see a dropped
// message or an unnecessary resync across the collected window.
func assertNoGapOrDuplicate(t *testing.T, envs []wsproto.Envelope) {
	t.Helper()
	if len(envs) == 0 {
		t.Fatal("collected no sequenced envelopes to check")
	}
	var last int64
	haveLast := false
	for _, e := range envs {
		if e.Type == wsproto.TypeStateSnapshot {
			last = e.Seq
			haveLast = true
			continue
		}
		if !haveLast {
			last = e.Seq
			haveLast = true
			continue
		}
		if e.Seq <= last {
			t.Fatalf("%s.seq = %d is <= previous applied seq %d (a real client's sequencer would drop this as a duplicate)", e.Type, e.Seq, last)
		}
		if e.Seq != last+1 {
			t.Fatalf("%s.seq = %d, want exactly %d (a real client's sequencer would see this as a gap and trigger an unnecessary resync)", e.Type, e.Seq, last+1)
		}
		last = e.Seq
	}
}

// --- Invariant coverage: start game ---

func TestSequencingInvariant_StartGame(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	joinNewPlayer(t, r, "P2")
	joinNewPlayer(t, r, "P3")
	joinNewPlayer(t, r, "P4")

	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(host.playerID, host.connID, start)

	envs := collectSequencedEnvelopes(t, host.conn, isType(wsproto.TypeTurnChanged), twoSeconds)
	assertNoGapOrDuplicate(t, envs)
}

// --- Invariant coverage: stroke/turn cascade ---

func TestSequencingInvariant_StrokeTurnCascade(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0]

	for i := 0; i < 3; i++ {
		stroke, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{
			Points: []wsproto.Point{{X: 0.2, Y: 0.2}}, Color: "#111", Width: 3,
		})
		r.Submit(players[i%len(players)].playerID, players[i%len(players)].connID, stroke)
	}

	envs := collectSequencedEnvelopes(t, host.conn, isType(wsproto.TypeTurnChanged), twoSeconds)
	assertNoGapOrDuplicate(t, envs)
}

// --- Invariant coverage: voting resolution (via a disconnect completing it) ---

func TestSequencingInvariant_VotingResolutionViaDisconnect(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	target := players[0].playerID
	for _, p := range players[:3] {
		v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(target)})
		r.Submit(p.playerID, p.connID, v)
	}
	watcher := players[0]
	disconnect(r, players[3])

	envs := collectSequencedEnvelopes(t, watcher.conn, isType(wsproto.TypeRoundResult), twoSeconds)
	assertNoGapOrDuplicate(t, envs)
}

// --- Requirement: a voter disconnecting during voting without resolving it ---
//
// Regression for scenario (a): markDisconnected's broadcastPresence and
// the standalone vote_update inside evaluateVoting used to share one
// pre-bumped revision, so a real client's sequencer dropped the
// vote_update as a duplicate of the presence change — remaining clients
// never saw the updated requirement.

func TestSequencingInvariant_NonResolvingVotingDisconnect(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	watcher := players[0]
	// Nobody has voted yet; disconnecting one of the remaining three still
	// leaves two others required — voting must not resolve.
	disconnect(r, players[3])

	presenceEnv := readUntilType(t, watcher.conn, wsproto.TypePlayerPresenceChanged, twoSeconds)
	voteEnv := readUntilType(t, watcher.conn, wsproto.TypeVoteUpdate, twoSeconds)

	var pp map[string]any
	_ = json.Unmarshal(presenceEnv.Payload, &pp)
	if pp["connected"] != false {
		t.Fatalf("player_presence_changed payload = %v, want connected:false for the disconnecting player", pp)
	}
	var vp map[string]any
	_ = json.Unmarshal(voteEnv.Payload, &vp)
	if cast, _ := vp["votesCast"].(float64); cast != 0 {
		t.Fatalf("vote_update votesCast = %v, want 0 (nobody has voted)", vp["votesCast"])
	}
	if total, _ := vp["votesTotal"].(float64); total != 3 {
		t.Fatalf("vote_update votesTotal = %v, want 3 (one of four disconnected)", vp["votesTotal"])
	}
	if voteEnv.Seq <= presenceEnv.Seq {
		t.Fatalf("vote_update.seq = %d, player_presence_changed.seq = %d; want vote_update strictly greater so a real client's sequencer does not drop it as a duplicate",
			voteEnv.Seq, presenceEnv.Seq)
	}
	if r.Snapshot().Phase != "voting" {
		t.Fatalf("phase = %q, want voting to remain unresolved", r.Snapshot().Phase)
	}
}

// --- Requirement: a non-host lobby player's grace expiring removes them ---

func TestSequencingInvariant_LobbyRemoval(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	bob := joinNewPlayer(t, r, "Bob")

	disconnect(r, bob)

	envs := collectSequencedEnvelopes(t, host.conn, isType(wsproto.TypePlayerLeft), 2*time.Second)
	assertNoGapOrDuplicate(t, envs)

	var p map[string]any
	_ = json.Unmarshal(envs[len(envs)-1].Payload, &p)
	if p["id"] != string(bob.playerID) {
		t.Fatalf("player_left id = %v, want %s", p["id"], bob.playerID)
	}
}

// --- Requirement: the lobby host disconnecting, grace expiring, and host migration ---
//
// Regression for scenario (b): handleGraceExpired used to pre-bump one
// revision for the whole operation, so player_left, host_changed (from
// maybeMigrateHost), and its own trailing lobby_update all shared one
// seq — a real client applied only the first and dropped the other two.

func TestSequencingInvariant_HostMigrationOnGraceExpiry(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	// Bob joins before Carla — Bob has the earlier joinSeq, so migration
	// must land on Bob, not Carla.
	bob := joinNewPlayer(t, r, "Bob")
	carla := joinNewPlayer(t, r, "Carla")
	drainExactly(t, carla.conn, 1) // her own join left a trailing lobby_update unread

	disconnect(r, host)

	envs := collectSequencedEnvelopes(t, carla.conn, isType(wsproto.TypeLobbyUpdate), 2*time.Second)
	assertNoGapOrDuplicate(t, envs)

	var sawPlayerLeft, sawHostChanged bool
	var hostChangedID string
	for _, e := range envs {
		switch e.Type {
		case wsproto.TypePlayerLeft:
			sawPlayerLeft = true
		case wsproto.TypeHostChanged:
			sawHostChanged = true
			var p map[string]any
			_ = json.Unmarshal(e.Payload, &p)
			hostChangedID, _ = p["hostId"].(string)
		}
	}
	if !sawPlayerLeft {
		t.Fatal("never saw player_left for the removed host")
	}
	if !sawHostChanged {
		t.Fatal("never saw host_changed for the migration")
	}
	if hostChangedID != string(bob.playerID) {
		t.Fatalf("new hostId = %q, want the earlier-joined Bob (%s), not Carla (%s)", hostChangedID, bob.playerID, carla.playerID)
	}
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("engine hostID = %q, want %q", got, bob.playerID)
	}
}

// --- Requirement: host migration deferred to a later reconnect (markConnected's path) ---
//
// Regression for scenario (c), exercised via a genuinely different call
// site than the previous test: maybeMigrateHost used to broadcast its own
// lobby_update internally, in addition to whichever caller's trailing
// broadcastLobby — a real client dropped one of the two identical
// lobby_updates as a duplicate. This scenario also proves markConnected's
// call to maybeMigrateHost (not handleGraceExpired's) still gets a
// correctly ordered host_changed -> lobby_update pair after the fix
// removed maybeMigrateHost's own internal broadcast.
//
// No bystander observes this one: any third connected client would
// itself be a valid migration candidate (maybeMigrateHost has no notion
// of "connected but ineligible"), which would migrate host ownership to
// them the moment the original host's grace expires — defeating the very
// "deferred, nobody eligible yet" setup this test needs. Bob's own
// reconnect is instead observed directly off his own new connection,
// built with joinAndPump (not the reconnectPlayer/drainUntilStateSnapshot
// helpers, which drain everything up to and including his own
// state_snapshot — discarding presence and host_changed right along with
// it, since this room's send order is presence, host_changed, THEN the
// reconnecting client's own snapshot, THEN lobby_update).
func TestSequencingInvariant_HostMigrationDeferredToReconnect(t *testing.T) {
	grace := 150 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	bob := joinNewPlayer(t, r, "Bob")

	// The host disconnects first, with enough of a head start that its
	// grace expires while Bob is still disconnected too (started below) —
	// so maybeMigrateHost finds no connected candidate yet, and migration
	// is deferred to Bob's later reconnect.
	disconnect(r, host)
	time.Sleep(60 * time.Millisecond)
	disconnect(r, bob)

	// Host's grace (started first) expires around t=150ms; Bob's (started
	// ~60ms later) around t=210ms. Wait past the host's expiry but well
	// before Bob's, then reconnect Bob.
	time.Sleep(120 * time.Millisecond)
	if got := r.Snapshot().HostID; got == bob.playerID {
		t.Fatal("migration happened before the reconnect — test setup's timing margin is too tight")
	}

	client, conn := dialTestConn(t)
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: bob.playerID, Token: bob.token})

	envs := collectSequencedEnvelopes(t, client, isType(wsproto.TypeLobbyUpdate), 2*time.Second)
	assertNoGapOrDuplicate(t, envs)

	var sawHostChanged bool
	var hostChangedID string
	for _, e := range envs {
		if e.Type == wsproto.TypeHostChanged {
			sawHostChanged = true
			var p map[string]any
			_ = json.Unmarshal(e.Payload, &p)
			hostChangedID, _ = p["hostId"].(string)
		}
	}
	if !sawHostChanged {
		t.Fatal("never saw host_changed for the deferred migration")
	}
	if hostChangedID != string(bob.playerID) {
		t.Fatalf("new hostId = %q, want Bob (%s)", hostChangedID, bob.playerID)
	}
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("engine hostID = %q, want %q", got, bob.playerID)
	}
	if res.PlayerID != bob.playerID {
		t.Fatalf("reconnect resolved to playerID %q, want %q", res.PlayerID, bob.playerID)
	}
}
