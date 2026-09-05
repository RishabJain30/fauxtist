package room

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// --- Requirement #20: reconnect credentials never appear in logs ---

func TestConnectionLifecycleLogsNeverContainReconnectTokens(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	r, hostID, hostToken := newTestRoom(t, "Host")
	startTestRoom(t, r)

	// Join() is synchronous — it blocks until processJoin (and its log
	// call) has fully run, so reading the buffer immediately afterward is
	// race-free with no extra synchronization needed.
	client, conn := dialTestConn(t)
	defer client.CloseNow()
	joinAndPump(t, r, conn, JoinRequest{Reconnect: true, PlayerID: hostID, Token: hostToken})
	drainUntilStateSnapshot(t, client)

	logged := buf.String()
	if logged == "" {
		t.Fatal("expected connection lifecycle logging to have produced output")
	}
	if strings.Contains(logged, hostToken) {
		t.Fatalf("log output leaked the host's raw reconnect token: %s", logged)
	}
}
