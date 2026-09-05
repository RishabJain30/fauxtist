package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func createTestRoom(t *testing.T, srv *httptest.Server, hostName string) createRoomResp {
	t.Helper()
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"`+hostName+`"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	var cr createRoomResp
	if err := json.NewDecoder(resp.Body).Decode(&cr); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if cr.Code == "" || cr.PlayerID == "" || cr.ReconnectToken == "" {
		t.Fatalf("incomplete createRoom response: %+v", cr)
	}
	return cr
}

func wsURLFor(srv *httptest.Server, code string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + code
}

// readErrorFrame reads until it sees a structured error envelope (or fails
// the test if the connection closes first without one).
func readErrorFrame(t *testing.T, c *websocket.Conn) (message, code string) {
	t.Helper()
	env := readUntil(t, c, wsproto.TypeError)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	m, _ := p["message"].(string)
	cd, _ := p["code"].(string)
	return m, cd
}

// TestPlayerIDsAreNotDerivedFromRoomCodeOrName proves requirement #1.
func TestPlayerIDsAreNotDerivedFromRoomCodeOrName(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr1 := createTestRoom(t, srv, "SameName")
	cr2 := createTestRoom(t, srv, "SameName")

	if cr1.PlayerID == cr2.PlayerID {
		t.Fatal("two different hosts with the same name got the same playerId")
	}
	for _, cr := range []createRoomResp{cr1, cr2} {
		if cr.PlayerID == cr.Code+"-host" {
			t.Fatalf("playerId %q is derived from the old code+\"-host\" scheme", cr.PlayerID)
		}
		if strings.Contains(cr.PlayerID, cr.Code) {
			t.Fatalf("playerId %q contains the room code %q", cr.PlayerID, cr.Code)
		}
		if strings.Contains(cr.PlayerID, "SameName") {
			t.Fatalf("playerId %q contains the host's name", cr.PlayerID)
		}
	}

	// A joining (non-host) player's id must not be code+"-"+name either.
	wsURL := wsURLFor(srv, cr1.Code)
	c := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Bob"})
	defer c.CloseNow()
	accepted := readUntil(t, c, wsproto.TypeJoinAccepted)
	var ap map[string]any
	_ = json.Unmarshal(accepted.Payload, &ap)
	bobID, _ := ap["playerId"].(string)
	if bobID == cr1.Code+"-Bob" {
		t.Fatalf("joiner playerId %q is derived from the old code+\"-\"+name scheme", bobID)
	}
	if strings.Contains(bobID, cr1.Code) || strings.Contains(bobID, "Bob") {
		t.Fatalf("joiner playerId %q leaks the room code or name", bobID)
	}
}

// TestReconnectCredentialsAreSufficientlyRandom proves requirement #2.
func TestReconnectCredentialsAreSufficientlyRandom(t *testing.T) {
	h := hub.New()
	s := New(h)
	// This test's 25 rapid room creations from one address exercise
	// credential randomness, not abuse resistance — the room-creation rate
	// limiter (roomcreatelimit.go) is a separate concern with its own
	// dedicated coverage (see TestCreateRoomIsRateLimitedPerIP).
	s.roomCreate = &roomCreateLimiter{
		global:     rate.NewLimiter(rate.Inf, 0),
		perIPLimit: rate.Inf,
		perIP:      map[string]*rate.Limiter{},
		lastSeen:   map[string]time.Time{},
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	seenTokens := map[string]bool{}
	seenIDs := map[string]bool{}
	for i := 0; i < 25; i++ {
		cr := createTestRoom(t, srv, "Host")
		if seenTokens[cr.ReconnectToken] {
			t.Fatalf("duplicate reconnect token across rooms: %q", cr.ReconnectToken)
		}
		seenTokens[cr.ReconnectToken] = true
		if seenIDs[cr.PlayerID] {
			t.Fatalf("duplicate playerId across rooms: %q", cr.PlayerID)
		}
		seenIDs[cr.PlayerID] = true

		b, err := base64.RawURLEncoding.DecodeString(cr.ReconnectToken)
		if err != nil {
			t.Fatalf("reconnectToken is not URL-safe unpadded base64: %v", err)
		}
		if len(b) != 32 {
			t.Fatalf("reconnectToken decodes to %d bytes, want 32 (256 bits)", len(b))
		}
	}
}

// TestGuessedHostIDDoesNotClaimHostAccess proves requirement #5.
func TestGuessedHostIDDoesNotClaimHostAccess(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	guessed := dialJoin(t, wsURL, wsproto.JoinPayload{
		PlayerID:       cr.Code + "-host", // the old, guessable scheme
		ReconnectToken: "anything",
	})
	defer guessed.CloseNow()

	_, code := readErrorFrame(t, guessed)
	if code != "invalid_reconnect" {
		t.Fatalf("error code = %q, want invalid_reconnect", code)
	}

	// The real host's credentials must still work afterward.
	real := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer real.CloseNow()
	env := readUntil(t, real, wsproto.TypeStateSnapshot)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	if p["hostId"] != cr.PlayerID {
		t.Fatalf("hostId = %v, want %s", p["hostId"], cr.PlayerID)
	}
}

// TestGuessedPlayerIDDoesNotClaimSeat proves requirement #6.
func TestGuessedPlayerIDDoesNotClaimSeat(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	bob := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Bob"})
	defer bob.CloseNow()
	_ = readUntil(t, bob, wsproto.TypeJoinAccepted)
	_ = readUntil(t, bob, wsproto.TypeStateSnapshot)

	guessed := dialJoin(t, wsURL, wsproto.JoinPayload{
		PlayerID:       cr.Code + "-Bob", // the old, guessable scheme
		ReconnectToken: "anything",
	})
	defer guessed.CloseNow()
	_, code := readErrorFrame(t, guessed)
	if code != "invalid_reconnect" {
		t.Fatalf("error code = %q, want invalid_reconnect", code)
	}

	// Bob's own connection must remain live and functional (not kicked).
	writeMsg(t, bob, wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "still me"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, _, err := bob.Read(ctx); err != nil {
		t.Fatalf("Bob's connection was disrupted by the guessed-id attempt: %v", err)
	}
}

// TestArbitraryReconnectTokenRejected proves requirement #7.
func TestArbitraryReconnectTokenRejected(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	c := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: "not-the-real-token"})
	defer c.CloseNow()
	_, code := readErrorFrame(t, c)
	if code != "invalid_reconnect" {
		t.Fatalf("error code = %q, want invalid_reconnect", code)
	}
}

// TestReconnectPreservesHostStatusScoreAndSeat proves requirements #8, #9,
// and #10: playing a full game to game_over, then reconnecting the host with
// its original credentials, returns to the same seat, keeps the score
// accumulated during the game, and keeps host privileges (it can still
// perform a host-only action).
func TestReconnectPreservesHostStatusScoreAndSeat(t *testing.T) {
	t.Setenv("FAUXTIST_REVEAL_MS", "30")
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	host := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer host.CloseNow()
	_ = readUntil(t, host, wsproto.TypeStateSnapshot)

	conns := map[string]*websocket.Conn{cr.PlayerID: host}
	var voteTarget string
	for _, n := range []string{"N1", "N2", "N3"} {
		c := dialJoin(t, wsURL, wsproto.JoinPayload{Name: n})
		defer c.CloseNow()
		accepted := readUntil(t, c, wsproto.TypeJoinAccepted)
		var ap map[string]any
		_ = json.Unmarshal(accepted.Payload, &ap)
		pid, _ := ap["playerId"].(string)
		conns[pid] = c
		if voteTarget == "" {
			voteTarget = pid
		}
		_ = readUntil(t, c, wsproto.TypeStateSnapshot)
	}
	time.Sleep(150 * time.Millisecond)

	writeMsg(t, host, wsproto.TypeStartGame, map[string]any{})
	finalScores := driveGameToGameOver(t, host, conns, voteTarget)
	scoreBefore := map[string]float64{}
	for _, s := range finalScores {
		m, _ := s.(map[string]any)
		id, _ := m["id"].(string)
		score, _ := m["score"].(float64)
		scoreBefore[id] = score
	}

	// Disconnect and reconnect the host with its original credentials.
	host.CloseNow()
	host2 := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer host2.CloseNow()
	env := readUntil(t, host2, wsproto.TypeStateSnapshot)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)

	if p["hostId"] != cr.PlayerID {
		t.Fatalf("hostId after reconnect = %v, want %s (host status not preserved)", p["hostId"], cr.PlayerID)
	}
	players, _ := p["players"].([]any)
	found := false
	for _, pl := range players {
		m, _ := pl.(map[string]any)
		if m["id"] != cr.PlayerID {
			continue
		}
		found = true
		score, _ := m["score"].(float64)
		if score != scoreBefore[cr.PlayerID] {
			t.Fatalf("host score after reconnect = %v, want %v (score not preserved)", score, scoreBefore[cr.PlayerID])
		}
	}
	if !found {
		t.Fatal("host seat missing from post-reconnect state_snapshot")
	}

	// Host privileges must still work post-reconnect.
	writeMsg(t, host2, wsproto.TypeNewGame, map[string]any{})
	_ = readUntil(t, host2, wsproto.TypeRoundStarted)
}

// TestDuplicateNamesRejectedCaseInsensitively proves requirement #12, and
// that a subsequent valid reconnect for the original player still works.
func TestDuplicateNamesRejectedCaseInsensitively(t *testing.T) {
	h := hub.New()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	alice := dialJoin(t, wsURL, wsproto.JoinPayload{Name: "Alice"})
	defer alice.CloseNow()
	accepted := readUntil(t, alice, wsproto.TypeJoinAccepted)
	var ap map[string]any
	_ = json.Unmarshal(accepted.Payload, &ap)
	aliceID, _ := ap["playerId"].(string)
	aliceToken, _ := ap["reconnectToken"].(string)
	_ = readUntil(t, alice, wsproto.TypeStateSnapshot)

	for _, variant := range []string{"alice", "ALICE", "AliCe"} {
		dupe := dialJoin(t, wsURL, wsproto.JoinPayload{Name: variant})
		_, code := readErrorFrame(t, dupe)
		if code != "name_taken" {
			t.Fatalf("variant %q: error code = %q, want name_taken", variant, code)
		}
		dupe.CloseNow()
	}

	// The original Alice can still reconnect with her real credentials.
	reconnected := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: aliceID, ReconnectToken: aliceToken})
	defer reconnected.CloseNow()
	env := readUntil(t, reconnected, wsproto.TypeStateSnapshot)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	players, _ := p["players"].([]any)
	found := false
	for _, pl := range players {
		m, _ := pl.(map[string]any)
		if m["id"] == aliceID {
			found = true
		}
	}
	if !found {
		t.Fatal("Alice's reconnect did not land on her original seat")
	}
}

// TestReconnectFrameIgnoresStrayNameField proves requirement #11 at the wire
// level: a reconnect frame that also smuggles a "name" field must not rename
// the seat.
func TestReconnectFrameIgnoresStrayNameField(t *testing.T) {
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
		"version": wsproto.ProtocolVersion,
		"type":    wsproto.TypeJoin,
		"payload": map[string]any{
			"playerId":       cr.PlayerID,
			"reconnectToken": cr.ReconnectToken,
			"name":           "Hacked",
		},
	}
	b, _ := json.Marshal(raw)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write: %v", err)
	}

	env := readUntil(t, c, wsproto.TypeStateSnapshot)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	players, _ := p["players"].([]any)
	for _, pl := range players {
		m, _ := pl.(map[string]any)
		if m["id"] == cr.PlayerID && m["name"] != "Host" {
			t.Fatalf("host name changed to %v via a smuggled reconnect field", m["name"])
		}
	}
}
