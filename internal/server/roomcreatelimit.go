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

// trustedProxyHops is the number of reverse proxies in front of this
// process whose X-Forwarded-For contribution can be trusted: exactly one
// — Render's own edge (see render.yaml and README.md's deployment
// section). This process is never deployed behind any other proxy.
//
// Each proxy a request passes through appends the address it observed to
// X-Forwarded-For rather than replacing it, so the Nth-from-the-right
// entry is the one that hop actually saw. An earlier version of clientIP
// read the LEFTMOST entry instead — but that position is whatever the
// original, unauthenticated client put there, in full: a script can set
// X-Forwarded-For to a fresh fake address on every single request, and
// Render's edge only ever appends its own observation after it, so the
// leftmost entry is attacker-controlled start to finish. Reading it meant
// the per-IP bucket below never accumulated against any one real address,
// defeating the limiter entirely. Trusting exactly trustedProxyHops
// entries from the right closes that gap: no matter what a client
// prepends, only the value Render's edge itself appended is ever used.
const trustedProxyHops = 1

// clientIP extracts the client address the per-IP bucket below keys on,
// trusting exactly trustedProxyHops proxy-appended X-Forwarded-For
// entries from the right, or falling back to the direct connection's own
// address (r.RemoteAddr, which nothing upstream of net/http can spoof) if
// the header is absent, malformed, or too short to contain a
// trusted-hop's entry at all.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if idx := len(parts) - trustedProxyHops; idx >= 0 {
			if ip := strings.TrimSpace(parts[idx]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
