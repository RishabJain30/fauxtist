package server

// This file holds the shared test harness for the server package's
// integration tests: how a room is created over HTTP, how a WebSocket is
// dialed and the join frame sent, and small utilities for reading/collecting
// server frames. The individual _test.go files reuse these; nothing here is
// production code. The patterns (httptest server, dialJoin, readUntil,
// createTestRoom, wsURLFor, envelopeStream/waitForEnvelope) mirror the ones
// the previous drawing-era tests used.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/room"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// palette mirrors the server's valid emoji set; tests hand each player a
// distinct one so no join is ever rejected for an unsupported avatar.
var palette = []string{"🦊", "🐙", "🐸", "🦉", "🐨", "🦁", "🐵", "🦄", "🐼", "🐧", "🦔", "🐝"}

// ---- HTTP / WS plumbing ----

func startServer(t *testing.T, h *hub.Hub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(h).Handler())
	// Cleanups run LIFO: register the hub first so the server closes before
	// the hub does.
	t.Cleanup(h.Close)
	t.Cleanup(srv.Close)
	return srv
}

// newFastHub builds a hub whose rooms run every timed phase at the given
// fixed duration, so a full match plays itself out in milliseconds.
func newFastHub(perPhase time.Duration) *hub.Hub {
	return hub.New(hub.WithRoomOptions(room.WithPhaseDuration(func(game.Phase) time.Duration {
		return perPhase
	})))
}

// newPhasedHub builds a hub whose rooms use a caller-supplied per-phase
// duration function — used when a test needs a couple of phases to linger
// long enough to act within while the rest race by.
func newPhasedHub(fn func(game.Phase) time.Duration) *hub.Hub {
	return hub.New(hub.WithRoomOptions(room.WithPhaseDuration(fn)))
}

func createTestRoom(t *testing.T, srv *httptest.Server, hostName string) createRoomResp {
	t.Helper()
	return createTestRoomEmoji(t, srv, hostName, "")
}

func createTestRoomEmoji(t *testing.T, srv *httptest.Server, hostName, emoji string) createRoomResp {
	t.Helper()
	body := `{"name":"` + hostName + `"`
	if emoji != "" {
		body += `,"emoji":"` + emoji + `"`
	}
	body += `}`
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("create room status = %d, want 200", resp.StatusCode)
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

// dialJoin dials the room and sends a join frame with the given payload.
func dialJoin(t *testing.T, wsURL string, payload wsproto.JoinPayload) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	env, _ := wsproto.Encode(wsproto.TypeJoin, payload)
	b, _ := json.Marshal(env)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write join: %v", err)
	}
	return c
}

// writeMsg sends a client command envelope, stamped with the non-empty
// requestId every real client command carries (see wsproto.ValidateEnvelope).
func writeMsg(t *testing.T, c *websocket.Conn, typ string, payload any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env, _ := wsproto.Encode(typ, payload)
	env.RequestID = "req-" + typ
	b, _ := json.Marshal(env)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write %s: %v", typ, err)
	}
}

// tryWrite sends a command envelope, ignoring any error. Safe to call from a
// background goroutine (unlike writeMsg, it never touches *testing.T).
func tryWrite(c *websocket.Conn, typ string, payload any) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	env, _ := wsproto.Encode(typ, payload)
	env.RequestID = "req-" + typ
	b, _ := json.Marshal(env)
	_ = c.Write(ctx, websocket.MessageText, b)
}

// readUntil reads envelopes until it sees one of type typ (or fails).
func readUntil(t *testing.T, c *websocket.Conn, typ string) wsproto.Envelope {
	t.Helper()
	for i := 0; i < 100; i++ {
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

// readErrorFrame reads until it sees a structured error envelope.
func readErrorFrame(t *testing.T, c *websocket.Conn) (message, code string) {
	t.Helper()
	env := readUntil(t, c, wsproto.TypeError)
	var p map[string]any
	_ = json.Unmarshal(env.Payload, &p)
	m, _ := p["message"].(string)
	cd, _ := p["code"].(string)
	return m, cd
}

// envelopeStream continuously reads c in the background and delivers decoded
// envelopes to the returned channel (closed once the connection ends). A
// nhooyr Conn wants exactly one blocking Read outstanding for its whole life;
// tests idle by waiting on the channel, never by putting a deadline on Read.
func envelopeStream(c *websocket.Conn) <-chan wsproto.Envelope {
	ch := make(chan wsproto.Envelope, 128)
	go func() {
		defer close(ch)
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var env wsproto.Envelope
			if json.Unmarshal(data, &env) == nil {
				ch <- env
			}
		}
	}()
	return ch
}

func waitForEnvelope(t *testing.T, ch <-chan wsproto.Envelope, overall time.Duration, pred func(wsproto.Envelope) bool) wsproto.Envelope {
	t.Helper()
	deadline := time.After(overall)
	for {
		select {
		case env, ok := <-ch:
			if !ok {
				t.Fatal("connection closed while waiting for an envelope")
			}
			if pred(env) {
				return env
			}
		case <-deadline:
			t.Fatal("timed out waiting for envelope")
		}
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// ---- Frame recorder ----
//
// A frameRecorder keeps one continuous Read outstanding on a connection (as a
// real client must) and captures every frame it receives, raw bytes and
// decoded envelope both, so a test can assert over the whole history a
// connection observed — including proving a secret never appeared.

type recordedFrame struct {
	env wsproto.Envelope
	raw []byte
}

type frameRecorder struct {
	mu     sync.Mutex
	frames []recordedFrame
	done   chan struct{}
}

func recordFrames(c *websocket.Conn) *frameRecorder {
	fr := &frameRecorder{done: make(chan struct{})}
	go func() {
		defer close(fr.done)
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			var env wsproto.Envelope
			if json.Unmarshal(data, &env) != nil {
				continue
			}
			fr.mu.Lock()
			fr.frames = append(fr.frames, recordedFrame{env: env, raw: append([]byte(nil), data...)})
			fr.mu.Unlock()
		}
	}()
	return fr
}

func (fr *frameRecorder) snapshot() []recordedFrame {
	fr.mu.Lock()
	defer fr.mu.Unlock()
	return append([]recordedFrame(nil), fr.frames...)
}

// awaitType polls until a frame of type typ has been recorded, returning it.
func (fr *frameRecorder) awaitType(t *testing.T, typ string, within time.Duration) recordedFrame {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		for _, f := range fr.snapshot() {
			if f.env.Type == typ {
				return f
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never recorded a %s frame within %v", typ, within)
	return recordedFrame{}
}

// hasType reports whether any recorded frame is of type typ.
func (fr *frameRecorder) hasType(typ string) bool {
	for _, f := range fr.snapshot() {
		if f.env.Type == typ {
			return true
		}
	}
	return false
}

// awaitPhase polls until a recorded frame reports the given phase (either a
// phase_changed or a state_snapshot carries a "phase" field).
func (fr *frameRecorder) awaitPhase(t *testing.T, phase string, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if fr.sawPhase(phase) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("never reached phase %q within %v", phase, within)
}

func (fr *frameRecorder) sawPhase(phase string) bool {
	for _, f := range fr.snapshot() {
		if payloadPhase(f.env) == phase {
			return true
		}
	}
	return false
}

func payloadPhase(env wsproto.Envelope) string {
	if env.Type != wsproto.TypePhaseChanged && env.Type != wsproto.TypeStateSnapshot {
		return ""
	}
	var p struct {
		Phase string `json:"phase"`
	}
	_ = json.Unmarshal(env.Payload, &p)
	return p.Phase
}

// ---- Match setup helpers ----

// joinPlayerRec dials a fresh named player, attaches a recorder, and returns
// the connection, its recorder, and the server-minted playerId (read from the
// join_accepted frame the recorder captures).
func joinPlayerRec(t *testing.T, wsURL, name, emoji string) (*websocket.Conn, *frameRecorder, string) {
	t.Helper()
	c := dialJoin(t, wsURL, wsproto.JoinPayload{Name: name, Emoji: emoji})
	fr := recordFrames(c)
	f := fr.awaitType(t, wsproto.TypeJoinAccepted, 3*time.Second)
	var ap wsproto.JoinAcceptedPayload
	_ = json.Unmarshal(f.env.Payload, &ap)
	if ap.PlayerID == "" {
		t.Fatalf("join_accepted for %s carried no playerId", name)
	}
	return c, fr, ap.PlayerID
}

// reconnectTokenFrom extracts a player's raw reconnect token from the
// join_accepted frame their recorder captured.
func reconnectTokenFrom(t *testing.T, fr *frameRecorder) string {
	t.Helper()
	f := fr.awaitType(t, wsproto.TypeJoinAccepted, 3*time.Second)
	var ap wsproto.JoinAcceptedPayload
	_ = json.Unmarshal(f.env.Payload, &ap)
	if ap.ReconnectToken == "" {
		t.Fatal("join_accepted carried no reconnect token")
	}
	return ap.ReconnectToken
}

// connectHostRec dials the host's seat by reconnect credentials and attaches
// a recorder (a reconnect gets no join_accepted, only a state_snapshot).
func connectHostRec(t *testing.T, wsURL string, cr createRoomResp) (*websocket.Conn, *frameRecorder) {
	t.Helper()
	c := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	fr := recordFrames(c)
	fr.awaitType(t, wsproto.TypeStateSnapshot, 3*time.Second)
	return c, fr
}

// readyAndStart sets every connection ready, waits for the readiness to
// register, then has the host start the match, retrying the start until the
// hostRecorder observes the first in-match phase (INCOME).
func readyAndStart(t *testing.T, host *websocket.Conn, all []*websocket.Conn, hostRec *frameRecorder) {
	t.Helper()
	for _, c := range all {
		writeMsg(t, c, wsproto.TypeSetReady, wsproto.SetReadyPayload{Ready: true})
	}
	time.Sleep(200 * time.Millisecond)
	for i := 0; i < 10; i++ {
		writeMsg(t, host, wsproto.TypeStartMatch, map[string]any{})
		deadline := time.Now().Add(400 * time.Millisecond)
		for time.Now().Before(deadline) {
			if hostRec.sawPhase(string(game.PhaseIncome)) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	t.Fatal("match never entered INCOME after start_match")
}

// ---- Board / command inspection ----

type boardTile struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Owner string `json:"owner"`
}

// ownedTiles finds owner's capital and a normal (starting adjacent) tile id
// from the first recorded state_snapshot that carries a populated board.
func ownedTiles(t *testing.T, frames []recordedFrame, owner string) (capital, normal string) {
	t.Helper()
	for _, f := range frames {
		if f.env.Type != wsproto.TypeStateSnapshot {
			continue
		}
		var p struct {
			Board []boardTile `json:"board"`
		}
		if json.Unmarshal(f.env.Payload, &p) != nil || len(p.Board) == 0 {
			continue
		}
		for _, tile := range p.Board {
			if tile.Owner != owner {
				continue
			}
			switch tile.Type {
			case "capital":
				capital = tile.ID
			case "normal":
				if normal == "" {
					normal = tile.ID
				}
			}
		}
		if capital != "" && normal != "" {
			return capital, normal
		}
	}
	t.Fatalf("could not find owned capital+normal tiles for %s", owner)
	return "", ""
}

var commandTypeSet = map[string]bool{
	"march": true, "fortify": true, "recruit": true,
	"build_fortress": true, "build_mine": true, "hold": true,
}

// containsCommandTo reports whether any command-shaped JSON object anywhere in
// v is of type cmdType targeting toTile. A board tile also has a "type" field
// but its value is never a command type, and it carries no "to", so this never
// matches public board data — only an actual leaked (or legitimately present)
// command object.
func containsCommandTo(v any, cmdType, toTile string) bool {
	switch x := v.(type) {
	case map[string]any:
		if ts, ok := x["type"].(string); ok && commandTypeSet[ts] && ts == cmdType {
			if to, ok := x["to"].(string); ok && to == toTile {
				return true
			}
		}
		for _, val := range x {
			if containsCommandTo(val, cmdType, toTile) {
				return true
			}
		}
	case []any:
		for _, val := range x {
			if containsCommandTo(val, cmdType, toTile) {
				return true
			}
		}
	}
	return false
}
