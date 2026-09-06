package room

import (
	"time"

	"golang.org/x/time/rate"

	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// abuseThreshold is how many consecutive rate-limited messages from one
// connection are tolerated before the connection itself is closed. A
// well-behaved client backs off after a single rejection; a script that
// keeps hammering past this many in a row is treated as abuse rather than
// a burst to smooth over.
const abuseThreshold = 20

// rateLimiters bundles one connection's per-category token buckets.
// Buckets are deliberately generous relative to how a human actually
// plays — one stroke per pen gesture, one vote click, occasional chat — so
// normal gameplay never brushes against them, while a scripted flood is
// capped well before it can churn the room actor. Allow() never blocks
// (see handle), so a saturated bucket costs nothing but a dropped message.
type rateLimiters struct {
	command *rate.Limiter // declarations, orders, lock/unlock, lobby/match/rematch actions
	ping    *rate.Limiter // map pings and proposal arrows (frequent during negotiation)
	chat    *rate.Limiter
	voice   *rate.Limiter // voice_join/leave/signal/state
	resync  *rate.Limiter
}

func newRateLimiters() *rateLimiters {
	return &rateLimiters{
		command: rate.NewLimiter(rate.Every(50*time.Millisecond), 40),
		ping:    rate.NewLimiter(rate.Every(100*time.Millisecond), 20),
		chat:    rate.NewLimiter(rate.Every(500*time.Millisecond), 5),
		voice:   rate.NewLimiter(rate.Every(50*time.Millisecond), 40),
		resync:  rate.NewLimiter(rate.Every(2*time.Second), 5),
	}
}

// allow reports whether a message of the given type may proceed right now,
// against the bucket its category maps to.
func (c *Client) allow(msgType string) bool {
	switch msgType {
	case wsproto.TypeMapPing, wsproto.TypeProposalArrow:
		return c.limiters.ping.Allow()
	case wsproto.TypeChatMessage:
		return c.limiters.chat.Allow()
	case wsproto.TypeVoiceJoin, wsproto.TypeVoiceLeave, wsproto.TypeVoiceSignal, wsproto.TypeVoiceState:
		return c.limiters.voice.Allow()
	case wsproto.TypeResync:
		return c.limiters.resync.Allow()
	default:
		return c.limiters.command.Allow()
	}
}
