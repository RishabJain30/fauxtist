package room

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// dialTestConn spins up a throwaway WS upgrade endpoint and returns a
// connected client-side conn plus the server-side conn the handler accepted,
// so a test can drive Room.Join with a real *websocket.Conn without going
// through internal/server.
func dialTestConn(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// websocket.Accept hijacks the underlying net.Conn; it stays open and
		// usable after this handler returns, so there is nothing to block on.
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- c
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	select {
	case server = <-serverConnCh:
	case <-time.After(3 * time.Second):
		t.Fatal("server never accepted the test connection")
	}
	return c, server
}

// newTestRoom builds a room with a freshly minted host seat, without
// starting its Run loop (so callers may inspect unexported state race-free
// before any goroutine touches it). Uses generous (effectively "never
// fires during this test") durations; use newTestRoomWithDurations for
// tests that specifically exercise grace/skip/guess timers.
func newTestRoom(t *testing.T, hostName string) (r *Room, hostID game.PlayerID, hostToken string) {
	t.Helper()
	return newTestRoomWithDurations(t, hostName, longTestDurations())
}

func newTestRoomWithDurations(t *testing.T, hostName string, durations Durations, opts ...RoomOption) (r *Room, hostID game.PlayerID, hostToken string) {
	t.Helper()
	pid, err := identity.NewPlayerID()
	if err != nil {
		t.Fatalf("NewPlayerID: %v", err)
	}
	tok, err := identity.NewReconnectToken()
	if err != nil {
		t.Fatalf("NewReconnectToken: %v", err)
	}
	host := game.Player{ID: game.PlayerID(pid), Name: hostName}
	r = NewRoom("TEST", host, identity.Hash(tok), 1, durations, opts...)
	return r, host.ID, tok
}

// longTestDurations returns durations long enough that no timer fires
// during a normal test run, for tests that don't care about timing.
func longTestDurations() Durations {
	return Durations{
		Discussion:       time.Hour,
		Reveal:           time.Hour,
		Reconnect:        time.Hour,
		DisconnectedTurn: time.Hour,
		ImpostorGuess:    time.Hour,
	}
}

func startTestRoom(t *testing.T, r *Room) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.Run(ctx)
}

// joinAndPump joins conn to the room and starts pumping the resulting
// Client's outbound queue onto the real socket, so the test's client-side
// Read calls actually observe what the room sends.
func joinAndPump(t *testing.T, r *Room, conn *websocket.Conn, req JoinRequest) JoinResult {
	t.Helper()
	res, err := r.Join(conn, req)
	if err != nil {
		t.Fatalf("join: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go res.Client.WriteLoopForServer(ctx)
	return res
}

func readEnvelope(t *testing.T, c *websocket.Conn) wsproto.Envelope {
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

// TestReconnectTokenNeverStoredRawOrLeaked proves requirements #3 and #4: the
// room only ever holds a sha256 hash of a seat's reconnect token (never the
// raw value), and the raw token never appears in a state_snapshot snapshot.
func TestReconnectTokenNeverStoredRawOrLeaked(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")

	// Race-free: Run() has not started, so nothing else touches r.seats yet.
	seat, ok := r.seats[hostID]
	if !ok {
		t.Fatal("expected a seat credential for the pre-seeded host")
	}
	if seat.tokenHash != identity.Hash(hostToken) {
		t.Fatal("stored hash does not match sha256(rawToken)")
	}
	if string(seat.tokenHash[:]) == hostToken {
		t.Fatal("stored credential equals the raw token")
	}

	startTestRoom(t, r)
	client, conn := dialTestConn(t)
	defer client.CloseNow()
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if strings.Contains(string(data), hostToken) {
		t.Fatalf("state_snapshot snapshot leaked the raw reconnect token: %s", data)
	}
}

// TestReplacedConnectionIsClosedAndCannotAct proves requirements #13 and #14:
// reconnecting closes the superseded connection, and a message tagged with
// the old, now-stale connID is dropped rather than acted on or broadcast.
func TestReplacedConnectionIsClosedAndCannotAct(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)

	client1, conn1 := dialTestConn(t)
	defer client1.CloseNow()
	res1 := joinAndPump(t, r, conn1, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	_ = readEnvelope(t, client1) // drain state_snapshot
	_ = readEnvelope(t, client1) // drain lobby_update (broadcastLobby reaches the joiner itself)

	client2, conn2 := dialTestConn(t)
	defer client2.CloseNow()
	res2 := joinAndPump(t, r, conn2, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	_ = readEnvelope(t, client2) // drain state_snapshot
	_ = readEnvelope(t, client2) // drain lobby_update

	if res2.PlayerID != res1.PlayerID {
		t.Fatalf("expected the same seat, got %q then %q", res1.PlayerID, res2.PlayerID)
	}
	if res2.ConnID == res1.ConnID {
		t.Fatal("expected the replacing connection to get a new connID")
	}

	// The superseded connection must be actively closed by the server side.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer closeCancel()
	if _, _, err := client1.Read(closeCtx); err == nil {
		t.Fatal("expected the replaced connection to be closed")
	}

	// A message tagged with the OLD connID must be dropped: it must not
	// reach the engine or be broadcast to the live connection.
	stale, _ := wsproto.Encode(wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "from-stale-conn"})
	r.Submit(hostID, res1.ConnID, stale)

	live, _ := wsproto.Encode(wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "from-live-conn"})
	r.Submit(hostID, res2.ConnID, live)

	seenStale, seenLive := false, false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !seenLive {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		_, data, err := client2.Read(ctx)
		cancel()
		if err != nil {
			break
		}
		if strings.Contains(string(data), "from-stale-conn") {
			seenStale = true
		}
		if strings.Contains(string(data), "from-live-conn") {
			seenLive = true
		}
	}
	if seenStale {
		t.Fatal("the stale connection's message was broadcast — the connID guard did not drop it")
	}
	if !seenLive {
		t.Fatal("the live connection's message was never broadcast")
	}
}

// TestReconnectIgnoresStrayNameField proves requirement #11: the reconnect
// path has no Name field to act on at all, so even a request that also sets
// one (simulating a malicious client) cannot rename the seat.
func TestReconnectIgnoresStrayNameField(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)

	client, conn := dialTestConn(t)
	defer client.CloseNow()
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken, Name: "Hacked"})

	env := readEnvelope(t, client)
	if env.Type != wsproto.TypeStateSnapshot {
		t.Fatalf("type = %q, want state_snapshot", env.Type)
	}
	var snap struct {
		Players []game.Player `json:"players"`
	}
	if err := json.Unmarshal(env.Payload, &snap); err != nil {
		t.Fatalf("unmarshal snapshot: %v", err)
	}
	for _, p := range snap.Players {
		if p.ID == hostID && p.Name != "Host" {
			t.Fatalf("host name changed via reconnect: %q", p.Name)
		}
	}
}
