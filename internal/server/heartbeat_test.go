package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

const twoSeconds = 2 * time.Second

// fastHeartbeat is a heartbeat window short enough to observe within a normal
// test timeout without sleeping for anything close to production timing.
func fastHeartbeat() HeartbeatConfig {
	return HeartbeatConfig{Interval: 30 * time.Millisecond, Timeout: 150 * time.Millisecond}
}

func TestValidPongKeepsConnectionAlive(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, WithHeartbeat(fastHeartbeat())).Handler())
	defer srv.Close()
	defer h.Close()

	cr := createTestRoom(t, srv, "Host")
	host := dialJoin(t, wsURLFor(srv, cr.Code), wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer host.CloseNow()
	ch := envelopeStream(host) // one continuous reader, as a real client keeps

	waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })

	// Outlive several heartbeat cycles with nothing but pings/pongs at the
	// transport level.
	time.Sleep(8 * fastHeartbeat().Interval)

	// The connection must still be genuinely alive end-to-end: a fresh command
	// still gets a normal response.
	writeMsg(t, host, wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "still connected"})
	env := waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeChatBroadcast })
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["text"] != "still connected" {
		t.Fatalf("chat after heartbeat cycles = %v", p)
	}
}

func TestMissingHeartbeatResponseTriggersPresenceDisconnect(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, WithHeartbeat(fastHeartbeat())).Handler())
	defer srv.Close()
	defer h.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)
	host := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer host.CloseNow()
	ch := envelopeStream(host)
	waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })

	// Bob joins, then goes silent — never reads again, so the server's pings to
	// him never get answered, simulating a vanished tab/process/network.
	bob := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Bob", Emoji: "🐙"})
	defer bob.CloseNow()

	waitForEnvelope(t, ch, 3*time.Second, func(e wsproto.Envelope) bool {
		if e.Type != wsproto.TypePlayerPresenceChanged {
			return false
		}
		var p map[string]any
		_ = json.Unmarshal(e.Payload, &p)
		return p["connected"] == false
	})
}

func TestStaleConnectionHeartbeatCannotAffectReplacement(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, WithHeartbeat(fastHeartbeat())).Handler())
	defer srv.Close()
	defer h.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)
	old := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	readUntil(t, old, wsproto.TypeStateSnapshot)

	// Reconnecting replaces and closes the old connection server-side; its own
	// heartbeat goroutine's next ping simply fails against the closed socket.
	replacement := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer replacement.CloseNow()
	ch := envelopeStream(replacement)
	waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })

	// Outlive the old connection's heartbeat window, then confirm the
	// replacement was never marked disconnected by anything left over.
	time.Sleep(3 * fastHeartbeat().Timeout)
	writeMsg(t, replacement, wsproto.TypeResync, map[string]any{})

	env := waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	you, _ := p["you"].(map[string]any)
	if you == nil {
		t.Fatalf("snapshot missing you: %+v", p)
	}
	if you["connected"] != true {
		t.Fatalf("replacement connection's own seat shows connected=%v, want true", you["connected"])
	}
}
