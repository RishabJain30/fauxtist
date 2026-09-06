package room

import (
	"errors"
	"log/slog"
	"strings"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/identity"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// JoinRequest is a client's parsed join, reconnect, or spectate attempt.
type JoinRequest struct {
	Reconnect   bool
	Name        string
	Emoji       string
	PlayerID    game.PlayerID
	Token       string
	AsSpectator bool
}

// JoinResult is returned to the server after a successful join/reconnect.
type JoinResult struct {
	Client    *Client
	PlayerID  game.PlayerID
	ConnID    uint64
	Spectator bool
}

// seatCredential is the server-held proof of ownership for one seat. Only the
// token's hash is ever stored.
type seatCredential struct {
	tokenHash identity.TokenHash
}

// spectatorInfo is a watcher's stable identity, kept so a reconnect and the
// spectator list can render them without an engine row.
type spectatorInfo struct {
	name  string
	emoji string
}

// Sentinel join/reconnect failures, each mapping to a stable protocol code.
var (
	ErrInvalidJoin      = errors.New("invalid join request")
	ErrInvalidReconnect = errors.New("invalid or expired reconnect token")
	ErrNameTaken        = errors.New("that name is already taken in this room")
	ErrRoomFull         = errors.New("room is full")
	ErrSpectatorsFull   = errors.New("this room has too many spectators")
	ErrGameStarted      = errors.New("match already started")
)

// JoinErrorCode maps a join/reconnect failure to a stable protocol error code.
func JoinErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrInvalidReconnect):
		return "invalid_reconnect"
	case errors.Is(err, ErrNameTaken):
		return "name_taken"
	case errors.Is(err, ErrRoomFull):
		return "room_full"
	case errors.Is(err, ErrSpectatorsFull):
		return "spectators_full"
	case errors.Is(err, ErrGameStarted):
		return "game_started"
	case errors.Is(err, ErrRoomClosed):
		return "room_closed"
	default:
		return "invalid_join"
	}
}

type joinReq struct {
	conn *websocket.Conn
	req  JoinRequest
	resp chan joinOutcome
}

type joinOutcome struct {
	result JoinResult
	err    error
}

// ErrRoomClosed is returned by Join when the room's actor has already stopped
// by the time the call reaches it.
var ErrRoomClosed = errors.New("room is closed")

// Join hands a freshly accepted connection and its parsed request to the room
// loop and blocks until resolved.
func (r *Room) Join(conn *websocket.Conn, req JoinRequest) (JoinResult, error) {
	resp := make(chan joinOutcome, 1)
	select {
	case r.joins <- joinReq{conn: conn, req: req, resp: resp}:
	case <-r.done:
		return JoinResult{}, ErrRoomClosed
	}
	select {
	case out := <-resp:
		return out.result, out.err
	case <-r.done:
		return JoinResult{}, ErrRoomClosed
	}
}

// processJoin runs on the Run goroutine: resolves identity, replaces any prior
// connection for the same seat, registers the new one, and sends the initial
// snapshot.
func (r *Room) processJoin(j joinReq) {
	if j.req.Reconnect {
		r.processReconnect(j)
		return
	}
	if j.req.AsSpectator || r.engine.Phase() != game.PhaseLobby {
		r.processSpectatorJoin(j)
		return
	}
	r.processActiveJoin(j)
}

func (r *Room) processActiveJoin(j joinReq) {
	name, verr := ValidatePlayerName(j.req.Name)
	if verr != nil {
		j.resp <- joinOutcome{err: ErrInvalidJoin}
		return
	}
	for _, p := range r.engine.State().Players {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			j.resp <- joinOutcome{err: ErrNameTaken}
			return
		}
	}
	emoji, eerr := validateEmoji(j.req.Emoji)
	if eerr != nil {
		j.resp <- joinOutcome{err: ErrInvalidJoin}
		return
	}
	playerID, perr := identity.NewPlayerID()
	if perr != nil {
		j.resp <- joinOutcome{err: ErrInvalidJoin}
		return
	}
	token, terr := identity.NewReconnectToken()
	if terr != nil {
		j.resp <- joinOutcome{err: ErrInvalidJoin}
		return
	}
	player := game.Player{ID: game.PlayerID(playerID), Name: name, Emoji: emoji}
	if uerr := r.engine.UpsertPlayer(player); uerr != nil {
		switch {
		case errors.Is(uerr, game.ErrRoomFull):
			j.resp <- joinOutcome{err: ErrRoomFull}
		case errors.Is(uerr, game.ErrGameStarted):
			j.resp <- joinOutcome{err: ErrGameStarted}
		default:
			j.resp <- joinOutcome{err: ErrInvalidJoin}
		}
		return
	}
	r.seats[player.ID] = seatCredential{tokenHash: identity.Hash(token)}
	r.ready[player.ID] = false

	c := r.registerClient(player.ID, name, emoji, false, j.conn)
	r.markConnected(player.ID)
	r.sendJoinAccepted(c, token, false)
	r.sendSnapshotTo(c)
	r.broadcastLobby()
	slog.Info("player joined", "room", r.Code, "player", player.ID)
	j.resp <- joinOutcome{result: JoinResult{Client: c, PlayerID: player.ID, ConnID: c.ConnID}}
}

func (r *Room) processSpectatorJoin(j joinReq) {
	if len(r.specSeats) >= game.MaxSpectators {
		j.resp <- joinOutcome{err: ErrSpectatorsFull}
		return
	}
	name, verr := ValidatePlayerName(j.req.Name)
	if verr != nil {
		name = "Spectator"
	}
	emoji, _ := validateEmoji(j.req.Emoji)
	specID, perr := identity.NewPlayerID()
	if perr != nil {
		j.resp <- joinOutcome{err: ErrInvalidJoin}
		return
	}
	token, terr := identity.NewReconnectToken()
	if terr != nil {
		j.resp <- joinOutcome{err: ErrInvalidJoin}
		return
	}
	id := game.PlayerID(specID)
	r.specSeats[id] = seatCredential{tokenHash: identity.Hash(token)}
	r.specViews[id] = spectatorInfo{name: name, emoji: emoji}

	c := r.registerClient(id, name, emoji, true, j.conn)
	r.sendJoinAccepted(c, token, true)
	r.sendSnapshotTo(c)
	r.broadcastSpectatorUpdate()
	slog.Info("spectator joined", "room", r.Code, "spectator", id)
	j.resp <- joinOutcome{result: JoinResult{Client: c, PlayerID: id, ConnID: c.ConnID, Spectator: true}}
}

func (r *Room) processReconnect(j joinReq) {
	if j.req.PlayerID == "" || j.req.Token == "" {
		j.resp <- joinOutcome{err: ErrInvalidReconnect}
		return
	}
	// Active seat?
	if seat, ok := r.seats[j.req.PlayerID]; ok && identity.Verify(j.req.Token, seat.tokenHash) {
		p := r.engine.State().PlayerByID(j.req.PlayerID)
		if p == nil {
			j.resp <- joinOutcome{err: ErrInvalidReconnect}
			return
		}
		c := r.registerClient(j.req.PlayerID, p.Name, p.Emoji, false, j.conn)
		r.markConnected(j.req.PlayerID)
		r.sendSnapshotTo(c)
		r.broadcastLobby()
		slog.Info("player reconnected", "room", r.Code, "player", j.req.PlayerID)
		j.resp <- joinOutcome{result: JoinResult{Client: c, PlayerID: j.req.PlayerID, ConnID: c.ConnID}}
		return
	}
	// Spectator seat?
	if seat, ok := r.specSeats[j.req.PlayerID]; ok && identity.Verify(j.req.Token, seat.tokenHash) {
		info := r.specViews[j.req.PlayerID]
		c := r.registerClient(j.req.PlayerID, info.name, info.emoji, true, j.conn)
		r.sendSnapshotTo(c)
		r.broadcastSpectatorUpdate()
		j.resp <- joinOutcome{result: JoinResult{Client: c, PlayerID: j.req.PlayerID, ConnID: c.ConnID, Spectator: true}}
		return
	}
	j.resp <- joinOutcome{err: ErrInvalidReconnect}
}

// registerClient mints a Client, replaces any prior live connection for the
// seat, and records it in the right map.
func (r *Room) registerClient(id game.PlayerID, name, emoji string, spectator bool, conn *websocket.Conn) *Client {
	connID := r.nextConnID
	r.nextConnID++
	c := newClient(id, connID, name, emoji, conn)
	c.Spectator = spectator
	if spectator {
		if old, ok := r.spectators[id]; ok {
			old.closeReplaced()
		}
		r.spectators[id] = c
	} else {
		if old, ok := r.clients[id]; ok {
			old.closeReplaced()
		}
		r.clients[id] = c
	}
	r.touch()
	return c
}

func (r *Room) sendJoinAccepted(c *Client, token string, spectator bool) {
	env, err := wsproto.Encode(wsproto.TypeJoinAccepted, wsproto.JoinAcceptedPayload{
		PlayerID:       string(c.PlayerID),
		ReconnectToken: token,
		Spectator:      spectator,
	})
	if err == nil {
		c.trySend(env)
	}
}

// leaveReq carries one disconnect notice into the Run loop.
type leaveReq struct {
	playerID game.PlayerID
	connID   uint64
}

// processLeave removes a client if its connID still matches the seat's live
// connection. Never removes a player from the engine roster — that only
// happens via reconnect-grace expiry (lobby) or an explicit resign.
func (r *Room) processLeave(lv leaveReq) {
	if c, ok := r.spectators[lv.playerID]; ok && c.ConnID == lv.connID {
		delete(r.spectators, lv.playerID)
		r.touch()
		r.broadcastSpectatorUpdate()
		return
	}
	c, ok := r.clients[lv.playerID]
	if !ok || c.ConnID != lv.connID {
		return
	}
	delete(r.clients, lv.playerID)
	r.touch()
	slog.Info("player disconnected", "room", r.Code, "player", lv.playerID)
	if r.voicePresent[lv.playerID] {
		delete(r.voicePresent, lv.playerID)
		r.broadcastVoicePeerLeft(lv.playerID)
	}
	r.markDisconnected(lv.playerID)
}
