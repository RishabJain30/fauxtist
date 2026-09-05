package hub

import (
	"context"
	"math/rand"
	"sync"
	"time"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/room"
)

// CodeLen is the length of a room join code.
const CodeLen = 4

const codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no ambiguous chars

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
}

// New creates an empty hub.
func New() *Hub {
	return &Hub{
		rooms: map[string]*entry{},
		rng:   rand.New(rand.NewSource(time.Now().UnixNano())),
	}
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
	code = h.uniqueCodeLocked()
	h.seq++
	seed := time.Now().UnixNano() + h.seq
	host := game.Player{ID: game.PlayerID(playerID), Name: hostName}
	r := room.NewRoom(code, host, identity.Hash(token), seed)
	ctx, cancel := context.WithCancel(context.Background())
	go r.Run(ctx)
	h.rooms[code] = &entry{room: r, cancel: cancel, seed: seed}
	return code, host.ID, token, nil
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
