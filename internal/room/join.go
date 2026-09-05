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

// JoinRequest is a client's parsed join or reconnect attempt. Exactly one of
// the two shapes is populated: a new join carries Name/Emoji, a reconnect
// carries PlayerID/Token.
type JoinRequest struct {
	Reconnect bool
	Name      string
	Emoji     string
	PlayerID  game.PlayerID
	Token     string
}

// JoinResult is returned to the server after a successful join or reconnect.
type JoinResult struct {
	Client   *Client
	PlayerID game.PlayerID
	ConnID   uint64
}

// seatCredential is the server-held proof of ownership for one seat. Only the
// token's hash is ever stored.
type seatCredential struct {
	tokenHash identity.TokenHash
}

// Sentinel join/reconnect failures. Each maps to a stable protocol error
// code via JoinErrorCode so the client can branch without parsing text.
var (
	ErrInvalidJoin      = errors.New("invalid join request")
	ErrInvalidReconnect = errors.New("invalid or expired reconnect token")
	ErrNameTaken        = errors.New("that name is already taken in this room")
	ErrRoomFull         = errors.New("room is full")
	ErrGameStarted      = errors.New("game already started")
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
	case errors.Is(err, ErrGameStarted):
		return "game_started"
	case errors.Is(err, ErrRoomClosed):
		return "room_closed"
	default:
		return "invalid_join"
	}
}

// joinReq carries one join attempt into the Run loop.
type joinReq struct {
	conn *websocket.Conn
	req  JoinRequest
	resp chan joinOutcome
}

type joinOutcome struct {
	result JoinResult
	err    error
}

// ErrRoomClosed is returned by Join when the room's actor has already
// stopped (expired from inactivity, or the process is shutting down) by
// the time this call reaches it — a narrow window between Hub.Get
// returning a room and this call reaching its now-dead actor. Callers
// treat it the same as "room not found".
var ErrRoomClosed = errors.New("room is closed")

// Join hands a freshly accepted connection and its parsed join/reconnect
// request to the room loop, and blocks until it has been resolved. Guards
// against the room's actor having already stopped (see ErrRoomClosed)
// instead of blocking forever on a request nothing will ever answer.
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

// processJoin runs on the Run goroutine: it resolves identity, replaces any
// prior connection for the same seat, and registers the new one.
func (r *Room) processJoin(j joinReq) {
	player, isNew, token, err := r.resolveJoin(j.req)
	if err != nil {
		j.resp <- joinOutcome{err: err}
		return
	}

	connID := r.nextConnID
	r.nextConnID++
	c := newClient(player.ID, connID, player.Name, player.Emoji, j.conn)

	if old, exists := r.clients[player.ID]; exists {
		old.closeReplaced()
	}
	r.clients[player.ID] = c
	slog.Info("player connected", "room", r.Code, "player", player.ID, "reconnect", j.req.Reconnect)
	// A join or reconnect always changes externally visible state (a new
	// roster entry, or a presence flip visible in every client's player
	// list) — bump once for the whole operation, before anything it
	// triggers (presence, host migration, the lobby broadcast) sends.
	r.revision++
	r.touch()
	r.markConnected(player.ID)

	if isNew {
		r.sendJoinAccepted(c, token)
	}
	r.sendSnapshot(c)
	r.broadcastLobby()

	j.resp <- joinOutcome{result: JoinResult{Client: c, PlayerID: player.ID, ConnID: connID}}
}

// resolveJoin validates a join/reconnect attempt against current room state
// and, for a fresh join, mints new seat credentials. Must only run on the
// Run goroutine.
func (r *Room) resolveJoin(req JoinRequest) (player game.Player, isNew bool, token string, err error) {
	if req.Reconnect {
		p, e := r.resolveReconnect(req)
		return p, false, "", e
	}
	return r.resolveNewJoin(req)
}

func (r *Room) resolveReconnect(req JoinRequest) (game.Player, error) {
	if req.PlayerID == "" || req.Token == "" {
		return game.Player{}, ErrInvalidReconnect
	}
	seat, ok := r.seats[req.PlayerID]
	if !ok || !identity.Verify(req.Token, seat.tokenHash) {
		return game.Player{}, ErrInvalidReconnect
	}
	for _, p := range r.engine.State().Players {
		if p.ID == req.PlayerID {
			return p, nil
		}
	}
	// Seat credential exists but the player row is missing; should not
	// happen, treat as invalid rather than fabricating a player.
	return game.Player{}, ErrInvalidReconnect
}

func (r *Room) resolveNewJoin(req JoinRequest) (game.Player, bool, string, error) {
	name, verr := ValidatePlayerName(req.Name)
	if verr != nil {
		return game.Player{}, false, "", ErrInvalidJoin
	}
	for _, p := range r.engine.State().Players {
		if strings.EqualFold(strings.TrimSpace(p.Name), name) {
			return game.Player{}, false, "", ErrNameTaken
		}
	}

	emoji, eerr := validateEmoji(req.Emoji)
	if eerr != nil {
		return game.Player{}, false, "", ErrInvalidJoin
	}

	playerID, perr := identity.NewPlayerID()
	if perr != nil {
		return game.Player{}, false, "", ErrInvalidJoin
	}
	token, terr := identity.NewReconnectToken()
	if terr != nil {
		return game.Player{}, false, "", ErrInvalidJoin
	}

	player := game.Player{ID: game.PlayerID(playerID), Name: name, Emoji: emoji}
	if uerr := r.engine.UpsertPlayer(player); uerr != nil {
		switch {
		case errors.Is(uerr, game.ErrRoomFull):
			return game.Player{}, false, "", ErrRoomFull
		case errors.Is(uerr, game.ErrWrongPhase):
			return game.Player{}, false, "", ErrGameStarted
		default:
			return game.Player{}, false, "", ErrInvalidJoin
		}
	}

	r.seats[player.ID] = seatCredential{tokenHash: identity.Hash(token)}
	return player, true, token, nil
}

func (r *Room) sendJoinAccepted(c *Client, token string) {
	env, err := wsproto.Encode(wsproto.TypeJoinAccepted, wsproto.JoinAcceptedPayload{
		PlayerID:       string(c.PlayerID),
		ReconnectToken: token,
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

// processLeave runs on the Run goroutine. A disconnect only removes a client
// if its connID still matches the currently registered connection for that
// seat; a stale connection replaced by a reconnect must not evict the seat's
// new, live connection. This never removes the player from the game roster
// — that only ever happens via presence's reconnect-grace expiry (in the
// lobby) or the phase-specific disconnect rules (in an active game).
func (r *Room) processLeave(lv leaveReq) {
	c, ok := r.clients[lv.playerID]
	if !ok || c.ConnID != lv.connID {
		return
	}
	delete(r.clients, lv.playerID)
	r.revision++ // presence flip, visible in every client's player list
	r.touch()
	slog.Info("player disconnected", "room", r.Code, "player", lv.playerID)
	if r.voicePresent[lv.playerID] {
		delete(r.voicePresent, lv.playerID)
		r.broadcastVoicePeerLeft(lv.playerID)
	}
	r.markDisconnected(lv.playerID)
}
