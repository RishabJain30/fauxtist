package room

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// joinFourAndStartKnowingImpostor is like joinFourAndStart, but also
// determines which player is round 1's impostor by reading each player's
// own round_started frame — the server sends youAreImpostor:true only to
// that one player, so this doesn't need to guess or hardcode a seed.
func joinFourAndStartKnowingImpostor(t *testing.T, r *Room, hostID game.PlayerID, hostToken string) (players []testPlayer, impostor game.PlayerID) {
	t.Helper()
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client1)
	host := testPlayer{conn: client1, playerID: hostID, connID: res1.ConnID, token: hostToken}
	players = []testPlayer{host, joinNewPlayer(t, r, "P2"), joinNewPlayer(t, r, "P3"), joinNewPlayer(t, r, "P4")}

	start, _ := wsproto.Encode(wsproto.TypeStartGame, map[string]any{})
	r.Submit(host.playerID, host.connID, start)

	for _, p := range players {
		env := readUntilType(t, p.conn, wsproto.TypeRoundStarted, 2*time.Second)
		var payload map[string]any
		_ = json.Unmarshal(env.Payload, &payload)
		if payload["youAreImpostor"] == true {
			impostor = p.playerID
		}
		readUntilType(t, p.conn, wsproto.TypeTurnChanged, 2*time.Second)
	}
	return players, impostor
}

// voteEveryoneFor submits a vote from every player for target — a
// unanimous vote guarantees that player is caught by plurality.
func voteEveryoneFor(r *Room, players []testPlayer, target game.PlayerID) {
	for _, pl := range players {
		v, _ := wsproto.Encode(wsproto.TypeCastVote, wsproto.VotePayload{Target: string(target)})
		r.Submit(pl.playerID, pl.connID, v)
	}
}

func playerConn(players []testPlayer, id game.PlayerID) testPlayer {
	for _, p := range players {
		if p.playerID == id {
			return p
		}
	}
	return testPlayer{}
}

func scoreOf(players []wsproto.PlayerView, id game.PlayerID) int {
	for _, p := range players {
		if p.ID == string(id) {
			return p.Score
		}
	}
	return -1
}

// --- Requirement #15: a caught impostor timing out is treated as an incorrect guess ---

func TestCaughtImpostorTimeoutResolvesAsIncorrectGuess(t *testing.T) {
	durations := longTestDurations()
	durations.ImpostorGuess = 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", durations)
	startTestRoom(t, r)
	players, impostor := joinFourAndStartKnowingImpostor(t, r, hostID, hostToken)
	enterVoting(t, r, players)
	voteEveryoneFor(r, players, impostor)
	waitForPhase(t, players[0].conn, "reveal")

	// The first round_result (entering reveal, awaiting a guess) doesn't
	// carry impostorTimedOut at all; keep reading until the final one
	// (after the timeout resolves) does.
	var p map[string]any
	for {
		env := readUntilType(t, players[0].conn, wsproto.TypeRoundResult, 2*time.Second)
		_ = json.Unmarshal(env.Payload, &p)
		if _, ok := p["impostorTimedOut"]; ok {
			break
		}
	}
	if p["impostorTimedOut"] != true {
		t.Fatalf("impostorTimedOut = %v, want true", p["impostorTimedOut"])
	}
	if p["impostorGuessedRight"] != false {
		t.Fatalf("impostorGuessedRight = %v, want false", p["impostorGuessedRight"])
	}
	scores, _ := p["scoreDelta"].(map[string]any)
	for id, delta := range scores {
		want := 1.0
		if id == string(impostor) {
			want = 0
		}
		if delta != want {
			t.Fatalf("scoreDelta[%s] = %v, want %v", id, delta, want)
		}
	}
}

// --- Requirement #16: guessing before the deadline prevents timeout resolution ---

func TestGuessBeforeDeadlinePreventsTimeoutResolution(t *testing.T) {
	durations := longTestDurations()
	durations.ImpostorGuess = 200 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", durations)
	startTestRoom(t, r)
	players, impostor := joinFourAndStartKnowingImpostor(t, r, hostID, hostToken)
	enterVoting(t, r, players)
	voteEveryoneFor(r, players, impostor)
	waitForPhase(t, players[0].conn, "reveal")

	impConn := playerConn(players, impostor)
	guess, _ := wsproto.Encode(wsproto.TypeImpostorGuess, wsproto.ImpostorGuessPayload{Guess: "not-the-word"})
	r.Submit(impConn.playerID, impConn.connID, guess)

	var p map[string]any
	for {
		env := readUntilType(t, players[0].conn, wsproto.TypeRoundResult, 2*time.Second)
		_ = json.Unmarshal(env.Payload, &p)
		if g, ok := p["impostorGuess"]; ok && g != "" {
			break
		}
	}
	if p["impostorTimedOut"] != false {
		t.Fatalf("impostorTimedOut = %v, want false (resolved by a real guess)", p["impostorTimedOut"])
	}

	// Wait past the original deadline: no further/duplicate resolution.
	time.Sleep(durations.ImpostorGuess + 150*time.Millisecond)
	if got := r.Snapshot().Phase; got != "reveal" {
		t.Fatalf("phase = %q, want still reveal (holding for the reveal-hold timer, no double-resolution)", got)
	}
}

// --- Requirement #17: a stale timer event after a phase transition is ignored ---

func TestStaleGuessTimeoutIgnoredAfterPhaseTransition(t *testing.T) {
	durations := longTestDurations()
	durations.ImpostorGuess = 30 * time.Millisecond
	durations.Reveal = 30 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", durations)
	startTestRoom(t, r)
	players, impostor := joinFourAndStartKnowingImpostor(t, r, hostID, hostToken)
	enterVoting(t, r, players)
	voteEveryoneFor(r, players, impostor)
	waitForPhase(t, players[0].conn, "reveal")

	// Let round 1 resolve via the real timeout, then let the reveal hold
	// advance into round 2.
	readUntilType(t, players[0].conn, wsproto.TypeRoundStarted, 3*time.Second)
	scoreBefore := scoreOf(r.Snapshot().Players, impostor)

	// Inject a STALE guess-timeout for round 1 directly into the channel —
	// exactly what a slow real timer firing late would send (round 2 has
	// since bumped the generation past 1). It must be dropped.
	r.guessTimeoutCh <- guessTimeoutMsg{roundGen: 1}
	time.Sleep(150 * time.Millisecond)

	if got := scoreOf(r.Snapshot().Players, impostor); got != scoreBefore {
		t.Fatalf("impostor score changed from %d to %d — a stale round-1 timeout affected round 2", scoreBefore, got)
	}
	if got := r.Snapshot().Phase; got != "drawing" {
		t.Fatalf("phase = %q, want drawing (round 2, unaffected by the stale round-1 timer)", got)
	}
}

// --- Requirement #18: presence and host-change events carry no secrets ---

func TestPresenceAndHostChangedEventsCarryNoSecrets(t *testing.T) {
	grace := 40 * time.Millisecond
	r, hostID, hostToken := newTestRoomWithDurations(t, "Host", shortGraceDurations(grace))
	startTestRoom(t, r)
	client1, conn1 := dialTestConn(t)
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client1)
	host := testPlayer{playerID: hostID, connID: res1.ConnID, token: hostToken}
	bob := joinNewPlayer(t, r, "Bob")

	disconnect(r, host)
	presenceEnv := readUntilType(t, bob.conn, wsproto.TypePlayerPresenceChanged, 2*time.Second)
	if strings.Contains(string(presenceEnv.Payload), hostToken) {
		t.Fatal("player_presence_changed leaked the raw reconnect token")
	}
	var pp map[string]any
	_ = json.Unmarshal(presenceEnv.Payload, &pp)
	for k := range pp {
		if k != "id" && k != "connected" {
			t.Fatalf("player_presence_changed carries an unexpected field %q", k)
		}
	}

	hostChangedEnv := readUntilType(t, bob.conn, wsproto.TypeHostChanged, 2*time.Second)
	if strings.Contains(string(hostChangedEnv.Payload), hostToken) {
		t.Fatal("host_changed leaked the raw reconnect token")
	}
	var hp map[string]any
	_ = json.Unmarshal(hostChangedEnv.Payload, &hp)
	for k := range hp {
		if k != "hostId" {
			t.Fatalf("host_changed carries an unexpected field %q", k)
		}
	}
}
