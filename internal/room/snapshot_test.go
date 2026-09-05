package room

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

const twoSeconds = 2 * time.Second

// requestResync submits a resync command on tp's own connection and returns
// the state_snapshot payload it gets back (as a raw map, for field-presence
// assertions the typed PlayerView/etc structs would hide).
func requestResync(t *testing.T, r *Room, tp testPlayer) map[string]any {
	t.Helper()
	env, _ := wsproto.Encode(wsproto.TypeResync, map[string]any{})
	r.Submit(tp.playerID, tp.connID, env)
	snap := readUntilType(t, tp.conn, wsproto.TypeStateSnapshot, twoSeconds)
	var p map[string]any
	if err := json.Unmarshal(snap.Payload, &p); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	return p
}

// --- Requirement #1/#2: initial attach and reconnect receive a complete snapshot ---

func TestInitialJoinReceivesCompleteSnapshot(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	client, conn := dialTestConn(t)
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})

	snap := readUntilType(t, client, wsproto.TypeStateSnapshot, twoSeconds)
	var p map[string]any
	if err := json.Unmarshal(snap.Payload, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p["phase"] != "lobby" {
		t.Fatalf("phase = %v, want lobby", p["phase"])
	}
	if p["hostId"] != string(hostID) {
		t.Fatalf("hostId = %v, want %s", p["hostId"], hostID)
	}
	you, _ := p["you"].(map[string]any)
	if you == nil || you["id"] != string(res.PlayerID) {
		t.Fatalf("you = %v, want id %s", p["you"], res.PlayerID)
	}
	if snap.RoomID != r.Code {
		t.Fatalf("envelope roomId = %q, want %q", snap.RoomID, r.Code)
	}
	if snap.Version != wsproto.ProtocolVersion {
		t.Fatalf("envelope version = %d, want %d", snap.Version, wsproto.ProtocolVersion)
	}
}

func TestReconnectReceivesLatestCompleteSnapshot(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0]

	// Advance state a bit (a stroke) before the reconnect, so the snapshot
	// the reconnect gets back must reflect it, not stale lobby-era state.
	stroke, _ := wsproto.Encode(wsproto.TypeStroke, wsproto.StrokePayload{Points: []wsproto.Point{{X: 0.1, Y: 0.1}}})
	r.Submit(host.playerID, host.connID, stroke)
	readUntilType(t, players[1].conn, wsproto.TypeTurnChanged, twoSeconds)

	disconnect(r, host)
	reconnected := reconnectPlayer(t, r, host)

	p := requestResync(t, r, reconnected)
	if p["phase"] != "drawing" {
		t.Fatalf("phase = %v, want drawing", p["phase"])
	}
	strokes, _ := p["strokes"].([]any)
	if len(strokes) != 1 {
		t.Fatalf("strokes = %v, want 1 recorded stroke", p["strokes"])
	}
	cur, _ := p["currentPlayer"].(string)
	if cur == "" {
		t.Fatal("currentPlayer missing from snapshot during drawing")
	}
}

// --- Requirement #3: snapshot reconstructs every supported phase ---

func TestSnapshotReconstructsEachPhase(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0]

	if p := requestResync(t, r, host); p["phase"] != "drawing" || p["currentPlayer"] == nil {
		t.Fatalf("drawing snapshot incomplete: %+v", p)
	}

	driveThroughDrawing(t, r, players, players[0]) // returns once the phase has already left drawing
	if p := requestResync(t, r, host); p["phase"] != "discussion" || p["discussionDeadlineMs"] == nil {
		t.Fatalf("discussion snapshot incomplete: %+v", p)
	}

	endDiscussion, _ := wsproto.Encode(wsproto.TypeEndDiscussion, map[string]any{})
	r.Submit(host.playerID, host.connID, endDiscussion)
	waitForPhase(t, players[0].conn, "voting")
	p := requestResync(t, r, host)
	if p["phase"] != "voting" {
		t.Fatalf("voting snapshot phase = %v", p["phase"])
	}
	if p["hasVoted"] != false {
		t.Fatalf("hasVoted = %v, want false before voting", p["hasVoted"])
	}
	targets, _ := p["voteTargets"].([]any)
	if len(targets) != 3 { // everyone except self
		t.Fatalf("voteTargets = %v, want 3 entries", targets)
	}

	// Vote everyone for the host so the round resolves deterministically
	// regardless of who this round's impostor is.
	for _, pl := range players {
		v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(host.playerID)})
		r.Submit(pl.playerID, pl.connID, v)
	}
	waitForPhase(t, players[0].conn, "reveal")
	p = requestResync(t, r, host)
	if p["phase"] != "reveal" {
		t.Fatalf("reveal snapshot phase = %v", p["phase"])
	}
	if p["lastResult"] == nil {
		t.Fatal("reveal snapshot missing lastResult")
	}
}

// --- Requirement #4/#5: redaction is correct for impostor vs non-impostor, and viewer-specific ---

func TestSnapshotHidesWordFromImpostorOnly(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players, impostor := joinFourAndStartKnowingImpostor(t, r, hostID, hostToken)

	for _, pl := range players {
		p := requestResync(t, r, pl)
		_, hasWord := p["word"]
		isImpostor := pl.playerID == impostor
		if isImpostor && hasWord {
			t.Fatalf("impostor %s received the word in their snapshot: %+v", pl.playerID, p)
		}
		if !isImpostor && !hasWord {
			t.Fatalf("non-impostor %s did not receive the word: %+v", pl.playerID, p)
		}
		if p["youAreImpostor"] != isImpostor {
			t.Fatalf("youAreImpostor = %v for %s, want %v", p["youAreImpostor"], pl.playerID, isImpostor)
		}
	}
}

// TestSnapshotHidesPastWordFromThatRoundsImpostorAfterRoundEnds proves a
// real bug found while consolidating redaction logic: the old room_state
// builder dumped the entire LastResult (including Word) unconditionally,
// so a round's own impostor could learn the secret word simply by
// refreshing or reconnecting any time after their round ended — even
// though the live round_result broadcast correctly withheld it from them
// in the moment. lastResult must stay redacted for that same impostor for
// as long as it's the most recent result.
func TestSnapshotHidesPastWordFromThatRoundsImpostorAfterRoundEnds(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players, impostor := joinFourAndStartKnowingImpostor(t, r, hostID, hostToken)
	enterVoting(t, r, players)

	// Vote for someone who is NOT the impostor, so the round resolves
	// immediately as "not caught" (no pending guess to wait on).
	var notImpostor game.PlayerID
	for _, pl := range players {
		if pl.playerID != impostor {
			notImpostor = pl.playerID
			break
		}
	}
	for _, pl := range players {
		v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(notImpostor)})
		r.Submit(pl.playerID, pl.connID, v)
	}
	waitForPhase(t, players[0].conn, "reveal")

	imp := playerConn(players, impostor)
	p := requestResync(t, r, imp)
	lastResult, _ := p["lastResult"].(map[string]any)
	if lastResult == nil {
		t.Fatal("snapshot missing lastResult after round ended")
	}
	if word, ok := lastResult["word"].(string); ok && word != "" {
		t.Fatalf("the round's own impostor received the word via lastResult: %+v", lastResult)
	}
}

// --- Requirement #5 (transport hygiene): secrets and internal ids never appear on the wire ---

func TestSnapshotNeverLeaksReconnectCredentialsOrConnectionIDs(t *testing.T) {
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", longTestDurations())
	startTestRoom(t, r)
	players := joinFourAndStart(t, r, hostID, hostToken)
	host := players[0]

	env, _ := wsproto.Encode(wsproto.TypeResync, map[string]any{})
	r.Submit(host.playerID, host.connID, env)
	snap := readUntilType(t, host.conn, wsproto.TypeStateSnapshot, twoSeconds)
	raw, _ := json.Marshal(snap)
	if strings.Contains(string(raw), hostToken) {
		t.Fatalf("snapshot leaked the raw reconnect token: %s", raw)
	}
	for _, field := range []string{"reconnectToken", "tokenHash", "connId", "connID"} {
		if strings.Contains(string(raw), field) {
			t.Fatalf("snapshot leaked internal field %q: %s", field, raw)
		}
	}
}

// --- Requirement #6/#7/#8: revision semantics ---

func TestRevisionIncreasesExactlyOncePerAcceptedCommand(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	client, conn := dialTestConn(t)
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	readUntilType(t, client, wsproto.TypeStateSnapshot, twoSeconds)
	before := r.Snapshot().Revision

	joinNewPlayer(t, r, "P2") // an accepted join is one command

	after := r.Snapshot().Revision
	if after != before+1 {
		t.Fatalf("revision = %d after one join, want %d", after, before+1)
	}
}

func TestRejectedCommandsAndSnapshotRequestsDoNotBumpRevision(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	nonHost := joinNewPlayer(t, r, "P2")

	before := r.Snapshot().Revision

	// A non-host trying to start the game is rejected (not enough players
	// too, but ErrNotHost fires first either way) — must not bump.
	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(nonHost.playerID, nonHost.connID, start)

	// An explicit resync is read-only.
	requestResync(t, r, host)

	// A malformed stroke payload is rejected before reaching the engine.
	bad, _ := wsproto.Encode(wsproto.TypeStroke, map[string]any{"points": "not-an-array"})
	r.Submit(host.playerID, host.connID, bad)
	readUntilType(t, host.conn, wsproto.TypeError, twoSeconds)

	after := r.Snapshot().Revision
	if after != before {
		t.Fatalf("revision = %d after only rejected/read-only actions, want unchanged %d", after, before)
	}
}

func TestAllRecipientsObserveSameRevisionForOneTransition(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	host := joinAndPumpHost(t, r, hostID, hostToken)
	p2 := joinNewPlayer(t, r, "P2")
	p3 := joinNewPlayer(t, r, "P3")
	p4 := joinNewPlayer(t, r, "P4")
	players := []testPlayer{host, p2, p3, p4}

	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(host.playerID, host.connID, start)

	seqs := map[string]int64{}
	for _, pl := range players {
		env := readUntilType(t, pl.conn, wsproto.TypeRoundStarted, twoSeconds)
		seqs[string(pl.playerID)] = env.Seq
	}
	first := seqs[string(host.playerID)]
	for id, seq := range seqs {
		if seq != first {
			t.Fatalf("seq for %s = %d, want %d (same as everyone else) even though round_started payloads are redacted differently", id, seq, first)
		}
	}
}

// joinAndPumpHost joins the pre-seeded host seat and drains its snapshot,
// without the extra host-migration search join.NewPlayer does (the host is
// never subject to migration on their own first connection).
func joinAndPumpHost(t *testing.T, r *Room, hostID game.PlayerID, hostToken string) testPlayer {
	t.Helper()
	client, conn := dialTestConn(t)
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client)
	return testPlayer{conn: client, playerID: res.PlayerID, connID: res.ConnID, token: hostToken}
}
