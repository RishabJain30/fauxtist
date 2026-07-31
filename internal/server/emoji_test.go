package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func TestPlayerEmojiRoundTrips(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Host"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	c := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Zoe", Emoji: "🦊"})
	defer c.Close(websocket.StatusNormalClosure, "")

	env := readUntil(t, c, wsproto.TypeRoomState)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	players, _ := p["players"].([]any)
	found := false
	for _, pl := range players {
		m, _ := pl.(map[string]any)
		if m["name"] == "Zoe" {
			if m["emoji"] != "🦊" {
				t.Fatalf("Zoe emoji = %v, want 🦊", m["emoji"])
			}
			found = true
		}
	}
	if !found {
		t.Fatalf("Zoe not found in room_state players: %v", players)
	}
}
