package room

import (
	"context"
	"encoding/json"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// Client is one player's live WebSocket connection. ConnID identifies this
// specific connection instance for a seat; a later reconnect mints a new
// Client with a new ConnID, and the old one is closed and no longer
// authoritative for its PlayerID.
type Client struct {
	PlayerID game.PlayerID
	ConnID   uint64
	Name     string
	Emoji    string
	conn     *websocket.Conn
	send     chan wsproto.Envelope
}

// newClient wraps a websocket connection with resolved seat identity.
func newClient(id game.PlayerID, connID uint64, name, emoji string, conn *websocket.Conn) *Client {
	return &Client{
		PlayerID: id,
		ConnID:   connID,
		Name:     name,
		Emoji:    emoji,
		conn:     conn,
		send:     make(chan wsproto.Envelope, 32),
	}
}

// writeLoop drains the send channel to the socket until the context is done.
func (c *Client) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-c.send:
			if !ok {
				return
			}
			b, err := json.Marshal(env)
			if err != nil {
				continue
			}
			if err := c.conn.Write(ctx, websocket.MessageText, b); err != nil {
				return
			}
		}
	}
}

// trySend enqueues a message, dropping it if the buffer is full (a slow client
// must never block the room goroutine).
func (c *Client) trySend(env wsproto.Envelope) {
	select {
	case c.send <- env:
	default:
	}
}

// closeReplaced closes a superseded connection so its read loop unblocks
// immediately instead of waiting on a future read or network timeout. The
// close handshake (which waits on the peer) runs in its own goroutine so it
// can never stall the room's single-threaded actor loop.
func (c *Client) closeReplaced() {
	go func() {
		_ = c.conn.Close(websocket.StatusNormalClosure, "replaced by reconnect")
	}()
}

// WriteLoopForServer runs the client's write pump (exported for the server).
func (c *Client) WriteLoopForServer(ctx context.Context) { c.writeLoop(ctx) }
