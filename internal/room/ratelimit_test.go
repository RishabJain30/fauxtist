package room

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// --- Requirement #13/#14: floods are throttled, and the throttled attempts never mutate state ---

func TestFloodedCommandsGetRateLimitedWithoutMutatingState(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	client, conn := dialTestConn(t)
	defer client.CloseNow()
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client)

	before := r.Snapshot().Revision

	// end_discussion is a no-op in the lobby phase either way (ErrWrongPhase),
	// so any revision change here can only be explained by a rate-limited
	// send somehow still reaching the engine, which must never happen.
	env, _ := wsproto.Encode(wsproto.TypeEndDiscussion, map[string]any{})
	for i := 0; i < 60; i++ {
		r.Submit(hostID, res.ConnID, env)
	}

	sawRateLimited := false
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !sawRateLimited {
		e := readEnvelopeWithTimeout(client, 200*time.Millisecond)
		if e == nil {
			continue
		}
		if e.Type == wsproto.TypeError {
			var p map[string]any
			_ = json.Unmarshal(e.Payload, &p)
			if p["code"] == "rate_limited" {
				sawRateLimited = true
			}
		}
	}
	if !sawRateLimited {
		t.Fatal("expected at least one rate_limited error from a 60-message instant flood")
	}
	if got := r.Snapshot().Revision; got != before {
		t.Fatalf("revision changed %d -> %d; a rejected/rate-limited command must never bump it", before, got)
	}
}

// TestNormalPacedCommandsAreNeverRateLimited proves the limiter's burst is
// generous enough for real gameplay: a handful of spaced-out commands (well
// below the burst capacity) must always go through.
func TestNormalPacedCommandsAreNeverRateLimited(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	client, conn := dialTestConn(t)
	defer client.CloseNow()
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client)

	chat, _ := wsproto.Encode(wsproto.TypeChatMessage, wsproto.ChatPayload{Text: "hello"})
	for i := 0; i < 3; i++ {
		r.Submit(hostID, res.ConnID, chat)
	}

	seen := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && seen < 3 {
		e := readEnvelopeWithTimeout(client, 200*time.Millisecond)
		if e == nil {
			continue
		}
		if e.Type == wsproto.TypeError {
			t.Fatalf("unexpected error for a normal, well-spaced chat send: %s", e.Payload)
		}
		if e.Type == wsproto.TypeChatBroadcast {
			seen++
		}
	}
	if seen != 3 {
		t.Fatalf("saw %d of 3 expected chat broadcasts", seen)
	}
}

// --- Requirement: repeated severe abuse closes the connection ---

func TestSevereRateLimitAbuseClosesConnection(t *testing.T) {
	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)
	client, conn := dialTestConn(t)
	defer client.CloseNow()
	res := joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client)

	env, _ := wsproto.Encode(wsproto.TypeEndDiscussion, map[string]any{})
	for i := 0; i < 200; i++ {
		r.Submit(hostID, res.ConnID, env)
	}

	closed := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		readCtx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		_, _, err := client.Read(readCtx)
		cancel()
		if err == nil {
			continue // drain a rate_limited error frame and keep watching for the close
		}
		if errors.Is(err, context.DeadlineExceeded) {
			continue // no frame within this slice; not itself a close
		}
		closed = true
		break
	}
	if !closed {
		t.Fatal("expected the connection to be closed after sustained rate-limit abuse")
	}
}
