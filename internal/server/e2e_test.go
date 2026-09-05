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

func writeMsg(t *testing.T, c *websocket.Conn, typ string, payload any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env, _ := wsproto.Encode(typ, payload)
	env.RequestID = "test-request" // every real client command carries one; see wsproto.ValidateEnvelope
	b, _ := json.Marshal(env)
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatalf("write %s: %v", typ, err)
	}
}

// driveGameToGameOver reads the host's stream and drives one full game to the
// game-over scoreboard: it makes the current drawer stroke, ends discussion,
// votes (everyone at voteTarget), lets a caught impostor guess, and rides the
// reveal holds. It asserts every meaningful phase was observed, then returns the
// final scores. Assumes a game is starting (round 1 about to begin).
func driveGameToGameOver(t *testing.T, host *websocket.Conn, conns map[string]*websocket.Conn, voteTarget string) []any {
	t.Helper()
	readHost := func() (wsproto.Envelope, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, data, err := host.Read(ctx)
		if err != nil {
			return wsproto.Envelope{}, err
		}
		var env wsproto.Envelope
		_ = json.Unmarshal(data, &env)
		return env, nil
	}

	currentRound := 0
	guessedRound := -1
	saw := map[string]bool{}

	for i := 0; i < 2000; i++ {
		env, err := readHost()
		if err != nil {
			t.Fatalf("read host stream: %v (progress: %+v)", err, saw)
		}
		saw[env.Type] = true
		var p map[string]any
		_ = json.Unmarshal(env.Payload, &p)

		switch env.Type {
		case wsproto.TypeRoundStarted:
			currentRound = int(numField(p, "round"))
		case wsproto.TypeTurnChanged:
			cur, _ := p["currentPlayer"].(string)
			if c := conns[cur]; c != nil {
				writeMsg(t, c, wsproto.TypeStroke, wsproto.StrokePayload{
					Points: []wsproto.Point{{X: 0.4, Y: 0.4}, {X: 0.6, Y: 0.6}}, Color: "#111", Width: 3,
				})
			}
		case wsproto.TypePhaseChanged:
			switch p["phase"] {
			case "discussion":
				writeMsg(t, host, wsproto.TypeEndDiscussion, map[string]any{})
			case "voting":
				for _, c := range conns {
					writeMsg(t, c, wsproto.TypeCastVote, wsproto.VotePayload{Target: voteTarget})
				}
			}
		case wsproto.TypeRoundResult:
			caught, _ := p["caught"].(bool)
			_, hasGuess := p["impostorGuess"]
			if caught && !hasGuess && guessedRound != currentRound {
				imp, _ := p["impostorId"].(string)
				if c := conns[imp]; c != nil {
					writeMsg(t, c, wsproto.TypeImpostorGuess, wsproto.ImpostorGuessPayload{Guess: "anything"})
					guessedRound = currentRound
				}
			}
		case wsproto.TypeGameOver:
			scores, _ := p["finalScores"].([]any)
			for _, want := range []string{wsproto.TypeRoundStarted, wsproto.TypeTurnChanged, wsproto.TypeStrokeBroadcast, wsproto.TypeVoteUpdate, wsproto.TypeRoundResult} {
				if !saw[want] {
					t.Fatalf("never observed %q during the game", want)
				}
			}
			return scores
		}
	}
	t.Fatalf("game did not reach game_over within message cap (saw: %+v)", saw)
	return nil
}

// setupGame creates a room and connects the host (by token) plus three named
// players, returning the host conn, the id->conn map, and a vote target.
func setupGame(t *testing.T, srv *httptest.Server) (*websocket.Conn, map[string]*websocket.Conn, string) {
	t.Helper()
	resp, _ := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Host"}`))
	var cr createRoomResp
	_ = json.NewDecoder(resp.Body).Decode(&cr)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/room/" + cr.Code

	conns := map[string]*websocket.Conn{}
	host := dialJoin(t, wsURL, wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	conns[cr.PlayerID] = host
	_ = readUntil(t, host, wsproto.TypeStateSnapshot) // drain the host's initial snapshot

	var voteTarget string
	for _, n := range []string{"N1", "N2", "N3"} {
		c := dialJoin(t, wsURL, wsproto.JoinPayload{Name: n, Emoji: "🐙"})
		accepted := readUntil(t, c, wsproto.TypeJoinAccepted)
		var ap map[string]any
		_ = json.Unmarshal(accepted.Payload, &ap)
		pid, _ := ap["playerId"].(string)
		if pid == "" {
			t.Fatalf("join_accepted for %s carried no playerId", n)
		}
		conns[pid] = c
		if voteTarget == "" {
			voteTarget = pid
		}
		_ = readUntil(t, c, wsproto.TypeStateSnapshot) // drain each joiner's snapshot
	}
	time.Sleep(150 * time.Millisecond) // let all four register
	return host, conns, voteTarget
}

func TestFullGameReachesGameOver(t *testing.T) {
	t.Setenv("FAUXTIST_REVEAL_MS", "30")
	srv := httptest.NewServer(New(hub.New()).Handler())
	defer srv.Close()

	host, conns, voteTarget := setupGame(t, srv)
	defer func() {
		for _, c := range conns {
			c.Close(websocket.StatusNormalClosure, "")
		}
	}()

	writeMsg(t, host, wsproto.TypeStartGame, map[string]any{})
	scores := driveGameToGameOver(t, host, conns, voteTarget)
	if len(scores) != 4 {
		t.Fatalf("finalScores len = %d, want 4", len(scores))
	}
}

func TestNewGameRematch(t *testing.T) {
	t.Setenv("FAUXTIST_REVEAL_MS", "30")
	srv := httptest.NewServer(New(hub.New()).Handler())
	defer srv.Close()

	host, conns, voteTarget := setupGame(t, srv)
	defer func() {
		for _, c := range conns {
			c.Close(websocket.StatusNormalClosure, "")
		}
	}()

	// First game.
	writeMsg(t, host, wsproto.TypeStartGame, map[string]any{})
	if s := driveGameToGameOver(t, host, conns, voteTarget); len(s) != 4 {
		t.Fatalf("game 1 finalScores len = %d, want 4", len(s))
	}

	// Host starts a rematch; a whole second game must play through to game over.
	writeMsg(t, host, wsproto.TypeNewGame, map[string]any{})
	if s := driveGameToGameOver(t, host, conns, voteTarget); len(s) != 4 {
		t.Fatalf("rematch finalScores len = %d, want 4", len(s))
	}
}

func numField(m map[string]any, k string) float64 {
	if v, ok := m[k].(float64); ok {
		return v
	}
	return 0
}
