package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

const twoSeconds = 2 * time.Second

// fastHeartbeat is a heartbeat window short enough to observe within a
// normal test timeout without sleeping for anything close to production
// timing.
func fastHeartbeat() HeartbeatConfig {
	return HeartbeatConfig{Interval: 30 * time.Millisecond, Timeout: 150 * time.Millisecond}
}

// envelopeStream continuously reads c in the background and delivers
// decoded envelopes to the returned channel (closed once the connection
// ends). The read itself must never be given a deadline: nhooyr.io/
// websocket's Conn treats any error from any method — including a context
// timeout — as fatal to the whole connection ("this applies to context
// expirations as well unfortunately", per its own doc). A real client
// (browser or otherwise) always keeps exactly one blocking Read
// outstanding for this reason; tests that need to idle through several
// heartbeat cycles while staying "alive" must do the same, so waiting is
// done on the channel instead of on the read.
func envelopeStream(c *websocket.Conn) <-chan wsproto.Envelope {
	ch := make(chan wsproto.Envelope, 64)
	go func() {
		defer close(ch)
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var env wsproto.Envelope
			if json.Unmarshal(data, &env) == nil {
				ch <- env
			}
		}
	}()
	return ch
}

// waitForEnvelope drains ch until pred matches, the connection closes, or
// overall elapses.
func waitForEnvelope(t *testing.T, ch <-chan wsproto.Envelope, overall time.Duration, pred func(wsproto.Envelope) bool) wsproto.Envelope {
	t.Helper()
	deadline := time.After(overall)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				t.Fatal("connection closed while waiting for an envelope")
			}
			if pred(env) {
				return env
			}
		case <-deadline:
			t.Fatal("timed out waiting for envelope")
		}
	}
}

// --- Requirement #22: a valid pong keeps a connection alive ---

func TestValidPongKeepsConnectionAlive(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, WithHeartbeat(fastHeartbeat())).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)
	host := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer host.CloseNow()
	ch := envelopeStream(host) // one continuous reader for the connection's whole life, as a real client keeps

	waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })

	// Outlive several heartbeat cycles with nothing but pings/pongs
	// happening at the transport level.
	time.Sleep(8 * fastHeartbeat().Interval)

	// The connection must still be genuinely alive end-to-end, not just
	// not-yet-closed: a fresh command still gets a normal response.
	writeMsg(t, host, wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "still connected"})
	env := waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeChatBroadcast })
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["text"] != "still connected" {
		t.Fatalf("chat after heartbeat cycles = %v", p)
	}
}

// --- Requirement #23: a missing heartbeat response closes the connection and flows through normal presence handling ---

func TestMissingHeartbeatResponseTriggersPresenceDisconnect(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, WithHeartbeat(fastHeartbeat())).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)
	host := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer host.CloseNow()
	ch := envelopeStream(host)
	waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })

	// Bob joins, then goes silent — never reads again, so the server's
	// pings to him never get answered. A real browser tab always answers
	// pings regardless of JS activity; this simulates the tab/process/
	// network having actually disappeared instead.
	bob := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Bob"})
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

// --- Requirement #24: heartbeat cleanup from a stale, replaced socket cannot affect the newer connection for the same seat ---

func TestStaleConnectionHeartbeatCannotAffectReplacement(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h, WithHeartbeat(fastHeartbeat())).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)
	old := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	readUntil(t, old, wsproto.TypeStateSnapshot)

	// Reconnecting replaces and closes the old connection server-side
	// (independent of heartbeat); its own heartbeat goroutine's next ping
	// will simply fail immediately against an already-closed socket.
	replacement := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer replacement.CloseNow()
	ch := envelopeStream(replacement)
	waitForEnvelope(t, ch, twoSeconds, func(e wsproto.Envelope) bool { return e.Type == wsproto.TypeStateSnapshot })

	// Outlive the old connection's heartbeat window, then confirm the
	// *replacement* was never marked disconnected by anything left over
	// from the old one.
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
