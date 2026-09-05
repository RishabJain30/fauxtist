package room

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"time"

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

// Room is the actor goroutine that owns a single game.
type Room struct {
	Code         string
	engine       *game.Engine
	clients      map[game.PlayerID]*Client
	voicePresent map[game.PlayerID]bool
	seats        map[game.PlayerID]seatCredential
	nextConnID   uint64

	inbox             chan inbound
	joins             chan joinReq
	leaves            chan leaveReq
	advance           chan struct{}
	discussionTimeout chan struct{}
	done              chan struct{}

	discussionTimer *time.Timer
	discussionDur   time.Duration
	revealTimer     *time.Timer
	revealDur       time.Duration
}

// NewRoom builds a lobby-phase room pre-seeded with its host. hostTokenHash
// is the sha256 digest of the host's reconnect token; the raw token is never
// held by the room.
func NewRoom(code string, host game.Player, hostTokenHash identity.TokenHash, seed int64) *Room {
	rng := rand.New(rand.NewSource(seed))
	wb := wordbank.New(rand.New(rand.NewSource(seed + 1)))
	return &Room{
		Code:              code,
		engine:            game.NewEngine([]game.Player{host}, host.ID, 1, rng, wb),
		clients:           map[game.PlayerID]*Client{},
		voicePresent:      map[game.PlayerID]bool{},
		seats:             map[game.PlayerID]seatCredential{host.ID: {tokenHash: hostTokenHash}},
		inbox:             make(chan inbound, 64),
		joins:             make(chan joinReq, 8),
		leaves:            make(chan leaveReq, 8),
		advance:           make(chan struct{}, 1),
		discussionTimeout: make(chan struct{}, 1),
		done:              make(chan struct{}),
		discussionDur:     45 * time.Second,
		revealDur:         revealDuration(),
	}
}

// revealDuration is how long the round result is shown before advancing. Env
// FAUXTIST_REVEAL_MS overrides it (used to keep integration tests fast).
func revealDuration() time.Duration {
	if ms := os.Getenv("FAUXTIST_REVEAL_MS"); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil {
			return time.Duration(n) * time.Millisecond
		}
	}
	return 6 * time.Second
}

// Run is the single-goroutine event loop. Nothing else mutates the engine.
func (r *Room) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.done:
			return
		case j := <-r.joins:
			r.processJoin(j)
		case <-r.advance:
			for _, ev := range r.engine.AdvanceRound() {
				r.broadcastEvent(ev)
			}
		case <-r.discussionTimeout:
			r.apply(r.engine.EndDiscussion(r.engine.State().HostID))
		case lv := <-r.leaves:
			r.processLeave(lv)
		case msg := <-r.inbox:
			if c, ok := r.clients[msg.from]; ok && c.ConnID == msg.connID {
				r.handle(msg)
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

// handle dispatches one inbound message to the engine and broadcasts events.
func (r *Room) handle(msg inbound) {
	switch msg.envelope.Type {
	case wsproto.TypeStartGame:
		r.apply(r.engine.StartGame(msg.from))
	case wsproto.TypeStroke:
		var p wsproto.StrokePayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, "bad stroke payload")
			return
		}
		r.apply(r.engine.AddStroke(msg.from, toStroke(msg.from, p)))
	case wsproto.TypeEndDiscussion:
		r.apply(r.engine.EndDiscussion(msg.from))
	case wsproto.TypeNewGame:
		r.apply(r.engine.Restart(msg.from))
	case wsproto.TypeCastVote:
		var p wsproto.VotePayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, "bad vote payload")
			return
		}
		r.apply(r.engine.CastVote(msg.from, game.PlayerID(p.Target)))
	case wsproto.TypeImpostorGuess:
		var p wsproto.ImpostorGuessPayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			r.sendError(msg.from, "bad guess payload")
			return
		}
		r.apply(r.engine.ImpostorGuess(msg.from, p.Guess))
	case wsproto.TypeChatMessage:
		var p wsproto.ChatPayload
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.broadcastChat(msg.from, p.Text)
	case wsproto.TypeVoiceJoin:
		r.voiceJoin(msg.from)
	case wsproto.TypeVoiceLeave:
		r.voiceLeave(msg.from)
	case wsproto.TypeVoiceSignal:
		var p wsproto.VoiceSignalIn
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.relayVoiceSignal(msg.from, p)
	case wsproto.TypeVoiceState:
		var p wsproto.VoiceStateIn
		if err := json.Unmarshal(msg.envelope.Payload, &p); err != nil {
			return
		}
		r.broadcastVoiceState(msg.from, p)
	default:
		r.sendError(msg.from, "unknown message type")
	}
}

// apply broadcasts engine events, or ignores a per-action validation error.
func (r *Room) apply(events []game.Event, err error) {
	if err != nil {
		// Validation errors are per-action; the client UI prevents most of them.
		// Kept explicit so future logging can hook in here.
		return
	}
	for _, ev := range events {
		r.broadcastEvent(ev)
	}
}

func toStroke(by game.PlayerID, p wsproto.StrokePayload) game.Stroke {
	pts := make([]game.Point, len(p.Points))
	for i, pt := range p.Points {
		pts[i] = game.Point{X: pt.X, Y: pt.Y}
	}
	return game.Stroke{By: by, Points: pts, Color: p.Color, Width: p.Width}
}
