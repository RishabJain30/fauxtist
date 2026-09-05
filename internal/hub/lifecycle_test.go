package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/room"
)

// testClock is a mutable clock for deterministic sweeper tests — advanced
// explicitly rather than waiting on real time.
type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time          { return c.now }
func (c *testClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func newTestClock() *testClock { return &testClock{now: time.Unix(0, 0)} }

// newTestHub builds a Hub whose background sweeper never fires on its own
// (a long interval) so tests can call Sweep directly and assert on its
// effect deterministically, without racing a real ticker.
func newTestHub(clock func() time.Time, cfg Config) *Hub {
	cfg.SweepInterval = time.Hour
	return New(WithClock(clock), WithConfig(cfg))
}

// --- Requirement #1/#2: empty inactive rooms expire via the sweeper; connected rooms never do ---

func TestSweepRemovesEmptyIdleRoom(t *testing.T) {
	clock := newTestClock()
	h := newTestHub(clock.Now, Config{EmptyRoomTTL: 5 * time.Minute, MaxRooms: 10})
	defer h.Close()

	code, _, _, err := h.CreateRoom("Host")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}

	h.Sweep()
	if _, ok := h.Get(code); !ok {
		t.Fatal("a fresh room must not be swept before its TTL elapses")
	}

	clock.Advance(6 * time.Minute)
	h.Sweep()
	if _, ok := h.Get(code); ok {
		t.Fatal("expected the idle room to be removed by Sweep")
	}
}

func TestSweepNeverRemovesAnActivelyConnectedRoom(t *testing.T) {
	clock := newTestClock()
	h := newTestHub(clock.Now, Config{EmptyRoomTTL: time.Minute, MaxRooms: 10})
	defer h.Close()

	code, hostID, hostToken, err := h.CreateRoom("Host")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	r, ok := h.Get(code)
	if !ok {
		t.Fatal("room missing right after creation")
	}
	client, conn := dialPair(t)
	defer client.CloseNow()
	if _, err := r.Join(conn, room.JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken}); err != nil {
		t.Fatalf("join: %v", err)
	}

	clock.Advance(time.Hour)
	h.Sweep()
	if _, ok := h.Get(code); !ok {
		t.Fatal("a room with a connected client must never be swept")
	}
}

// --- Requirement #6: maximum room capacity is enforced ---

func TestMaxRoomsCapacityEnforced(t *testing.T) {
	h := New(WithConfig(Config{EmptyRoomTTL: time.Hour, SweepInterval: time.Hour, MaxRooms: 2}))
	defer h.Close()

	if _, _, _, err := h.CreateRoom("A"); err != nil {
		t.Fatalf("CreateRoom 1: %v", err)
	}
	if _, _, _, err := h.CreateRoom("B"); err != nil {
		t.Fatalf("CreateRoom 2: %v", err)
	}
	if _, _, _, err := h.CreateRoom("C"); err != ErrHubAtCapacity {
		t.Fatalf("CreateRoom 3 err = %v, want ErrHubAtCapacity", err)
	}
}

// --- Requirement #5: Hub.Close stops every room safely and is idempotent ---

func TestCloseStopsSweeperAndEveryRoomAndIsIdempotent(t *testing.T) {
	h := New(WithConfig(Config{EmptyRoomTTL: time.Hour, SweepInterval: time.Hour, MaxRooms: 10}))
	code, _, _, err := h.CreateRoom("Host")
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	r, _ := h.Get(code)

	h.Close()
	h.Close() // must not panic or block

	select {
	case <-r.Stopped():
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not stop the room's actor")
	}
	if _, ok := h.Get(code); ok {
		t.Fatal("expected Close to remove every room from the hub's map")
	}
}

// dialPair spins up a throwaway WS upgrade endpoint and returns a connected
// client-side conn plus the server-side conn the handler accepted, so a
// test can drive room.Room.Join with a real *websocket.Conn without going
// through internal/server.
func dialPair(t *testing.T) (client, server *websocket.Conn) {
	t.Helper()
	serverConnCh := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
