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

// --- Requirement #9: unsupported protocol versions are rejected with the documented close code ---

func TestUnsupportedProtocolVersionRejected(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	raw := map[string]any{
		"version": 999,
		"type":    wsproto.TypeJoin,
		"payload": map[string]any{"playerId": cr.PlayerID, "reconnectToken": cr.ReconnectToken},
	}
	b, _ := json.Marshal(raw)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}

	msg, code := readErrorFrame(t, c)
	if code != "unsupported_version" {
		t.Fatalf("error code = %q, want unsupported_version", code)
	}
	if msg == "" {
		t.Fatal("error message empty")
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil {
		t.Fatal("expected the connection to close after the version rejection")
	} else if got := websocket.CloseStatus(err); got != websocket.StatusCode(wsproto.CloseUnsupportedVersion) {
		t.Fatalf("close code = %d, want %d", got, wsproto.CloseUnsupportedVersion)
	}
}

// --- Requirement #10: malformed envelopes are rejected safely, never panic ---

func TestMalformedJoinFrameRejectedWithoutPanicking(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	cases := []struct {
		name string
		body []byte
	}{
		{"not json at all", []byte("this is not json")},
		{"valid json, wrong shape", []byte(`{"foo":"bar"}`)},
		{"valid envelope, wrong type", []byte(`{"version":1,"type":"stroke","payload":{}}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, wsURL, nil)
			if err != nil {
				t.Fatalf("dial: %v", err)
			}
			defer c.CloseNow()
			if err := c.Write(ctx, websocket.MessageText, tc.body); err != nil {
				t.Fatalf("write: %v", err)
			}
			_, code := readErrorFrame(t, c)
			if code != "invalid_envelope" {
				t.Fatalf("error code = %q, want invalid_envelope", code)
			}
			readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer readCancel()
			if _, _, err := c.Read(readCtx); err == nil {
				t.Fatal("expected the connection to close after a malformed first frame")
			} else if got := websocket.CloseStatus(err); got != websocket.StatusCode(wsproto.CloseInvalidEnvelope) {
				t.Fatalf("close code = %d, want %d", got, wsproto.CloseInvalidEnvelope)
			}
		})
	}

	// The server process itself must still be healthy: a completely fresh,
	// well-formed join on a new connection must still succeed.
	fresh := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "AfterGarbage"})
	defer fresh.CloseNow()
	env := readUntil(t, fresh, wsproto.TypeJoinAccepted)
	if env.Type != wsproto.TypeJoinAccepted {
		t.Fatalf("server did not accept a normal join after malformed frames from other connections")
	}
}

// A malformed or version-mismatched frame arriving mid-session (after a
// real join has already succeeded) must be dropped, not treated as fatal —
// closing an otherwise-healthy connection over one bad message would be
// far more disruptive than silently ignoring it.
func TestMalformedFrameMidSessionDoesNotDisconnect(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)
	c := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer c.CloseNow()
	readUntil(t, c, wsproto.TypeStateSnapshot)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.Write(ctx, websocket.MessageText, []byte("not json"))
	_ = c.Write(ctx, websocket.MessageText, []byte(`{"version":999,"type":"chat_message","payload":{"text":"hi"}}`))

	// The connection must still be alive and processing normal commands.
	writeMsg(t, c, wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "still here"})
	env := readUntil(t, c, wsproto.TypeChatBroadcast)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["text"] != "still here" {
		t.Fatalf("chat after garbage frames = %v, want the message to have gone through", p)
	}
}
