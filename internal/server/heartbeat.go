package server

import (
	"context"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
)

// HeartbeatConfig controls how the server checks that a connection is still
// alive. A deployed WebSocket can look open even after the browser, network,
// or an intermediate proxy has actually disappeared; without this, such a
// connection would sit in the read loop forever, undetected.
type HeartbeatConfig struct {
	// Interval is how often a ping is sent to an idle connection.
	Interval time.Duration
	// Timeout is how long to wait for the matching pong before treating
	// the connection as dead.
	Timeout time.Duration
}

// DefaultHeartbeatConfig returns production defaults, each overridable by
// an environment variable (in whole milliseconds) for deployment tuning or
// fast tests:
//
//	FAUXTIST_HEARTBEAT_INTERVAL_MS
//	FAUXTIST_HEARTBEAT_TIMEOUT_MS
//
// The defaults (25s between pings, 10s to answer one) tolerate ordinary
// mobile-network latency and brief connectivity blips without either firing
// pings so often they're wasteful or taking so long to notice a dead peer
// that a disconnected player sits in limbo.
// Values are read via envconfig.PositiveDurationMS, which rejects a
// non-positive or unreasonably large override (a zero Interval, in
// particular, would otherwise reach time.NewTicker and panic) rather than
// silently applying it — main's startup validation (envconfig.Validate)
// already fails the process before this ever runs with a bad value, so
// the fallback to def here in that error case is defense in depth, not
// the primary guard.
func DefaultHeartbeatConfig() HeartbeatConfig {
	interval, _ := envconfig.PositiveDurationMS("FAUXTIST_HEARTBEAT_INTERVAL_MS", 25*time.Second)
	timeout, _ := envconfig.PositiveDurationMS("FAUXTIST_HEARTBEAT_TIMEOUT_MS", 10*time.Second)
	return HeartbeatConfig{
		Interval: interval,
		Timeout:  timeout,
	}
}

// runHeartbeat pings conn on cfg.Interval until ctx is done or a ping goes
// unanswered within cfg.Timeout, at which point it closes conn and returns.
// That close unblocks the connection's own read loop with an error, which
// runs the server's normal disconnect path (room.Leave, presence, grace) —
// heartbeat failure needs no disconnect handling of its own.
//
// Ping is safe to call concurrently with the read loop that's always
// running alongside this (nhooyr.io/websocket's documented contract: every
// Conn method may be called concurrently except Reader/Read with itself).
// Ping does not itself read from the connection; it writes a ping frame and
// waits for the already-running read loop to observe the matching pong. A
// stale connection that's been replaced by a reconnect is simply closed by
// its owner elsewhere, which makes this loop's next Ping fail immediately
// and return — it never touches presence or looks anything up by player,
// so a late pong (or a late failure) here can never affect a different,
// newer connection for the same seat.
func runHeartbeat(ctx context.Context, conn *websocket.Conn, cfg HeartbeatConfig) {
	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				_ = conn.CloseNow()
				return
			}
		}
	}
}
