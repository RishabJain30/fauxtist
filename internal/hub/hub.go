package hub

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/room"
)

// ErrHubAtCapacity is returned by CreateRoom when the hub already has
// Config.MaxRooms rooms registered. Callers map this to a distinct,
// user-friendly response rather than a generic failure.
var ErrHubAtCapacity = errors.New("room capacity reached")

// entry couples a room with its cancel func and metadata for idle sweeping.
type entry struct {
	room   *room.Room
	cancel context.CancelFunc
	seed   int64
}

// Hub owns the lifecycle of all rooms.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*entry
	rng   *rand.Rand
	seq   int64
	cfg   Config
	clock func() time.Time

	sweepStop chan struct{}
	sweepDone chan struct{}
	closeOnce sync.Once
}

// Option configures optional Hub behavior at construction time.
type Option func(*Hub)

// WithConfig overrides room-lifecycle limits (empty-room TTL, sweep
// interval, max rooms) from DefaultConfig.
func WithConfig(cfg Config) Option {
	return func(h *Hub) { h.cfg = cfg }
}

// WithClock overrides the hub's notion of "now", propagated to every room
// it creates for activity/expiry tracking. Tests use this to advance time
// deterministically instead of sleeping; production never sets it.
func WithClock(clock func() time.Time) Option {
	return func(h *Hub) { h.clock = clock }
}

// New creates an empty hub and starts its background sweeper, which
// periodically reaps empty, long-idle rooms (see Sweep). Call Close when
// done with it so the sweeper goroutine and every registered room stop
// cleanly.
func New(opts ...Option) *Hub {
	h := &Hub{
		rooms: map[string]*entry{},
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
		cfg:   DefaultConfig(),
		clock: time.Now,

		sweepStop: make(chan struct{}),
		sweepDone: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(h)
	}
	go h.runSweeper()
	return h
}

// CreateRoom registers a new room and mints the host's seat credentials. It
// returns the join code, the host's playerId, and the host's raw reconnect
// token — the only time that raw token is ever available outside the room's
// own memory (where only its hash is kept).
func (h *Hub) CreateRoom(hostName string) (code string, hostID game.PlayerID, hostToken string, err error) {
	playerID, err := identity.NewPlayerID()
	if err != nil {
		return "", "", "", err
	}
	token, err := identity.NewReconnectToken()
	if err != nil {
		return "", "", "", err
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) >= h.cfg.MaxRooms {
		return "", "", "", ErrHubAtCapacity
	}
	code, err = generateUniqueCode(takenCodes(h.rooms), codeAlphabet, CodeLen, maxCodeAttempts)
	if err != nil {
		return "", "", "", err
	}
	h.seq++
	seed := time.Now().UnixNano() + h.seq
	host := game.Player{ID: game.PlayerID(playerID), Name: hostName}
	r := room.NewRoom(code, host, identity.Hash(token), seed, room.DefaultDurations(), room.WithClock(h.clock))
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	h.rooms[code] = &entry{room: r, cancel: cancel, seed: seed}
	slog.Info("room created", "code", code, "rooms", len(h.rooms))
	return code, host.ID, token, nil
}

func takenCodes(rooms map[string]*entry) map[string]bool {
	taken := make(map[string]bool, len(rooms))
	for code := range rooms {
		taken[code] = true
	}
	return taken
}

// Get returns a room by code. The room may have already self-expired (see
// room.MaybeExpire) by the time a caller acts on it — Room.Join reports
// room.ErrRoomClosed in that narrow case rather than hanging, so callers
// must handle that alongside a plain "not found".
func (h *Hub) Get(code string) (*room.Room, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[code]
	if !ok {
		return nil, false
	}
	return e.room, true
}

// Sweep checks every registered room for expiry eligibility (see
// room.MaybeExpire) and removes each one that expired itself. Safe to
// call directly (tests do, for determinism instead of waiting on the
// background ticker); the running Hub also calls this on cfg.SweepInterval.
func (h *Hub) Sweep() {
	h.mu.Lock()
	codes := make([]string, 0, len(h.rooms))
	entries := make([]*entry, 0, len(h.rooms))
	for code, e := range h.rooms {
		codes = append(codes, code)
		entries = append(entries, e)
	}
	h.mu.Unlock()

	for i, e := range entries {
		if e.room.MaybeExpire(h.cfg.EmptyRoomTTL) {
			h.removeRoom(codes[i], e.room)
		}
	}
}

// removeRoom idempotently unregisters code, but only if the currently
// registered room for it is still the exact instance want — guarding
// against deleting a newer room that reused the same code after the one
// this caller decided to remove was already gone. Cancels its context and
// signals shutdown; Room.Run's own exit path stops its timers and closes
// its clients.
func (h *Hub) removeRoom(code string, want *room.Room) {
	h.mu.Lock()
	e, ok := h.rooms[code]
	if !ok || e.room != want {
		h.mu.Unlock()
		return
	}
	delete(h.rooms, code)
	h.mu.Unlock()

	slog.Info("room expired", "code", code)
	e.cancel()
	e.room.Shutdown()
}

// runSweeper periodically calls Sweep until Close stops it.
func (h *Hub) runSweeper() {
	defer close(h.sweepDone)
	ticker := time.NewTicker(h.cfg.SweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-h.sweepStop:
			return
		case <-ticker.C:
			h.Sweep()
		}
	}
}

// Close idempotently stops the sweeper and shuts down every currently
// registered room, for graceful process shutdown. Safe to call more than
// once.
func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		close(h.sweepStop)
		<-h.sweepDone

		h.mu.Lock()
		entries := h.rooms
		h.rooms = map[string]*entry{}
		h.mu.Unlock()

		for _, e := range entries {
			e.cancel()
			e.room.Shutdown()
		}
		slog.Info("hub closed", "rooms_stopped", len(entries))
	})
}
