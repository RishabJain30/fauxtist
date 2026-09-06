package server

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
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

// trustedProxyHops is the number of reverse proxies in front of this process
// whose X-Forwarded-For contribution can be trusted. It is configurable via
// FAUXTIST_TRUSTED_PROXY_HOPS and defaults to 1 on Render (its single trusted
// edge, detected via the RENDER env var) and 0 elsewhere (no proxy — trust
// only the direct peer). A test may override it directly.
//
// Each proxy a request passes through appends the address it observed to
// X-Forwarded-For rather than replacing it, so the Nth-from-the-right entry is
// the one that hop actually saw. An earlier version of clientIP read the
// LEFTMOST entry — but that position is whatever the original, unauthenticated
// client put there, in full: a script can set X-Forwarded-For to a fresh fake
// address on every request, and the trusted edge only ever appends its own
// observation after it, so the leftmost entry is attacker-controlled start to
// finish. Reading it meant the per-IP bucket never accumulated against any one
// real address, defeating the limiter. Trusting exactly trustedProxyHops
// entries from the right closes that gap; trusting zero uses RemoteAddr, which
// nothing upstream of net/http can spoof.
var trustedProxyHops = resolveTrustedProxyHops()

func resolveTrustedProxyHops() int {
	if n, err := envconfig.NonNegativeInt("FAUXTIST_TRUSTED_PROXY_HOPS", -1, 10); err == nil && n >= 0 {
		return n
	}
	if os.Getenv("RENDER") != "" {
		return 1
	}
	return 0
}

// clientIP extracts the client address the per-IP bucket keys on: trusting
// exactly trustedProxyHops proxy-appended X-Forwarded-For entries from the
// right, or falling back to the direct connection's own address if no proxy is
// trusted or the header is absent, malformed, or too short.
func clientIP(r *http.Request) string {
	if trustedProxyHops > 0 {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if idx := len(parts) - trustedProxyHops; idx >= 0 {
				if ip := strings.TrimSpace(parts[idx]); ip != "" {
					return ip
				}
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
