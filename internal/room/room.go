package room

import (
	"context"
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wordbank"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// inbound couples a decoded envelope with its sender.
type inbound struct {
	from     game.PlayerID
	envelope wsproto.Envelope
}

// joinReq registers a client with the room loop.
type joinReq struct {
	client *Client
	resp   chan error
}

// Room is the actor goroutine that owns a single game.
type Room struct {
	Code         string
	engine       *game.Engine
	clients      map[game.PlayerID]*Client
	voicePresent map[game.PlayerID]bool

	inbox   chan inbound
	joins   chan joinReq
	leaves  chan game.PlayerID
	advance chan struct{}
	done    chan struct{}

	discussionTimer *time.Timer
	discussionDur   time.Duration
	revealTimer     *time.Timer
	revealDur       time.Duration
}

// NewRoom builds a lobby-phase room. Players are added as they join (Task 12
// creates their engine entries before the game starts); for simplicity in v1 the
// engine is created once enough players have joined the lobby.
func NewRoom(code string, players []game.Player, host game.PlayerID, seed int64) *Room {
	rng := rand.New(rand.NewSource(seed))
	wb := wordbank.New(rand.New(rand.NewSource(seed + 1)))
	return &Room{
		Code:          code,
		engine:        game.NewEngine(players, host, len(players), rng, wb),
		clients:       map[game.PlayerID]*Client{},
		voicePresent:  map[game.PlayerID]bool{},
		inbox:         make(chan inbound, 64),
		joins:         make(chan joinReq, 8),
		leaves:        make(chan game.PlayerID, 8),
		advance:       make(chan struct{}, 1),
		done:          make(chan struct{}),
		discussionDur: 45 * time.Second,
		revealDur:     revealDuration(),
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
			err := r.engine.UpsertPlayer(game.Player{ID: j.client.PlayerID, Name: j.client.Name, Emoji: j.client.Emoji})
			if err != nil {
				j.resp <- err
				continue
			}
			r.clients[j.client.PlayerID] = j.client
			r.sendSnapshot(j.client)
			r.broadcastLobby()
			j.resp <- nil
		case <-r.advance:
			for _, ev := range r.engine.AdvanceRound() {
				r.broadcastEvent(ev)
			}
		case id := <-r.leaves:
			delete(r.clients, id)
			if r.voicePresent[id] {
				delete(r.voicePresent, id)
				r.broadcastVoicePeerLeft(id)
			}
			r.broadcastPlayerLeft(id)
		case msg := <-r.inbox:
			r.handle(msg)
		}
	}
}

// Join registers a client and blocks until the room has processed it, returning
// any rejection (room full or game already started).
func (r *Room) Join(c *Client) error {
	resp := make(chan error, 1)
	r.joins <- joinReq{client: c, resp: resp}
	return <-resp
}

// Leave unregisters a client.
func (r *Room) Leave(id game.PlayerID) { r.leaves <- id }

// Submit hands an inbound message to the loop.
func (r *Room) Submit(from game.PlayerID, env wsproto.Envelope) {
	r.inbox <- inbound{from: from, envelope: env}
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
