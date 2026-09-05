package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func dial(t *testing.T, wsURL, name string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	env, _ := wsproto.Encode(wsproto.TypeJoin, wsproto.JoinPayload{Name: name})
	b, _ := json.Marshal(env)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write join: %v", err)
	}
	return c
}

func readEnv(t *testing.T, c *websocket.Conn) wsproto.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return env
}

func TestJoinReceivesRoomState(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	// Create a room over HTTP.
	body := strings.NewReader(`{"name":"Alice"}`)
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	if cr.Code == "" {
		t.Fatal("no room code returned")
	}

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code
	// Joins as a distinct player, not the host's own name ("Alice" already
	// occupies the pre-seeded host seat).
	c := dial(t, wsURL, "Zoe")
	defer c.Close(websocket.StatusNormalClosure, "")

	env := readEnv(t, c)
	if env.Type != wsproto.TypeJoinAccepted {
		t.Fatalf("first message type = %q, want join_accepted", env.Type)
	}
	env = readEnv(t, c)
	if env.Type != wsproto.TypeRoomState {
		t.Fatalf("second message type = %q, want room_state", env.Type)
	}
}

func TestStrokeBroadcastsToAllClients(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	// "Amy" and "Bob" are distinct from the host's own name ("Alice").
	a := dial(t, wsURL, "Amy")
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dial(t, wsURL, "Bob")
	defer b.Close(websocket.StatusNormalClosure, "")

	// Drain each client's join_accepted + initial room_state frames.
	_ = readEnv(t, a)
	_ = readEnv(t, a)
	_ = readEnv(t, b)
	_ = readEnv(t, b)

	// A chat message from A must reach B (asserts the broadcast transport path).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	chat, _ := wsproto.Encode(wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "hi"})
	cb, _ := json.Marshal(chat)
	if err := a.Write(ctx, websocket.MessageText, cb); err != nil {
		t.Fatalf("write chat: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		env := readEnv(t, b)
		if env.Type == wsproto.TypeChatBroadcast {
			return
		}
	}
	t.Fatal("Bob never received chat_broadcast")
}

func TestHealthz(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServesIndexAtRoot(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (is internal/webui/dist populated?)", resp.StatusCode)
	}
}

func TestJoinRejectedWhenRoomFull(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Host"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	// The host pre-seat occupies one slot; fill the remaining 7 with players.
	conns := []*websocket.Conn{}
	for i := 0; i < 7; i++ {
		c := dial(t, wsURL, "P"+string(rune('a'+i)))
		conns = append(conns, c)
		_ = readEnv(t, c) // drain room_state
	}
	defer func() {
		for _, c := range conns {
			c.Close(websocket.StatusNormalClosure, "")
		}
	}()

	// The 9th participant (roster already 8) must be rejected: a structured
	// room_full error frame, then the connection is closed by the server.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	over, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer over.Close(websocket.StatusNormalClosure, "")
	join, _ := wsproto.Encode(wsproto.TypeJoin, wsproto.JoinPayload{Name: "TooMany"})
	jb, _ := json.Marshal(join)
	_ = over.Write(ctx, websocket.MessageText, jb)

	env := readEnv(t, over)
	if env.Type != wsproto.TypeError {
		t.Fatalf("type = %q, want error", env.Type)
	}
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["code"] != "room_full" {
		t.Fatalf("error code = %v, want room_full", p["code"])
	}

	closeCtx, closeCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer closeCancel()
	if _, _, err := over.Read(closeCtx); err == nil {
		t.Fatal("expected rejected join to close the connection after the error frame")
	}
}
