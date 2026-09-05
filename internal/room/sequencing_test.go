package room

import (
	"testing"

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
