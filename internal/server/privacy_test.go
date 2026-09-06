package server

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// TestSecretOrdersNeverLeakBeforeResolution is the core privacy guarantee:
// during SECRET_PLANNING a player's hidden order commands and Faux flag are
// visible only to themselves, never to another player or a spectator, until
// the round actually resolves.
func TestSecretOrdersNeverLeakBeforeResolution(t *testing.T) {
	// DECLARATION and SECRET_PLANNING linger long enough to act within; the
	// rest race by.
	h := newPhasedHub(func(p game.Phase) time.Duration {
		switch p {
		case game.PhaseDeclaration, game.PhaseSecretPlanning:
			return 800 * time.Millisecond
		default:
			return 40 * time.Millisecond
		}
	})
	srv := startServer(t, h)
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)
	aID := cr.PlayerID

	aConn, aRec := connectHostRec(t, wsURL, cr)
	defer aConn.Close(websocket.StatusNormalClosure, "")
	bConn, bRec, bID := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer bConn.Close(websocket.StatusNormalClosure, "")
	cConn, _, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer cConn.Close(websocket.StatusNormalClosure, "")

	readyAndStart(t, aConn, []*websocket.Conn{aConn, bConn, cConn}, aRec)

	// A spectator joins after the match has started (a name-only join once the
	// phase is past lobby resolves to a read-only seat).
	specConn := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Watcher", Emoji: palette[3]})
	defer specConn.Close(websocket.StatusNormalClosure, "")
	specRec := recordFrames(specConn)

	// Both players' starting board is available in their match-start snapshot.
	aRec.awaitPhase(t, string(game.PhaseIncome), 3*time.Second)
	bRec.awaitPhase(t, string(game.PhaseIncome), 3*time.Second)
	aCapital, aNormal := ownedTiles(t, aRec.snapshot(), aID)
	_, bNormal := ownedTiles(t, bRec.snapshot(), bID)

	// A declares a real (non-Hold) command so it may later be marked Faux.
	aRec.awaitPhase(t, string(game.PhaseDeclaration), 3*time.Second)
	writeMsg(t, aConn, wsproto.TypeSubmitDecl, wsproto.SubmitDeclPayload{
		Command: wsproto.CommandWire{Type: string(game.CmdRecruit), To: aCapital},
	})

	// In SECRET_PLANNING, A hides a Fortify behind a Faux declaration; B hides
	// a Fortify of its own. Each then resyncs so its post-submit snapshot is
	// captured by its recorder.
	aRec.awaitPhase(t, string(game.PhaseSecretPlanning), 3*time.Second)
	writeMsg(t, aConn, wsproto.TypeSetOrders, wsproto.SetOrdersPayload{
		Faux:     true,
		Commands: []wsproto.CommandWire{{Type: string(game.CmdFortify), To: aNormal}},
	})
	writeMsg(t, bConn, wsproto.TypeSetOrders, wsproto.SetOrdersPayload{
		Faux:     false,
		Commands: []wsproto.CommandWire{{Type: string(game.CmdFortify), To: bNormal}},
	})
	// Confirm both drafts were accepted before resyncing.
	aRec.awaitType(t, wsproto.TypeOrdersSaved, 3*time.Second)
	bRec.awaitType(t, wsproto.TypeOrdersSaved, 3*time.Second)
	writeMsg(t, aConn, wsproto.TypeResync, map[string]any{})
	writeMsg(t, bConn, wsproto.TypeResync, map[string]any{})

	// The round eventually resolves; everything below is asserted against what
	// B (and the spectator) saw strictly before that point.
	bRec.awaitType(t, wsproto.TypeRoundResolved, 5*time.Second)

	// --- B must never have seen A's secret before resolution ---
	beforeResolve := framesBefore(bRec.snapshot(), wsproto.TypeRoundResolved)
	if len(beforeResolve) == 0 {
		t.Fatal("captured no B frames before round_resolved")
	}
	for _, f := range beforeResolve {
		if bytes.Contains(f.raw, []byte(`"faux":true`)) {
			t.Fatalf("B received A's Faux flag before resolution in a %s frame: %s", f.env.Type, f.raw)
		}
		var v any
		if json.Unmarshal(f.raw, &v) == nil && containsCommandTo(v, "fortify", aNormal) {
			t.Fatalf("B received A's hidden Fortify (to %s) before resolution in a %s frame", aNormal, f.env.Type)
		}
	}

	// --- B's own submitted orders ARE present in B's own snapshot ---
	bSnap, ok := findSnapshot(bRec.snapshot(), func(m map[string]any) bool {
		return m["phase"] == string(game.PhaseSecretPlanning) && m["myOrders"] != nil
	})
	if !ok {
		t.Fatal("B's own SECRET_PLANNING snapshot never carried myOrders")
	}
	if bSnap["role"] != "player" {
		t.Fatalf("B snapshot role = %v, want player", bSnap["role"])
	}

	// --- A's own snapshot has A's orders but never leaks B's ---
	aSnap, ok := findSnapshot(aRec.snapshot(), func(m map[string]any) bool {
		return m["phase"] == string(game.PhaseSecretPlanning) && m["myOrders"] != nil
	})
	if !ok {
		t.Fatal("A's own SECRET_PLANNING snapshot never carried myOrders")
	}
	if containsCommandTo(aSnap, "fortify", bNormal) {
		t.Fatalf("A's snapshot leaked B's hidden Fortify (to %s)", bNormal)
	}

	// --- A spectator sees a read-only seat with no private drafts ---
	specSnapFrame := specRec.awaitType(t, wsproto.TypeStateSnapshot, 3*time.Second)
	var specSnap map[string]any
	if err := json.Unmarshal(specSnapFrame.env.Payload, &specSnap); err != nil {
		t.Fatalf("unmarshal spectator snapshot: %v", err)
	}
	if specSnap["role"] != "spectator" {
		t.Fatalf("spectator role = %v, want spectator", specSnap["role"])
	}
	if _, present := specSnap["myOrders"]; present {
		t.Fatal("spectator snapshot must not carry myOrders")
	}
	if _, present := specSnap["myDeclaration"]; present {
		t.Fatal("spectator snapshot must not carry myDeclaration")
	}
	// And no spectator frame before resolution leaked A's secret either.
	for _, f := range framesBefore(specRec.snapshot(), wsproto.TypeRoundResolved) {
		if bytes.Contains(f.raw, []byte(`"faux":true`)) {
			t.Fatalf("spectator received a Faux flag before resolution: %s", f.raw)
		}
		var v any
		if json.Unmarshal(f.raw, &v) == nil && containsCommandTo(v, "fortify", aNormal) {
			t.Fatalf("spectator received A's hidden Fortify before resolution")
		}
	}
}

// framesBefore returns the recorded frames that precede the first frame of the
// given type.
func framesBefore(frames []recordedFrame, typ string) []recordedFrame {
	for i, f := range frames {
		if f.env.Type == typ {
			return frames[:i]
		}
	}
	return frames
}

// findSnapshot returns the first state_snapshot payload (decoded to a map)
// matching pred.
func findSnapshot(frames []recordedFrame, pred func(map[string]any) bool) (map[string]any, bool) {
	for _, f := range frames {
		if f.env.Type != wsproto.TypeStateSnapshot {
			continue
		}
		var m map[string]any
		if json.Unmarshal(f.env.Payload, &m) != nil {
			continue
		}
		if pred(m) {
			return m, true
		}
	}
	return nil, false
}
