package room

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// maxChatHistory bounds how many recent public chat messages the room keeps
// in memory (returned in reconnect snapshots, never persisted).
const maxChatHistory = 50

// inbound couples a decoded envelope with the sender and the connection it
// arrived on.
type inbound struct {
	from     game.PlayerID
	connID   uint64
	envelope wsproto.Envelope
}

// chatEntry is one retained public chat message.
type chatEntry struct {
	From string `json:"from"`
	Name string `json:"name"`
	Text string `json:"text"`
}

// phaseFireMsg carries the generation a phase/early timer was scheduled with,
// so a stale callback fired after a transition is ignored.
type phaseFireMsg struct{ gen int64 }

// Room is the actor goroutine that owns a single match. Every field is only
// read or mutated from the Run goroutine; everything else communicates with
// it through the channels declared here.
type Room struct {
	Code   string
	engine *game.Engine

	clients      map[game.PlayerID]*Client // active-player connections
	spectators   map[game.PlayerID]*Client // read-only spectator connections
	voicePresent map[game.PlayerID]bool
	seats        map[game.PlayerID]seatCredential // active-player seat creds
	specSeats    map[game.PlayerID]seatCredential // spectator seat creds
	specViews    map[game.PlayerID]spectatorInfo  // stable spectator identity
	nextConnID   uint64

	presence    map[game.PlayerID]*presence
	ready       map[game.PlayerID]bool
	rematchOK   map[game.PlayerID]bool
	afk         map[game.PlayerID]bool
	interacted  map[game.PlayerID]int // round of a player's last interaction, for AFK
	nextJoinSeq int64
	durations   Durations

	chatHistory []chatEntry

	// revision is the room's authoritative state revision, stamped as Seq on
	// every outbound sequenced envelope so every recipient of one transition
	// observes the same number even with differently-redacted payloads. It is
	// mutated in exactly two ways: broadcastSequenced (a single shared
	// lifecycle/game envelope) and any per-viewer sequenced fan-out that bumps
	// once before the loop (see the snapshot/game-event broadcasts).
	revision int64

	// Phase timing (server-authoritative absolute deadlines).
	phaseDeadline        time.Time
	phaseTimer           *time.Timer
	phaseGen             int64 // bumped on every phase transition
	matchGen             int64 // bumped on every match start/rematch
	earlyCountdownActive bool
	earlyTimer           *time.Timer
	earlyDeadline        time.Time
	paused               bool
	pauseRemaining       time.Duration

	// phaseDurOverride lets tests shrink every phase to a few milliseconds
	// without waiting out the preset timings; nil in production.
	phaseDurOverride func(game.Phase) time.Duration

	seed         int64
	rematchCount int

	clock        func() time.Time
	lastActivity time.Time

	inbox          chan inbound
	joins          chan joinReq
	leaves         chan leaveReq
	graceExpiredCh chan graceExpiredMsg
	phaseFireCh    chan phaseFireMsg
	earlyFireCh    chan phaseFireMsg
	soloFireCh     chan int64
	snapshotCh     chan snapshotReq
	expireCh       chan expireReq
	done           chan struct{}
	shutdownOnce   sync.Once

	graceTimers map[game.PlayerID]*time.Timer
}

// NewRoom builds a lobby-phase room pre-seeded with its host. hostTokenHash is
// the sha256 digest of the host's reconnect token; the raw token is never held
// by the room.
func NewRoom(code string, host game.Player, hostTokenHash identity.TokenHash, seed int64, durations Durations, opts ...RoomOption) *Room {
	r := &Room{
		Code:           code,
		engine:         game.NewEngine(host, seed),
		clients:        map[game.PlayerID]*Client{},
		spectators:     map[game.PlayerID]*Client{},
		voicePresent:   map[game.PlayerID]bool{},
		seats:          map[game.PlayerID]seatCredential{host.ID: {tokenHash: hostTokenHash}},
		specSeats:      map[game.PlayerID]seatCredential{},
		specViews:      map[game.PlayerID]spectatorInfo{},
		presence:       map[game.PlayerID]*presence{},
		ready:          map[game.PlayerID]bool{},
		rematchOK:      map[game.PlayerID]bool{},
		afk:            map[game.PlayerID]bool{},
		interacted:     map[game.PlayerID]int{},
		durations:      durations,
		seed:           seed,
		clock:          time.Now,
		inbox:          make(chan inbound, 64),
		joins:          make(chan joinReq, 8),
		leaves:         make(chan leaveReq, 8),
		graceExpiredCh: make(chan graceExpiredMsg, 8),
		phaseFireCh:    make(chan phaseFireMsg, 4),
		earlyFireCh:    make(chan phaseFireMsg, 4),
		soloFireCh:     make(chan int64, 4),
		snapshotCh:     make(chan snapshotReq, 4),
		expireCh:       make(chan expireReq, 1),
		done:           make(chan struct{}),
		graceTimers:    map[game.PlayerID]*time.Timer{},
	}
	for _, opt := range opts {
		opt(r)
	}
	r.lastActivity = r.clock()
	return r
}

// Run is the single-goroutine event loop. Nothing else mutates the engine,
// presence, or timers. On return, every timer is stopped and every connection
// is closed.
func (r *Room) Run(ctx context.Context) {
	defer r.Shutdown()
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
		case lv := <-r.leaves:
			r.processLeave(lv)
		case m := <-r.graceExpiredCh:
			r.handleGraceExpired(m)
		case m := <-r.phaseFireCh:
			r.onPhaseFire(m.gen)
		case m := <-r.earlyFireCh:
			r.onEarlyFire(m.gen)
		case g := <-r.soloFireCh:
			r.onSoloFire(g)
		case req := <-r.snapshotCh:
			req.resp <- r.roomSnapshot()
		case req := <-r.expireCh:
			expired := r.handleExpireCheck(req)
			req.resp <- expired
			if expired {
				return
			}
		case msg := <-r.inbox:
			if c, ok := r.clients[msg.from]; ok && c.ConnID == msg.connID {
				r.handle(c, msg)
			} else if sc, ok := r.spectators[msg.from]; ok && sc.ConnID == msg.connID {
				r.handleSpectator(sc, msg)
			}
		}
	}
}

// Submit hands an inbound message to the loop, tagged with the connection it
// arrived on so a superseded connection can no longer act for that seat.
func (r *Room) Submit(from game.PlayerID, connID uint64, env wsproto.Envelope) {
	r.inbox <- inbound{from: from, connID: connID, envelope: env}
}

// Leave unregisters a client, if its connID is still the seat's live one.
func (r *Room) Leave(id game.PlayerID, connID uint64) {
	r.leaves <- leaveReq{playerID: id, connID: connID}
}

// RoomSnapshot is a race-free read of the room's externally relevant state,
// for callers outside the Run goroutine (tests, observability).
type RoomSnapshot struct {
	Phase          game.Phase
	HostID         game.PlayerID
	Round          int
	Revision       int64
	ConnectedCount int
	SpectatorCount int
	LastActivityAt time.Time
}

type snapshotReq struct{ resp chan RoomSnapshot }

func (r *Room) roomSnapshot() RoomSnapshot {
	s := r.engine.State()
	return RoomSnapshot{
		Phase:          s.Phase,
		HostID:         s.HostID,
		Round:          s.Round,
		Revision:       r.revision,
		ConnectedCount: len(r.clients),
		SpectatorCount: len(r.spectators),
		LastActivityAt: r.lastActivity,
	}
}

// Snapshot returns a race-free read of room state, computed on the Run
// goroutine like everything else.
func (r *Room) Snapshot() RoomSnapshot {
	resp := make(chan RoomSnapshot, 1)
	select {
	case r.snapshotCh <- snapshotReq{resp: resp}:
	case <-r.done:
		return RoomSnapshot{}
	}
	select {
	case s := <-resp:
		return s
	case <-r.done:
		return RoomSnapshot{}
	}
}

// handle dispatches one inbound message from an active player. Every message
// first spends a token from its rate-limit category; a client that floods
// past abuseThreshold in a row is disconnected.
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
	case wsproto.TypeSetReady:
		r.handleSetReady(c, msg)
	case wsproto.TypeUpdateSettings:
		r.handleUpdateSettings(c, msg)
	case wsproto.TypeStartMatch:
		r.handleStartMatch(c, msg)
	case wsproto.TypeSubmitDecl:
		r.handleSubmitDecl(c, msg)
	case wsproto.TypeSetOrders:
		r.handleSetOrders(c, msg)
	case wsproto.TypeLockOrders:
		r.handleLockOrders(c, msg)
	case wsproto.TypeUnlockOrders:
		r.handleUnlockOrders(c, msg)
	case wsproto.TypeMapPing:
		r.handleMapPing(c, msg)
	case wsproto.TypeProposalArrow:
		r.handleProposalArrow(c, msg)
	case wsproto.TypeChatMessage:
		r.handleChat(c, msg)
	case wsproto.TypeLeaveForNow:
		r.handleLeaveForNow(c, msg)
	case wsproto.TypeResignMatch:
		r.handleResign(c, msg)
	case wsproto.TypeEndNoContest:
		r.handleEndNoContest(c, msg)
	case wsproto.TypeKeepWaiting:
		r.handleKeepWaiting(c, msg)
	case wsproto.TypeRematchReady:
		r.handleRematchReady(c, msg)
	case wsproto.TypeStartRematch:
		r.handleStartRematch(c, msg)
	case wsproto.TypeReturnToLobby:
		r.handleReturnToLobby(c, msg)
	case wsproto.TypeRemovePlayer:
		r.handleRemovePlayer(c, msg)
	case wsproto.TypeResync:
		r.sendSnapshotTo(c)
	case wsproto.TypeVoiceJoin:
		r.voiceJoin(msg.from)
	case wsproto.TypeVoiceLeave:
		r.voiceLeave(msg.from)
	case wsproto.TypeVoiceSignal:
		r.handleVoiceSignal(c, msg)
	case wsproto.TypeVoiceState:
		r.handleVoiceState(c, msg)
	default:
		r.sendError(msg.from, msg.envelope.RequestID, "unknown_message_type", "unknown message type")
	}
}

// handleSpectator dispatches the small set of messages a read-only spectator
// may send. Everything else is rejected — a spectator never affects the match,
// player chat, or voice.
func (r *Room) handleSpectator(c *Client, msg inbound) {
	if !c.allow(msg.envelope.Type) {
		r.sendError(msg.from, msg.envelope.RequestID, "rate_limited", "too many requests, slow down")
		return
	}
	r.touch()
	switch msg.envelope.Type {
	case wsproto.TypeResync:
		r.sendSnapshotTo(c)
	case wsproto.TypeClaimSeat:
		r.handleClaimSeat(c, msg)
	case wsproto.TypeLeaveForNow, wsproto.TypeResignMatch:
		// A spectator leaving is just a socket close; nothing to forfeit.
		r.sendLeaveAccepted(c)
	default:
		r.sendError(msg.from, msg.envelope.RequestID, "spectator_forbidden", "spectators cannot perform that action")
	}
}
