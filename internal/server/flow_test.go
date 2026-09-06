package server

import (
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// TestFullMatchProgressesToGameOver plays a full 3-player match on fast
// phases: everyone readies, the host starts, and the server timers drive the
// phase machine on their own to GAME_OVER. It submits a declaration and orders
// for one player during the right phases, asserts the phase machine advanced
// through the expected phases, and verifies that one continuously-connected
// player observed a gap-free sequence of revision numbers.
func TestFullMatchProgressesToGameOver(t *testing.T) {
	// Let the two interactive phases linger long enough to act within; the
	// rest race by so the whole match finishes in a couple of seconds.
	h := newPhasedHub(func(p game.Phase) time.Duration {
		switch p {
		case game.PhaseDeclaration, game.PhaseSecretPlanning:
			return 100 * time.Millisecond
		default:
			return 25 * time.Millisecond
		}
	})
	srv := startServer(t, h)
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)

	host, hostRec := connectHostRec(t, wsURL, cr)
	defer host.Close(websocket.StatusNormalClosure, "")
	p2, _, _ := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer p2.Close(websocket.StatusNormalClosure, "")
	p3, p3Rec, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer p3.Close(websocket.StatusNormalClosure, "")

	// Quick preset (6 rounds) before anyone readies (settings clears ready).
	writeMsg(t, host, wsproto.TypeUpdateSettings, wsproto.UpdateSettingsPayload{Preset: string(game.PresetQuick)})
	time.Sleep(50 * time.Millisecond)

	readyAndStart(t, host, []*websocket.Conn{host, p2, p3}, hostRec)

	// Drive one declaration + one orders submission during the right phases.
	// A Hold declaration and empty (Hold-only) orders are always legal
	// regardless of the board, so this needs no per-tile knowledge.
	done := make(chan struct{})
	go func() {
		defer close(done)
		submittedDecl, submittedOrders := false, false
		deadline := time.Now().Add(25 * time.Second)
		for time.Now().Before(deadline) {
			if hostRec.hasType(wsproto.TypeGameOver) {
				return
			}
			if !submittedDecl && hostRec.sawPhase(string(game.PhaseDeclaration)) {
				tryWrite(p2, wsproto.TypeSubmitDecl, wsproto.SubmitDeclPayload{
					Command: wsproto.CommandWire{Type: string(game.CmdHold)},
				})
				submittedDecl = true
			}
			if !submittedOrders && hostRec.sawPhase(string(game.PhaseSecretPlanning)) {
				tryWrite(p2, wsproto.TypeSetOrders, wsproto.SetOrdersPayload{Faux: false})
				submittedOrders = true
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// The match must reach GAME_OVER on its own.
	hostRec.awaitType(t, wsproto.TypeGameOver, 25*time.Second)
	<-done

	// The phase machine must have visibly advanced through the key phases.
	for _, phase := range []string{
		string(game.PhaseDeclaration),
		string(game.PhaseSecretPlanning),
		string(game.PhaseResolution),
		string(game.PhaseGameOver),
	} {
		if !hostRec.sawPhase(phase) {
			t.Fatalf("host never observed phase %q during the match", phase)
		}
	}
	if !hostRec.hasType(wsproto.TypeRoundResolved) {
		t.Fatal("host never observed a round_resolved event")
	}

	assertContiguousSeqs(t, p3Rec.snapshot())
}

// assertContiguousSeqs verifies that, from the first state_snapshot onward, the
// Seq stamped on a single connection's frames never goes backwards and never
// skips a value: each distinct revision differs from the previous by exactly
// one (unsequenced frames repeat the current revision; a gap would mean a
// dropped sequenced broadcast).
func assertContiguousSeqs(t *testing.T, frames []recordedFrame) {
	t.Helper()
	started := false
	var prev int64
	increments := 0
	for _, f := range frames {
		if !started {
			if f.env.Type == wsproto.TypeStateSnapshot {
				started = true
				prev = f.env.Seq
			}
			continue
		}
		s := f.env.Seq
		switch {
		case s == prev:
			// unsequenced frame at the current revision
		case s == prev+1:
			prev = s
			increments++
		case s < prev:
			t.Fatalf("seq went backwards: %d after %d (type %s)", s, prev, f.env.Type)
		default:
			t.Fatalf("seq gap: jumped from %d to %d (type %s)", prev, s, f.env.Type)
		}
	}
	if increments < 5 {
		t.Fatalf("expected many sequenced revisions over a full match, saw %d increments", increments)
	}
}
