package server

import (
	"encoding/json"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// TestReconnectDuringSecretPlanningRestoresOrders proves a player who submits
// orders, drops, and reconnects mid-phase gets their own draft back.
func TestReconnectDuringSecretPlanningRestoresOrders(t *testing.T) {
	h := newPhasedHub(func(p game.Phase) time.Duration {
		if p == game.PhaseSecretPlanning {
			return 3 * time.Second
		}
		return 40 * time.Millisecond
	})
	srv := startServer(t, h)
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)

	aConn, aRec := connectHostRec(t, wsURL, cr)
	defer aConn.Close(websocket.StatusNormalClosure, "")
	bConn, bRec, bID := joinPlayerRec(t, wsURL, "Bob", palette[1])
	bToken := reconnectTokenFrom(t, bRec)
	cConn, _, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer cConn.Close(websocket.StatusNormalClosure, "")

	readyAndStart(t, aConn, []*websocket.Conn{aConn, bConn, cConn}, aRec)
	bRec.awaitPhase(t, string(game.PhaseIncome), 3*time.Second)
	_, bNormal := ownedTiles(t, bRec.snapshot(), bID)

	bRec.awaitPhase(t, string(game.PhaseSecretPlanning), 3*time.Second)
	writeMsg(t, bConn, wsproto.TypeSetOrders, wsproto.SetOrdersPayload{
		Faux:     false,
		Commands: []wsproto.CommandWire{{Type: string(game.CmdFortify), To: bNormal}},
	})
	bRec.awaitType(t, wsproto.TypeOrdersSaved, 3*time.Second)

	// Bob drops and immediately reconnects (still within SECRET_PLANNING).
	bConn.Close(websocket.StatusNormalClosure, "reconnecting")
	bConn2 := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: bID, ReconnectToken: bToken})
	defer bConn2.Close(websocket.StatusNormalClosure, "")

	snap := readUntil(t, bConn2, wsproto.TypeStateSnapshot)
	var m map[string]any
	if err := json.Unmarshal(snap.Payload, &m); err != nil {
		t.Fatalf("unmarshal reconnect snapshot: %v", err)
	}
	if m["myOrders"] == nil {
		t.Fatalf("reconnect snapshot did not restore myOrders: %s", snap.Payload)
	}
	if !containsCommandTo(m, "fortify", bNormal) {
		t.Fatalf("reconnect snapshot's myOrders did not include the submitted Fortify (to %s)", bNormal)
	}
}

// TestPhaseTimeoutAdvancesWithoutSubmissions proves the match never hangs when
// nobody submits: deadlines auto-Hold and the round still resolves and the
// match still ends.
func TestPhaseTimeoutAdvancesWithoutSubmissions(t *testing.T) {
	srv := startServer(t, newFastHub(40*time.Millisecond))
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)

	aConn, aRec := connectHostRec(t, wsURL, cr)
	defer aConn.Close(websocket.StatusNormalClosure, "")
	bConn, _, _ := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer bConn.Close(websocket.StatusNormalClosure, "")
	cConn, _, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer cConn.Close(websocket.StatusNormalClosure, "")

	readyAndStart(t, aConn, []*websocket.Conn{aConn, bConn, cConn}, aRec)

	// No player ever submits a declaration or orders.
	aRec.awaitType(t, wsproto.TypeGameOver, 25*time.Second)

	for _, phase := range []string{
		string(game.PhaseDeclarationReveal),
		string(game.PhaseSecretPlanning),
		string(game.PhaseResolution),
	} {
		if !aRec.sawPhase(phase) {
			t.Fatalf("match never advanced through %q despite no submissions", phase)
		}
	}
	if !aRec.hasType(wsproto.TypeRoundResolved) {
		t.Fatal("a round never resolved despite no submissions")
	}
}

// TestResignInLobbyExitsAndInvalidatesReconnect proves resign_match broadcasts
// a forfeited player_exited and permanently invalidates the resigner's
// reconnect token. (The leave_accepted acknowledgment the resigner is meant to
// receive is covered separately — see TestResignAcksResigningPlayer.)
func TestResignInLobbyExitsAndInvalidatesReconnect(t *testing.T) {
	srv := startServer(t, hub.New())
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)

	aConn, aRec := connectHostRec(t, wsURL, cr)
	defer aConn.Close(websocket.StatusNormalClosure, "")
	bConn, _, _ := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer bConn.Close(websocket.StatusNormalClosure, "")
	cConn, cRec, cID := joinPlayerRec(t, wsURL, "Cara", palette[2])
	cToken := reconnectTokenFrom(t, cRec)
	defer cConn.Close(websocket.StatusNormalClosure, "")

	writeMsg(t, cConn, wsproto.TypeResignMatch, map[string]any{})

	// The host observes Cara's permanent exit.
	deadline := time.Now().Add(3 * time.Second)
	sawExit := false
	for time.Now().Before(deadline) && !sawExit {
		for _, f := range aRec.snapshot() {
			if f.env.Type != wsproto.TypePlayerExited {
				continue
			}
			var p wsproto.PlayerExitedPayload
			_ = json.Unmarshal(f.env.Payload, &p)
			if p.ID == cID && p.Forfeited {
				sawExit = true
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !sawExit {
		t.Fatalf("host never observed a forfeited player_exited for %s", cID)
	}

	// Cara's old token no longer works.
	reConn := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cID, ReconnectToken: cToken})
	defer reConn.CloseNow()
	_, code := readErrorFrame(t, reConn)
	if code != "invalid_reconnect" {
		t.Fatalf("reconnect after resign: error code = %q, want invalid_reconnect", code)
	}
}

// TestResignAcksResigningPlayer covers the contract that a resigning player
// receives a leave_accepted so they can tear down cleanly. The server no longer
// closes the socket itself on resign (that raced the write pump and dropped the
// ack) — the client closes as it returns home — so the ack now flushes.
func TestResignAcksResigningPlayer(t *testing.T) {
	srv := startServer(t, hub.New())
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)

	aConn, _ := connectHostRec(t, wsURL, cr)
	defer aConn.Close(websocket.StatusNormalClosure, "")
	bConn, _, _ := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer bConn.Close(websocket.StatusNormalClosure, "")
	cConn, cRec, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer cConn.Close(websocket.StatusNormalClosure, "")

	writeMsg(t, cConn, wsproto.TypeResignMatch, map[string]any{})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, f := range cRec.snapshot() {
			if f.env.Type == wsproto.TypeLeaveAccepted {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("resigning player never received leave_accepted")
}

// TestHostDisconnectMigratesHostAfterGrace proves an in-match host disconnect,
// once its reconnect grace expires, migrates the host to another connected
// player (emitting host_changed).
func TestHostDisconnectMigratesHostAfterGrace(t *testing.T) {
	t.Setenv("FAUXTIST_RECONNECT_GRACE_MS", "80")
	// Keep phases long so the match stays comfortably in progress while the
	// 80ms grace window elapses.
	srv := startServer(t, newFastHub(500*time.Millisecond))
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)
	aID := cr.PlayerID

	aConn, aRec := connectHostRec(t, wsURL, cr)
	bConn, bRec, bID := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer bConn.Close(websocket.StatusNormalClosure, "")
	cConn, _, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer cConn.Close(websocket.StatusNormalClosure, "")

	readyAndStart(t, aConn, []*websocket.Conn{aConn, bConn, cConn}, aRec)
	aRec.awaitPhase(t, string(game.PhaseIncome), 3*time.Second)

	// The host vanishes mid-match.
	aConn.Close(websocket.StatusNormalClosure, "host gone")

	hc := bRec.awaitType(t, wsproto.TypeHostChanged, 3*time.Second)
	var p wsproto.HostChangedPayload
	_ = json.Unmarshal(hc.env.Payload, &p)
	if p.HostID == aID {
		t.Fatalf("host_changed kept the departed host %s", aID)
	}
	if p.HostID != bID {
		t.Fatalf("host migrated to %s, want the earliest-joined connected player %s", p.HostID, bID)
	}
}

// TestLateJoinerBecomesSpectator proves a name-only join once the match has
// started resolves to a read-only spectator seat.
func TestLateJoinerBecomesSpectator(t *testing.T) {
	srv := startServer(t, newFastHub(500*time.Millisecond))
	cr := createTestRoom(t, srv, "Alice")
	wsURL := wsURLFor(srv, cr.Code)

	aConn, aRec := connectHostRec(t, wsURL, cr)
	defer aConn.Close(websocket.StatusNormalClosure, "")
	bConn, _, _ := joinPlayerRec(t, wsURL, "Bob", palette[1])
	defer bConn.Close(websocket.StatusNormalClosure, "")
	cConn, _, _ := joinPlayerRec(t, wsURL, "Cara", palette[2])
	defer cConn.Close(websocket.StatusNormalClosure, "")

	readyAndStart(t, aConn, []*websocket.Conn{aConn, bConn, cConn}, aRec)
	aRec.awaitPhase(t, string(game.PhaseIncome), 3*time.Second)

	late := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Latecomer", Emoji: palette[4]})
	defer late.Close(websocket.StatusNormalClosure, "")

	accepted := readUntil(t, late, wsproto.TypeJoinAccepted)
	var ap wsproto.JoinAcceptedPayload
	_ = json.Unmarshal(accepted.Payload, &ap)
	if !ap.Spectator {
		t.Fatal("a join after match start must be accepted as a spectator")
	}
	snap := readUntil(t, late, wsproto.TypeStateSnapshot)
	var m map[string]any
	_ = json.Unmarshal(snap.Payload, &m)
	if m["role"] != "spectator" {
		t.Fatalf("late joiner snapshot role = %v, want spectator", m["role"])
	}
}
