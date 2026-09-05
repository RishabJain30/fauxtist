package room

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/wordbank"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// inbound couples a decoded envelope with the sender and the connection it
// arrived on.
type inbound struct {
	from     game.PlayerID
	connID   uint64
	envelope wsproto.Envelope
}

// Room is the actor goroutine that owns a single game. Every field below is
// only ever read or mutated from the Run goroutine; everything else
// (server handlers, timer callbacks) communicates with it exclusively
// through the channels declared here.
type Room struct {
	Code         string
	engine       *game.Engine
	clients      map[game.PlayerID]*Client
	voicePresent map[game.PlayerID]bool
	seats        map[game.PlayerID]seatCredential
	nextConnID   uint64

	presence        map[game.PlayerID]*presence
	nextJoinSeq     int64
	roundGeneration int64
	durations       Durations

	// revision is the room's authoritative state revision: bumped exactly
	// once per accepted command/transition that changes externally visible
	// state (see stamp, apply, processJoin, processLeave,
	// handleGraceExpired), and stamped as Seq on every outbound envelope so
	// every recipient of the same transition observes the same number even
	// though their payloads are redacted differently. Snapshot requests,
	// heartbeats, and rejected commands never bump it.
	revision int64

	// clock is the room's notion of "now", for activity tracking and
	// expiry decisions — overridden in tests via WithClock so idle-timeout
	// tests never need a real sleep. lastActivity is stamped by touch at
	// every meaningful event (creation, join/reconnect, disconnect,
	// dispatched command); MaybeExpire compares against it.
	clock        func() time.Time
	lastActivity time.Time

	inbox             chan inbound
	joins             chan joinReq
	leaves            chan leaveReq
	advance           chan struct{}
	discussionTimeout chan struct{}
	graceExpiredCh    chan graceExpiredMsg
	drawSkipCh        chan drawSkipMsg
	guessTimeoutCh    chan guessTimeoutMsg
	snapshotCh        chan snapshotReq
	expireCh          chan expireReq
	done              chan struct{}
	shutdownOnce      sync.Once

	discussionTimer    *time.Timer
	discussionDeadline time.Time
	revealTimer        *time.Timer
	drawSkipTimer      *time.Timer
	guessTimer         *time.Timer
	guessDeadline      time.Time
	graceTimers        map[game.PlayerID]*time.Timer
}

// NewRoom builds a lobby-phase room pre-seeded with its host. hostTokenHash
// is the sha256 digest of the host's reconnect token; the raw token is never
// held by the room.
func NewRoom(code string, host game.Player, hostTokenHash identity.TokenHash, seed int64, durations Durations, opts ...RoomOption) *Room {
	rng := rand.New(rand.NewSource(seed))
	wb := wordbank.New(rand.New(rand.NewSource(seed + 1)))
	r := &Room{
		Code:              code,
		engine:            game.NewEngine([]game.Player{host}, host.ID, 1, rng, wb),
		clients:           map[game.PlayerID]*Client{},
		voicePresent:      map[game.PlayerID]bool{},
		seats:             map[game.PlayerID]seatCredential{host.ID: {tokenHash: hostTokenHash}},
		presence:          map[game.PlayerID]*presence{},
		durations:         durations,
		clock:             time.Now,
		inbox:             make(chan inbound, 64),
		joins:             make(chan joinReq, 8),
		leaves:            make(chan leaveReq, 8),
		advance:           make(chan struct{}, 1),
		discussionTimeout: make(chan struct{}, 1),
		graceExpiredCh:    make(chan graceExpiredMsg, 8),
		drawSkipCh:        make(chan drawSkipMsg, 1),
		guessTimeoutCh:    make(chan guessTimeoutMsg, 1),
		snapshotCh:        make(chan snapshotReq, 4),
		expireCh:          make(chan expireReq, 1),
		done:              make(chan struct{}),
		graceTimers:       map[game.PlayerID]*time.Timer{},
	}
	for _, opt := range opts {
		opt(r)
	}
	r.lastActivity = r.clock()
	return r
}

// Run is the single-goroutine event loop. Nothing else mutates the engine,
// presence, or timers. On return, for any reason, every timer is stopped
// and every connected client is closed — an abandoned or explicitly shut
// down room never leaves either running behind it.
func (r *Room) Run(ctx context.Context) {
	defer r.Shutdown() // idempotent: marks r.done closed so blocked Join/MaybeExpire callers never hang past this point
	defer r.stopAllTimers()
	defer r.closeAllClients()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case j := <-r.joins:
			r.processJoin(j)
		case <-r.advance:
			r.apply(r.engine.AdvanceRound(), nil)
		case <-r.discussionTimeout:
			r.apply(r.engine.EndDiscussion(r.engine.State().HostID))
		case lv := <-r.leaves:
			r.processLeave(lv)
		case m := <-r.graceExpiredCh:
			r.handleGraceExpired(m)
		case m := <-r.drawSkipCh:
			r.handleDrawSkip(m)
		case m := <-r.guessTimeoutCh:
			r.handleGuessTimeout(m)
		case req := <-r.snapshotCh:
			req.resp <- RoomSnapshot{
				Phase:          r.engine.State().Phase,
				HostID:         r.engine.State().HostID,
				Players:        r.playerViews(),
				Revision:       r.revision,
				ConnectedCount: len(r.clients),
				LastActivityAt: r.lastActivity,
			}
		case req := <-r.expireCh:
			expired := r.handleExpireCheck(req)
			req.resp <- expired
			if expired {
				return
			}
		case msg := <-r.inbox:
			if c, ok := r.clients[msg.from]; ok && c.ConnID == msg.connID {
				r.handle(c, msg)
			}
		}
	}
}

// Leave unregisters a client, if its connID is still the seat's live one.
func (r *Room) Leave(id game.PlayerID, connID uint64) {
	r.leaves <- leaveReq{playerID: id, connID: connID}
}

// Submit hands an inbound message to the loop, tagged with the connection it
// arrived on so a superseded connection can no longer act for that seat.
func (r *Room) Submit(from game.PlayerID, connID uint64, env wsproto.Envelope) {
	r.inbox <- inbound{from: from, connID: connID, envelope: env}
}

// RoomSnapshot is a point-in-time, race-free read of the room's externally
// relevant state, for callers outside the Run goroutine (tests, and any
// future admin/observability endpoint) that must never touch engine or
// presence state directly.
type RoomSnapshot struct {
	Phase          game.Phase
	HostID         game.PlayerID
	Players        []wsproto.PlayerView
	Revision       int64
	ConnectedCount int
	LastActivityAt time.Time
}

type snapshotReq struct{ resp chan RoomSnapshot }

// Snapshot returns the room's current phase, host, and player list (with
// presence merged in), computed on the Run goroutine like everything else.
func (r *Room) Snapshot() RoomSnapshot {
	resp := make(chan RoomSnapshot, 1)
	r.snapshotCh <- snapshotReq{resp: resp}
	return <-resp
}

// handle dispatches one inbound message to the engine and broadcasts
// events. Every dispatched message first spends one token from its
// category's rate limiter (see ratelimit.go); a message that doesn't get
// one is rejected outright, before it ever reaches engine or validation
// logic, and never counts as activity. A client that keeps flooding past
// abuseThreshold in a row is disconnected.
func (r *Room) handle(c *Client, msg inbound) {
	if !c.allow(msg.envelope.Type) {
		c.consecutiveRateLimited++
		if c.consecutiveRateLimited > abuseThreshold {
			slog.Warn("disconnecting client for sustained rate-limit abuse", "room", r.Code, "player", c.PlayerID)
			c.close(websocket.StatusPolicyViolation, "rate limit exceeded")
			return
		}
		r.sendError(msg.from, msg.envelope.RequestID, "rate_limited", "too many requests, slow down")
		return
	}
	c.consecutiveRateLimited = 0
	r.touch()

	switch msg.envelope.Type {
	case wsproto.TypeStartGame:
		r.apply(r.engine.StartGame(msg.from))
	case wsproto.TypeStroke:
		var p wsproto.StrokePayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "bad_payload", "bad stroke payload")
			return
		}
		s, err := validateStroke(p)
		if err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "invalid_stroke", err.Error())
			return
		}
		r.apply(r.engine.AddStroke(msg.from, toStroke(msg.from, s)))
	case wsproto.TypeEndDiscussion:
		r.apply(r.engine.EndDiscussion(msg.from))
	case wsproto.TypeNewGame:
		r.apply(r.engine.Restart(msg.from))
	case wsproto.TypeCastVote:
		var p wsproto.VotePayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "bad_payload", "bad vote payload")
			return
		}
		r.apply(r.engine.CastVote(msg.from, game.PlayerID(p.Target), r.connectedSet()))
	case wsproto.TypeImpostorGuess:
		var p wsproto.ImpostorGuessPayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "bad_payload", "bad guess payload")
			return
		}
		guess, err := validateGuess(p.Guess)
		if err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "invalid_guess", err.Error())
			return
		}
		r.apply(r.engine.ImpostorGuess(msg.from, guess))
		r.evaluateGuessDeadline() // a valid guess cancels the pending timeout
	case wsproto.TypeChatMessage:
		var p wsproto.ChatPayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		text, err := validateChatText(p.Text)
		if err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "invalid_chat", err.Error())
			return
		}
		r.broadcastChat(msg.from, text)
	case wsproto.TypeVoiceJoin:
		r.voiceJoin(msg.from)
	case wsproto.TypeVoiceLeave:
		r.voiceLeave(msg.from)
	case wsproto.TypeVoiceSignal:
		var p wsproto.VoiceSignalIn
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		if err := validateVoiceSignal(p, msg.from, r.connectedSet()); err != nil {
			r.sendError(msg.from, msg.envelope.RequestID, "invalid_voice_signal", err.Error())
			return
		}
		r.relayVoiceSignal(msg.from, p)
	case wsproto.TypeVoiceState:
		var p wsproto.VoiceStateIn
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.broadcastVoiceState(msg.from, p)
	case wsproto.TypeResync:
		// Explicit resync request: a read-only re-send of the current
		// snapshot to this one connection. Never bumps the revision — it
		// observes state, it doesn't change it.
		r.sendSnapshot(c)
	default:
		r.sendError(msg.from, msg.envelope.RequestID, "unknown_message_type", "unknown message type")
	}
}

// apply broadcasts engine events, or ignores a per-action validation error.
// This is the single chokepoint every engine-event-producing action routes
// through (StartGame, AddStroke, EndDiscussion, Restart, CastVote,
// ImpostorGuess, SkipTurn, ResolveImpostorTimeout, AdvanceRound). A
// rejected command (err != nil) never bumps the revision at all. Returns
// whether anything was actually applied.
func (r *Room) apply(events []game.Event, err error) bool {
	if err != nil {
		// Validation errors are per-action; the client UI prevents most of them.
		// Kept explicit so future logging can hook in here.
		return false
	}
	return r.applyEvents(events)
}

// applyEvents broadcasts each event in order, bumping the room's revision
// once per event rather than once per call. A single accepted command can
// cascade into several ordered engine events (e.g. StartGame's
// RoundStarted followed by TurnChanged, or a vote completing both
// PhaseChanged and RoundEnded) — the frontend sequencer
// (web/src/sequencing.js) treats each seq as exactly one applied
// transition, so two distinct events sharing one seq would cause the
// second to be dropped as a duplicate. Giving every event its own revision
// keeps them individually distinguishable while every recipient of the
// SAME event (broadcastEvent's per-client fan-out) still observes the same
// number, since the bump happens once before that fan-out, not inside it.
// Returns whether anything was applied.
func (r *Room) applyEvents(events []game.Event) bool {
	if len(events) == 0 {
		return false
	}
	for _, ev := range events {
		r.revision++
		r.broadcastEvent(ev)
	}
	return true
}

func toStroke(by game.PlayerID, p wsproto.StrokePayload) game.Stroke {
	pts := make([]game.Point, len(p.Points))
	for i, pt := range p.Points {
		pts[i] = game.Point{X: pt.X, Y: pt.Y}
	}
	return game.Stroke{By: by, Points: pts, Color: p.Color, Width: p.Width}
}
