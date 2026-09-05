# Join / reconnect protocol

Every WebSocket connection to `/ws/room/{code}` starts with exactly one
`join` frame, which is either a **new join** or a **reconnect**:

```json
// new join — a player the room has never seen
{"type": "join", "payload": {"name": "Alice", "emoji": "🦊"}}

// reconnect — claiming an existing seat (the host's first connection is
// always a reconnect, since the host seat is created at room-creation time)
{"type": "join", "payload": {"playerId": "...", "reconnectToken": "..."}}
```

The server tells the two apart by whether `playerId`/`reconnectToken` are
present; if either is, the whole frame is treated as a reconnect attempt.

## Identity

- `playerId`: 128 bits of `crypto/rand`, base64 URL-safe, no padding. Public,
  sent to every client in room snapshots.
- `reconnectToken`: 256 bits of `crypto/rand`, same encoding. Secret — only
  the server and the seat's owner ever see the raw value.
- The server stores only `sha256(reconnectToken)` and compares candidates in
  constant time. Neither value is derived from the room code or the player's
  name, so knowing the room code (which every invited player has) is not
  enough to guess or claim a seat.

## Server responses

- **New join, accepted:** a private `join_accepted` frame
  (`{playerId, reconnectToken}`), sent only to that connection, followed by
  the usual `room_state` snapshot.
- **Reconnect, accepted:** just the `room_state` snapshot — the client
  already has its credentials, it's how it reconnected. The existing seat's
  name, emoji, score, host status, and game state are preserved unchanged.
- **Rejected** (either path): a structured `error` frame with a stable
  `code`, then the connection is closed:
  - `invalid_join` — malformed or invalid new-join request
  - `invalid_reconnect` — unknown playerId, or a token that doesn't match
  - `name_taken` — another seat already has that name (case-insensitive)
  - `room_full` — roster already at capacity
  - `game_started` — new joins are lobby-only

## Connection replacement

Reconnecting always wins the seat: any previous live connection for that
`playerId` is closed server-side (`reason: "replaced by reconnect"`) and can
no longer submit actions — every inbound message is tagged with the
connection's id, and the room drops anything tagged with a superseded id.

## Frontend persistence

The frontend stores `{playerId, reconnectToken}` in `sessionStorage`, keyed
by room code (`web/src/credentials.js`), and rewrites the URL to
`/join/<code>` on entry so a page refresh can look the credentials back up
and reconnect automatically instead of prompting for a name again. The
reconnect token is never rendered, logged, or put in an invite link — invite
links only ever carry the room code.
