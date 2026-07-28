package room

import (
	"context"
	"encoding/json"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// Client is one player's live WebSocket connection.
type Client struct {
	PlayerID game.PlayerID
	Name     string
	conn     *websocket.Conn
	send     chan wsproto.Envelope
}

// newClient wraps a websocket connection.
func newClient(id game.PlayerID, name string, conn *websocket.Conn) *Client {
	return &Client{
		PlayerID: id,
		Name:     name,
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

// NewClientForServer is the exported constructor used by the server package.
func NewClientForServer(id game.PlayerID, name string, conn *websocket.Conn) *Client {
	return newClient(id, name, conn)
}

// WriteLoopForServer runs the client's write pump (exported for the server).
func (c *Client) WriteLoopForServer(ctx context.Context) { c.writeLoop(ctx) }
