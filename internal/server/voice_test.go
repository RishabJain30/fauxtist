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

func readUntil(t *testing.T, c *websocket.Conn, typ string) wsproto.Envelope {
	t.Helper()
	for i := 0; i < 50; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, data, err := c.Read(ctx)
		cancel()
		if err != nil {
			t.Fatalf("read waiting for %s: %v", typ, err)
		}
		var env wsproto.Envelope
		_ = json.Unmarshal(data, &env)
		if env.Type == typ {
			return env
		}
	}
	t.Fatalf("never saw %s", typ)
	return wsproto.Envelope{}
}

func TestVoiceSignalRelayedToTarget(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Host"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	a := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer a.Close(websocket.StatusNormalClosure, "")
	_ = readUntil(t, a, wsproto.TypeRoomState)
	aID := cr.PlayerID

	b := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "B"})
	defer b.Close(websocket.StatusNormalClosure, "")
	accepted := readUntil(t, b, wsproto.TypeJoinAccepted)
	var ap map[string]any
	_ = json.Unmarshal(accepted.Payload, &ap)
	bID, _ := ap["playerId"].(string)
	_ = readUntil(t, b, wsproto.TypeRoomState)

	time.Sleep(100 * time.Millisecond)

	// A enables voice -> B must observe A joining voice.
	writeMsg(t, a, wsproto.TypeVoiceJoin, map[string]any{})
	pj := readUntil(t, b, wsproto.TypeVoicePeerJoined)
	var pjp map[string]any
	_ = json.Unmarshal(pj.Payload, &pjp)
	if pjp["id"] != aID {
		t.Fatalf("peer_joined id = %v, want %s", pjp["id"], aID)
	}

	// B enables voice -> B's voice_peers must list A.
	writeMsg(t, b, wsproto.TypeVoiceJoin, map[string]any{})
	peers := readUntil(t, b, wsproto.TypeVoicePeers)
	var pp map[string]any
	_ = json.Unmarshal(peers.Payload, &pp)
	ids, _ := pp["ids"].([]any)
	if len(ids) != 1 || ids[0] != aID {
		t.Fatalf("voice_peers = %v, want [%s]", ids, aID)
	}

	// A sends an offer addressed to B -> only B receives it, with from=A.
	writeMsg(t, a, wsproto.TypeVoiceSignal, map[string]any{
		"to": bID, "kind": "offer", "payload": map[string]any{"sdp": "x"},
	})
	sig := readUntil(t, b, wsproto.TypeVoiceSignal)
	var sp map[string]any
	_ = json.Unmarshal(sig.Payload, &sp)
	if sp["from"] != aID || sp["kind"] != "offer" {
		t.Fatalf("signal = %v, want from=%s kind=offer", sp, aID)
	}
}
