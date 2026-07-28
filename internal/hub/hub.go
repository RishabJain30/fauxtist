package hub

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/room"
)

// CodeLen is the length of a room join code.
const CodeLen = 4

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars

// entry couples a room with its cancel func and metadata for idle sweeping.
type entry struct {
	room   *room.Room
	cancel context.CancelFunc
	host   game.PlayerID
	seed   int64
}

// Hub owns the lifecycle of all rooms.
type Hub struct {
	mu    sync.Mutex
	rooms map[string]*entry
	rng   *rand.Rand
	seq   int64
}

// New creates an empty hub.
func New() *Hub {
	return &Hub{
		rooms: map[string]*entry{},
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// CreateRoom registers a new room whose only member (initially) is the host, and
// returns its join code. The engine's player list grows as players join in the
// lobby (see server.go).
func (h *Hub) CreateRoom(hostName string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	code := h.uniqueCodeLocked()
	h.seq++
	seed := time.Now().UnixNano() + h.seq
	// The host is player index 0. Its stable PlayerID is the pre-seated host id;
	// the server hands this token back to the host so it can claim the seat.
	host := game.PlayerID(code + "-host")
	players := []game.Player{{ID: host, Name: hostName}}
	r := room.NewRoom(code, players, host, seed)
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	h.rooms[code] = &entry{room: r, cancel: cancel, host: host, seed: seed}
	return code
}

// Get returns a room by code.
func (h *Hub) Get(code string) (*room.Room, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[code]
	if !ok {
		return nil, false
	}
	return e.room, true
}

// HostID returns the pre-seated host id for a room.
func (h *Hub) HostID(code string) (game.PlayerID, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.rooms[code]
	if !ok {
		return "", false
	}
	return e.host, true
}

func (h *Hub) uniqueCodeLocked() string {
	for {
		b := make([]byte, CodeLen)
		for i := range b {
			b[i] = codeAlphabet[h.rng.Intn(len(codeAlphabet))]
		}
		code := string(b)
		if _, exists := h.rooms[code]; !exists {
			return code
		}
	}
}
