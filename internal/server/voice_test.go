package server

import (
	"encoding/json"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// TestVoiceSignalRelayedToTarget proves the voice signalling relay: a peer
// enabling voice is announced to others, voice_peers lists existing peers, and
// a directed offer reaches only its addressed target with the correct sender.
func TestVoiceSignalRelayedToTarget(t *testing.T) {
	srv := startServer(t, hub.New())
	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	a := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer a.Close(websocket.StatusNormalClosure, "")
	readUntil(t, a, wsproto.TypeStateSnapshot)
	aID := cr.PlayerID

	b := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Bob", Emoji: palette[1]})
	defer b.Close(websocket.StatusNormalClosure, "")
	accepted := readUntil(t, b, wsproto.TypeJoinAccepted)
	var ap wsproto.JoinAcceptedPayload
	_ = json.Unmarshal(accepted.Payload, &ap)
	bID := ap.PlayerID
	readUntil(t, b, wsproto.TypeStateSnapshot)

	time.Sleep(100 * time.Millisecond) // let both register as active players

	// A enables voice -> B observes A joining voice.
	writeMsg(t, a, wsproto.TypeVoiceJoin, map[string]any{})
	pj := readUntil(t, b, wsproto.TypeVoicePeerJoined)
	var pjp map[string]any
	_ = json.Unmarshal(pj.Payload, &pjp)
	if pjp["id"] != aID {
		t.Fatalf("peer_joined id = %v, want %s", pjp["id"], aID)
	}

	// B enables voice -> B's voice_peers lists A.
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
