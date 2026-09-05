package room

import (
	"encoding/json"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// drawingTestDurations returns durations where the disconnected-turn skip
// fires quickly and everything else is out of the way.
func drawingTestDurations(skip time.Duration) Durations {
	d := longTestDurations()
	d.DisconnectedTurn = skip
	return d
}

// --- Requirement #8: the migrated host can use every host-only action ---
//
// Split into two focused scenarios (lobby start, mid-game end_discussion)
// rather than one long end-to-end game. A rematch (new_game) is dispatched
// through the exact same path as these two — room.handle() calls
// engine.Restart(msg.from) gated only by engine's own `by == HostID` check,
// the same check StartGame/EndDiscussion use, and HostID is the same field
// SetHostID updates during migration — so it is not a separately gated
// action; TestSetHostIDTransitionsOwnership (engine) plus these two cover
// the actual integration point.

func TestMigratedHostCanStartLobbyGame(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}

	bob := joinNewPlayer(t, r, "P2")
	_ = joinNewPlayer(t, r, "P3")
	_ = joinNewPlayer(t, r, "P4")
	// A 4th non-host joiner: the original host is about to be removed from
	// the lobby roster entirely (grace expiry, still in the lobby), so
	// enough OTHER players must remain to still meet MinPlayers afterward.
	_ = joinNewPlayer(t, r, "P5")

	disconnect(r, host) // original host disconnects and is removed from the lobby after grace
	readUntilType(t, bob.conn, wsproto.TypeHostChanged, 2*time.Second)
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("expected migration to Bob, got %q", got)
	}

	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(bob.playerID, bob.connID, start)
	readUntilType(t, bob.conn, wsproto.TypeRoundStarted, 2*time.Second)
}

func TestMigratedHostCanEndDiscussion(t *testing.T) {
	skip := 40 * time.Millisecond
	durations := drawingTestDurations(skip)
	durations.Reconnect = 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", durations)
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host, bob := players[0], players[1]

	// Drive through all of round 1's drawing with everyone connected, so
	// reaching discussion doesn't depend on the skip timer at all.
	driveThroughDrawing(t, r, players, players[1])

	disconnect(r, host) // now disconnect the (still-)host, from discussion
	readUntilType(t, bob.conn, wsproto.TypeHostChanged, 2*time.Second)
	if got := r.Snapshot().HostID; got != bob.playerID {
		t.Fatalf("expected migration to Bob, got %q", got)
	}

	endDiscussion, _ := wsproto.Encode(wsproto.TypeEndDiscussion, map[string]any{})
	r.Submit(bob.playerID, bob.connID, endDiscussion)
	waitForPhase(t, bob.conn, "voting")
	if got := r.Snapshot().Phase; got != "voting" {
		t.Fatalf("phase = %q after the migrated host's end_discussion, want voting", got)
	}
}

// waitForPhase reads frames off c until a phase_changed to want arrives.
func waitForPhase(t *testing.T, c *websocket.Conn, want string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		env := readEnvelopeWithTimeout(c, 500*time.Millisecond)
		if env == nil || env.Type != wsproto.TypePhaseChanged {
			continue
		}
		var p map[string]any
		_ = json.Unmarshal(env.Payload, &p)
		if p["phase"] == want {
			return
		}
	}
	t.Fatalf("never saw phase_changed to %q", want)
}

// driveThroughDrawing submits a stroke on behalf of whoever watcher's
// stream reports as the current drawer, until the phase leaves drawing.
// joinFourAndStart already consumed the round's first TurnChanged
// (turnIndex 0, players[0]) to confirm drawing had begun, without acting on
// it, so this submits that first stroke itself before watching for more.
func driveThroughDrawing(t *testing.T, r *Room, players []testPlayer, watcher testPlayer) {
	t.Helper()
	byID := map[string]testPlayer{}
	for _, p := range players {
		byID[string(p.playerID)] = p
	}
	first, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{Points: []wsproto.Point{{X: 0.2, Y: 0.2}}})
	r.Submit(players[0].playerID, players[0].connID, first)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		env := readEnvelopeWithTimeout(watcher.conn, 500*time.Millisecond)
		if env == nil {
			continue
		}
		switch env.Type {
		case wsproto.TypeTurnChanged:
			var p map[string]any
			_ = json.Unmarshal(env.Payload, &p)
			cur, _ := p["currentPlayer"].(string)
			if pl, ok := byID[cur]; ok {
				s, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{Points: []wsproto.Point{{X: 0.2, Y: 0.2}}})
				r.Submit(pl.playerID, pl.connID, s)
			}
		case wsproto.TypePhaseChanged:
			var p map[string]any
			_ = json.Unmarshal(env.Payload, &p)
			if p["phase"] != "drawing" {
				return
			}
		}
	}
	t.Fatal("timed out driving through drawing")
}

// --- Requirement #9 / #10: disconnected current drawer is skipped, unless they reconnect first ---

func TestDisconnectedDrawerIsSkippedAfterDeadline(t *testing.T) {
	skip := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", drawingTestDurations(skip))
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0] // host draws first (roster index 0)

	disconnect(r, host)
	env := readUntilType(t, players[1].conn, wsproto.TypeTurnChanged, 2*time.Second)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["currentPlayer"] == string(host.playerID) {
		t.Fatal("turn did not advance past the disconnected drawer")
	}
}

func TestReconnectingBeforeDrawingDeadlinePreventsSkip(t *testing.T) {
	skip := 300 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", drawingTestDurations(skip))
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0]

	disconnect(r, host)
	host2 := reconnectPlayer(t, r, host)

	// Wait past the original skip deadline, then confirm it's still the
	// reconnected host's turn (a stroke from them must be accepted).
	time.Sleep(skip + 150*time.Millisecond)
	stroke, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{Points: []wsproto.Point{{X: 0.3, Y: 0.3}}})
	r.Submit(host2.playerID, host2.connID, stroke)
	env := readUntilType(t, players[1].conn, wsproto.TypeTurnChanged, 2*time.Second)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["currentPlayer"] == string(host.playerID) {
		t.Fatal("turn advanced past the reconnected host's own stroke as if it were a skip")
	}
}

// --- Requirement #11: consecutive disconnected drawers don't freeze or recurse indefinitely ---

func TestConsecutiveDisconnectedDrawersAreSkippedSafely(t *testing.T) {
	skip := 30 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", drawingTestDurations(skip))
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)

	// Disconnect the first three drawers (roster order); only players[3]
	// remains connected.
	disconnect(r, players[0])
	disconnect(r, players[1])
	disconnect(r, players[2])

	// The turn must cascade through all three skips and land on the one
	// remaining connected player without hanging.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		env := readEnvelopeWithTimeout(players[3].conn, 500*time.Millisecond)
		if env == nil {
			continue
		}
		if env.Type == wsproto.TypeTurnChanged {
			var p map[string]any
			_ = json.Unmarshal(env.Payload, &p)
			if p["currentPlayer"] == string(players[3].playerID) {
				return // success
			}
		}
	}
	t.Fatal("turn never reached the one remaining connected player")
}

// --- Requirement #12 / #13 / #14: voting ignores disconnected non-voters, keeps cast votes, allows a reconnecting voter to still vote ---

func TestVotingResolvesWithoutDisconnectedVoters(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	// Everyone but players[3] votes for players[0]; players[3] then
	// disconnects. Voting must resolve without waiting for their vote.
	for _, pl := range players[:3] {
		v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(players[0].playerID)})
		r.Submit(pl.playerID, pl.connID, v)
	}
	if got := r.Snapshot().Phase; got != "voting" {
		t.Fatalf("phase = %q, want still voting (1 unvoted connected player)", got)
	}

	disconnect(r, players[3])
	readUntilType(t, players[0].conn, wsproto.TypePhaseChanged, 2*time.Second)
	if got := r.Snapshot().Phase; got != "reveal" {
		t.Fatalf("phase = %q, want reveal once the only unvoted player disconnected", got)
	}
}

func TestVotesCastBeforeDisconnectRemainInTally(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(players[0].playerID)})
	r.Submit(players[0].playerID, players[0].connID, v)
	env := readUntilType(t, players[1].conn, wsproto.TypeVoteUpdate, 2*time.Second)
	var before map[string]any
	_ = json.Unmarshal(env.Payload, &before)
	castBefore := before["votesCast"].(float64)

	disconnect(r, players[0]) // the voter who already voted disconnects
	// Presence change during voting re-broadcasts vote_update; votesCast
	// must not have dropped even though the voter who cast it is now gone.
	env2 := readUntilType(t, players[1].conn, wsproto.TypeVoteUpdate, 2*time.Second)
	var after map[string]any
	_ = json.Unmarshal(env2.Payload, &after)
	if after["votesCast"].(float64) != castBefore {
		t.Fatalf("votesCast = %v after disconnect, want unchanged %v", after["votesCast"], castBefore)
	}
}

func TestReconnectingBeforeVoteResolutionCanStillVote(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	disconnect(r, players[3])
	readUntilType(t, players[0].conn, wsproto.TypeVoteUpdate, 2*time.Second) // presence recompute

	p3 := reconnectPlayer(t, r, players[3])
	v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(players[0].playerID)})
	r.Submit(p3.playerID, p3.connID, v)

	// The reconnected player's vote must be accepted (no error), and must
	// count toward the tally: cast the other three too and confirm the
	// round resolves (which requires all 4, proving p3's vote landed).
	for _, pl := range players[:3] {
		v2, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(players[0].playerID)})
		r.Submit(pl.playerID, pl.connID, v2)
	}
	readUntilType(t, players[1].conn, wsproto.TypePhaseChanged, 2*time.Second)
	if got := r.Snapshot().Phase; got != "reveal" {
		t.Fatalf("phase = %q, want reveal (all 4 votes, including the reconnected one, counted)", got)
	}
}

// enterVoting drives a fresh 4-player game (from joinFourAndStart) through
// round 1's drawing and discussion, landing in PhaseVoting.
func enterVoting(t *testing.T, r *Room, players []testPlayer) {
	t.Helper()
	driveThroughDrawing(t, r, players, players[0])
	endDiscussion, _ := wsproto.Encode(wsproto.TypeEndDiscussion, map[string]any{})
	r.Submit(players[0].playerID, players[0].connID, endDiscussion)
	waitForPhase(t, players[0].conn, "voting")
	if got := r.Snapshot().Phase; got != "voting" {
		t.Fatalf("phase = %q, want voting", got)
	}
}
