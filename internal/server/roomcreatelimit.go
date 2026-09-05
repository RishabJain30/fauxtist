package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// roomCreateLimiter rate-limits POST /api/rooms: one modest global bucket,
// plus a stricter per-client-IP bucket. Unlike the WebSocket protocol's
// per-connection limiters (internal/room/ratelimit.go), room creation
// consumes a scarce, process-wide resource (hub.Config.MaxRooms) before a
// client has even joined a room to be rate-limited within — a single
// script hammering this endpoint could otherwise exhaust that cap and
// lock out every legitimate player until rooms start expiring.
type roomCreateLimiter struct {
	global     *rate.Limiter
	perIPLimit rate.Limit
	perIPBurst int

	mu       sync.Mutex
	perIP    map[string]*rate.Limiter
	lastSeen map[string]time.Time
}

// newRoomCreateLimiter returns a limiter generous enough for ordinary
// bursts of real players starting games at once (a global burst of 20,
// refilling one every 200ms) while still stopping a single client from
// creating more than a handful of rooms in quick succession (a burst of
// 3, refilling one every 10s).
func newRoomCreateLimiter() *roomCreateLimiter {
	return &roomCreateLimiter{
		global:     rate.NewLimiter(rate.Every(200*time.Millisecond), 20),
		perIPLimit: rate.Every(10 * time.Second),
		perIPBurst: 3,
		perIP:      map[string]*rate.Limiter{},
		lastSeen:   map[string]time.Time{},
	}
}

// allow reports whether a room-creation request from ip may proceed. Never
// blocks — a request that doesn't get a token is rejected outright, never
// queued.
func (l *roomCreateLimiter) allow(ip string) bool {
	if !l.global.Allow() {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.evictStale(now)
	lim, ok := l.perIP[ip]
	if !ok {
		lim = rate.NewLimiter(l.perIPLimit, l.perIPBurst)
		l.perIP[ip] = lim
	}
	l.lastSeen[ip] = now
	return lim.Allow()
}

// evictStale drops any per-IP bucket not used in over an hour, so this map
// can't grow unbounded over the life of a long-running process. Called
// with mu already held.
func (l *roomCreateLimiter) evictStale(now time.Time) {
	for ip, seen := range l.lastSeen {
		if now.Sub(seen) > time.Hour {
			delete(l.lastSeen, ip)
			delete(l.perIP, ip)
		}
	}
}

// clientIP extracts the best-effort client address for rate-limiting
// purposes: the leftmost X-Forwarded-For entry when present (Render's edge
// proxy sets this), falling back to the direct connection's address. This
// is a hint for spreading rate-limit buckets across distinct clients, not
// a security boundary — a client can freely spoof X-Forwarded-For, which
// only ever makes the limiter treat them as a different (still-limited)
// bucket, never grants them extra trust.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.IndexByte(xff, ','); i >= 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
