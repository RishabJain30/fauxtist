package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// TestCreateRoomAndHostEmojiRoundTrips proves the host's chosen emoji is
// carried onto their seat (an earlier version dropped it): create with 🐙,
// reconnect as the host, and confirm the host row in the snapshot shows 🐙.
func TestCreateRoomAndHostEmojiRoundTrips(t *testing.T) {
	srv := startServer(t, hub.New())
	cr := createTestRoomEmoji(t, srv, "Alice", "🐙")

	c := dialJoin(t, wsURLFor(srv, cr.Code), wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer c.Close(websocket.StatusNormalClosure, "")

	env := readUntil(t, c, wsproto.TypeStateSnapshot)
	var p struct {
		Players []struct {
			ID    string `json:"id"`
			Emoji string `json:"emoji"`
		} `json:"players"`
	}
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	found := false
	for _, pl := range p.Players {
		if pl.ID == cr.PlayerID {
			found = true
			if pl.Emoji != "🐙" {
				t.Fatalf("host emoji = %q, want 🐙 (host-emoji-drop regression)", pl.Emoji)
			}
		}
	}
	if !found {
		t.Fatalf("host %s not found in snapshot players", cr.PlayerID)
	}
}

func TestCreateRoomRejectsInvalidContentType(t *testing.T) {
	srv := startServer(t, hub.New())

	cases := []struct {
		name        string
		contentType string
	}{
		{"missing content-type", ""},
		{"wrong content-type", "text/plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/rooms", strings.NewReader(`{"name":"Alice"}`))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusUnsupportedMediaType {
				t.Fatalf("status = %d, want 415", resp.StatusCode)
			}
			var body apiError
			_ = json.NewDecoder(resp.Body).Decode(&body)
			if body.Code != "invalid_content_type" {
				t.Fatalf("code = %q, want invalid_content_type", body.Code)
			}
		})
	}
}

func TestCreateRoomRejectsInvalidName(t *testing.T) {
	srv := startServer(t, hub.New())
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"   "}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body apiError
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Code != "invalid_name" {
		t.Fatalf("code = %q, want invalid_name", body.Code)
	}
}

func TestCreateRoomRejectsInvalidEmoji(t *testing.T) {
	srv := startServer(t, hub.New())
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice","emoji":"notanemoji"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	var body apiError
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Code != "invalid_emoji" {
		t.Fatalf("code = %q, want invalid_emoji", body.Code)
	}
}

// TestUnsupportedProtocolVersionRejectedAtJoin sends a v1 join and expects a
// structured unsupported_version error, then a close with code 4001.
func TestUnsupportedProtocolVersionRejectedAtJoin(t *testing.T) {
	srv := startServer(t, hub.New())
	cr := createTestRoom(t, srv, "Host")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURLFor(srv, cr.Code), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	raw := map[string]any{
		"version": 1,
		"type":    wsproto.TypeJoin,
		"payload": map[string]any{"name": "Bob", "emoji": "🐙"},
	}
	b, _ := json.Marshal(raw)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, code := readErrorFrame(t, c)
	if code != "unsupported_version" {
		t.Fatalf("error code = %q, want unsupported_version", code)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil {
		t.Fatal("expected the connection to close after the version rejection")
	} else if got := websocket.CloseStatus(err); got != websocket.StatusCode(wsproto.CloseUnsupportedVersion) {
		t.Fatalf("close code = %d, want %d", got, wsproto.CloseUnsupportedVersion)
	}
}

// TestNewPlayerJoinAcceptedThenSnapshot proves a fresh join to a lobby gets a
// join_accepted (with credentials) then a state_snapshot with role "player".
func TestNewPlayerJoinAcceptedThenSnapshot(t *testing.T) {
	srv := startServer(t, hub.New())
	cr := createTestRoom(t, srv, "Alice")

	c := dialJoin(t, wsURLFor(srv, cr.Code), wsproto.JoinPayload{Name: "Bob", Emoji: "🐙"})
	defer c.Close(websocket.StatusNormalClosure, "")

	accepted := readUntil(t, c, wsproto.TypeJoinAccepted)
	var ap wsproto.JoinAcceptedPayload
	if err := json.Unmarshal(accepted.Payload, &ap); err != nil {
		t.Fatalf("unmarshal join_accepted: %v", err)
	}
	if ap.PlayerID == "" || ap.ReconnectToken == "" {
		t.Fatalf("join_accepted missing credentials: %+v", ap)
	}
	if ap.Spectator {
		t.Fatal("a lobby joiner must be a player, not a spectator")
	}

	snap := readUntil(t, c, wsproto.TypeStateSnapshot)
	var sp struct {
		Role string `json:"role"`
	}
	_ = json.Unmarshal(snap.Payload, &sp)
	if sp.Role != "player" {
		t.Fatalf("snapshot role = %q, want player", sp.Role)
	}
}
