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
	c := dial(t, wsURL, "Alice")
	defer c.Close(websocket.StatusNormalClosure, "")

	env := readEnv(t, c)
	if env.Type != wsproto.TypeRoomState {
		t.Fatalf("first message type = %q, want room_state", env.Type)
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

	a := dial(t, wsURL, "Alice")
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dial(t, wsURL, "Bob")
	defer b.Close(websocket.StatusNormalClosure, "")

	// Drain each client's initial room_state frame.
	_ = readEnv(t, a)
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

	// The 9th participant (roster already 8) must be rejected: the connection
	// is closed by the server, so a Read returns an error.
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
	if _, _, err := over.Read(ctx); err == nil {
		t.Fatal("expected rejected join to close the connection")
	}
}
