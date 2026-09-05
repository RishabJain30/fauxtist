# Wire protocol

Fauxtist speaks a versioned JSON protocol over one WebSocket connection per
player, at `/ws/room/{code}`. This document covers the envelope, revision/
sequencing, the canonical snapshot, redaction rules, the reconnect
lifecycle, heartbeat behavior, error payloads, and close codes. For the
join/reconnect handshake itself (identity, credentials), see the section
below — the rest of this document assumes that handshake has already
succeeded.

## Protocol version

The current version is **1** (`wsproto.ProtocolVersion` in Go,
`PROTOCOL_VERSION` in `web/src/protocol.js`). Every message in both
directions carries it. The server checks it once, on the very first
(join) frame of a connection; a mismatch is rejected with a structured
error and `CloseUnsupportedVersion` before any session exists. A
mismatched or malformed frame arriving *after* a session is established
is silently dropped rather than closing an otherwise-healthy connection —
see [Malformed and mismatched frames](#malformed-and-mismatched-frames-mid-session).

## Envelope

Every server → client message:

```json
{
  "version": 1,
  "type": "state_snapshot",
  "roomId": "ABCD",
  "seq": 42,
  "payload": {}
}
```

- `roomId` — the room's public join code.
- `seq` — the room's authoritative revision at the moment this message was
  sent (see [Revisions and sequencing](#revisions-and-sequencing)). Chat
  and voice messages still carry the current `seq` for envelope
  consistency, but never advance it themselves.

Every client → server command:

```json
{
  "version": 1,
  "type": "cast_vote",
  "requestId": "8f2c...",
  "payload": {}
}
```

- `requestId` — a client-generated id. The server echoes it back on any
  `error` produced by that specific command, so a client can correlate a
  rejection with the exact command that triggered it.

Both directions are parsed through one typed path (`wsproto.Envelope` in
Go, `parseServerMessage`/`encodeCommand` in `web/src/protocol.js`) — no
handler builds or reads a raw `{type, payload}` object by hand.

### Client command types

`join`, `start_game`, `stroke`, `chat_message`, `cast_vote`,
`impostor_guess`, `end_discussion`, `new_game`, `resync`,
`ice_config_request`, plus the voice signaling types (`voice_join`,
`voice_leave`, `voice_signal`, `voice_state`).

Every one of these (except `join`, handled by its own dedicated path) is
validated for shape — known type, non-empty/bounded `requestId`, bounded
payload size, exactly one JSON value with no trailing data — before it
ever reaches a room actor (`wsproto.ValidateEnvelope`); a failure here is
dropped the same as any other malformed mid-session frame (see
[Malformed and mismatched frames](#malformed-and-mismatched-frames-mid-session)).
Per-type semantic limits (stroke point count/coordinate bounds/width/
color, chat/guess length, voice-signal target/kind/size) are enforced
once the room actor unmarshals the payload — see
[Input validation](#input-validation).

### Server event types

`state_snapshot`, `join_accepted`, `lobby_update`, `player_left`,
`player_presence_changed`, `host_changed`, `round_started`,
`stroke_broadcast`, `turn_changed`, `phase_changed`, `vote_update`,
`round_result`, `game_over`, `chat_broadcast`, `error`, `ice_config`,
plus the voice broadcast types (`voice_peers`, `voice_peer_joined`,
`voice_peer_left`, `voice_signal`).

## Revisions and sequencing

Each room maintains one monotonically increasing `revision` counter,
stamped as `seq` on every outbound message. It advances **exactly once**
per accepted, externally-visible state change — a join, a reconnect, a
disconnect, a reconnect-grace expiry, or any client command the game
engine accepts — even when that one change fans out into several
recipient-specific payloads (e.g. `round_started`, where the impostor and
everyone else receive different fields) or cascades into several wire
messages. Every recipient of one transition observes the same `seq`,
regardless of how their own payload was redacted.

It does **not** advance for: a snapshot/resync request (read-only), a
heartbeat, or a rejected command. Timestamps are never used as a sequence
— only this counter.

### Client-side ordering rules

The client tracks the highest sequenced revision it has successfully
applied (see `web/src/sequencing.js`, a pure function unit-tested without
a socket). Only "sequenced" message types participate — the ones that
represent actual room/game state (everything in
[server event types](#server-event-types) above except `join_accepted`,
`error`, and the chat/voice types, which are never gated on sequence):

- **Duplicate or older** (`seq <= lastApplied`): ignored.
- **Next expected** (`seq == lastApplied + 1`): applied, and becomes the
  new baseline.
- **Gap** (`seq > lastApplied + 1`): *not* applied. The client sends
  `resync` and, until the next `state_snapshot` arrives, drops any further
  sequenced messages rather than risk applying them out of order.
- **`state_snapshot`**: always a full, atomic state replacement rather
  than a merge. Accepted if its `seq` is not older than the current
  baseline (a snapshot can legitimately repeat or advance the baseline,
  but never regress it) — an older one arriving late (e.g. from a slow
  request racing a newer push) is dropped.

Sequence tracking resets whenever the client intentionally switches rooms.

## The canonical snapshot

`state_snapshot` is the one message a client needs to fully reconstruct
its UI, for any phase, without depending on any incremental event it may
have missed. It's sent after every successful initial attach, every
successful reconnect, and every explicit `resync` request. There is a
single backend builder (`(*Room).buildSnapshot` in
`internal/room/broadcast.go`) that every one of those call sites uses, so
visibility/redaction logic exists in exactly one place.

Payload fields (all viewer-specific; absent/defaulted fields simply don't
apply to the current phase):

| Field | Meaning |
|---|---|
| `phase`, `round`, `totalRounds`, `hostId` | Core room state |
| `you` | The viewer's own public identity (id/name/emoji/score/connected) |
| `players` | Ordered roster, with score and presence |
| `currentPlayer`, `turnIndex`, `lap`, `totalLaps` | Drawing progress |
| `strokes` | Every stroke committed so far this round, to rebuild the canvas |
| `category`, `word`, `youAreImpostor` | Round secrets — see redaction below |
| `discussionDeadlineMs` | Absolute deadline (epoch ms), discussion phase only |
| `hasVoted`, `votesCast`, `votesTotal`, `voteTargets` | Voting phase only |
| `lastResult`, `guessDeadlineMs` | Reveal phase: the round result and, while a caught impostor's guess is pending, its deadline |
| `finalScores` | Game-over phase only |

### Redaction rules

- The secret word is never sent to that round's impostor — not in `word`,
  and not via `lastResult.word` either, for as long as that round is the
  most recent one. (An earlier version of this snapshot leaked the word
  through `lastResult` to a round's own impostor if they reconnected or
  refreshed any time after their round ended; `redactedResult` in
  `internal/room/broadcast.go` is the single place this is now enforced,
  shared by both the live announcement and the snapshot.)
- The impostor's identity is never revealed mid-round — only at reveal,
  which is the game's own intentional reveal moment.
- A reconnecting impostor whose guess is still pending never receives the
  word, exactly like a client that never disconnected.
- Votes are never revealed beyond aggregate counts and the viewer's own
  `hasVoted` — who voted for whom is never sent to anyone before the
  reveal's tally.
- `youAreImpostor` and `hasVoted` are always about the viewer only.
- No reconnect token, token hash, or internal connection/timer id is ever
  serialized anywhere on the wire.
- There are no spectators — every connection is a seated player.

## Reconnect lifecycle

See [Join / reconnect protocol](#join--reconnect-protocol) below for how a
connection first authenticates. Once connected, `web/src/roomConnection.js`
(a framework-agnostic controller `useRoomSocket.js` thinly wraps) owns
recovery:

1. **Unexpected close** → status `reconnecting`, retry with backoff:
   immediate, 500ms, 1s, 2s, 4s, then capped at 10s, each with jitter.
2. Every retry — including the very first automatic one — reconnects with
   the seat's own `playerId`/`reconnectToken` once the hook has ever
   learned them, **never** the original name/emoji, even if the original
   connection was a fresh join. A dropped socket never creates a new
   player.
3. An `online` window event fires an immediate retry, bypassing whatever
   backoff delay is currently pending.
4. Retries continue for up to the server's own reconnect-grace default
   (60s) of continuous failure; past that, status becomes `failed` —
   retrying past the point the seat may already be gone would just be
   misleading.
5. A `state_snapshot` arriving resets the backoff counter and clears the
   failure clock — the connection is considered stable again.
6. Fatal rejections (`invalid_reconnect`, `name_taken`, `room_full`,
   `game_started`, `invalid_join`, `unsupported_version`) stop retries
   immediately; `invalid_reconnect` also clears the stored credentials, so
   a subsequent fresh visit doesn't keep trying a dead seat.
7. An intentional stop (unmount, leaving, or switching rooms) closes the
   socket normally and never schedules a retry.
8. No gameplay command is ever queued while disconnected or resyncing —
   `send()` is a no-op in every status except `connected`. Replaying a
   stroke or vote later could produce an invalid or duplicated transition,
   so the UI instead waits for a fresh snapshot and re-enables actions only
   once it's applied.

Connection statuses a component can render around: `connecting`,
`connected`, `reconnecting`, `resyncing`, `failed`, `closed`.

## Heartbeat

The server pings each connection on an interval (`FAUXTIST_HEARTBEAT_INTERVAL_MS`,
default 25s) and, if a pong doesn't arrive within a timeout
(`FAUXTIST_HEARTBEAT_TIMEOUT_MS`, default 10s), closes the socket. That
close unblocks the connection's own read loop with an error, which runs
through the exact same disconnect → presence → reconnect-grace path as any
other drop — heartbeat failure needs no plumbing of its own.

Browsers answer real WebSocket ping frames automatically at the transport
level; there is no application-level heartbeat message, and the frontend
needs no code for this at all.

## Input validation

Beyond the envelope-shape checks in
[Client command types](#client-command-types), the room actor validates
each command's own payload (`internal/room/validate.go`) before ever
calling into the game engine:

| Command | Rules |
|---|---|
| `stroke` | 1–500 points; every coordinate finite and within [-0.5, 1.5] (a small overscan margin past the canvas's normalized [0,1]); width 0.5–20; color in the supported palette |
| `chat_message` | trimmed; non-empty; ≤300 runes |
| `impostor_guess` | trimmed; ≤100 runes (empty is left to the engine to simply score as wrong) |
| `cast_vote` | target must be a player in the current roster (enforced by the game engine itself) |
| `voice_signal` | target must be another currently connected player (never the sender); `kind` must be `offer`/`answer`/`ice`; payload ≤8 KiB |
| new join's `emoji` | empty (defaults to the first palette entry) or exactly one of the app's supported emoji |

A rejected command never mutates state or advances the room's revision;
most send a typed `error` back (see below), matching the same-named
validation failure code (`invalid_stroke`, `invalid_chat`,
`invalid_guess`, `invalid_voice_signal`).

## Rate limiting

Every connection carries five independent token buckets (one per command
category: ordinary control/game commands, `stroke`, `chat_message`, voice
signaling, and `resync`), checked with a non-blocking `Allow()` — a
saturated bucket never stalls the room actor waiting for capacity. A
rejected message never mutates state or advances the revision, same as a
failed validation; the client gets back a typed `rate_limited` error.
Buckets are sized generously above normal play (see
`internal/room/ratelimit.go` for exact numbers) so ordinary gameplay and
WebRTC negotiation never brush against them. A connection that keeps
sending past the limit for more than 20 messages in a row — not a burst,
sustained abuse — is disconnected with `StatusPolicyViolation` (1008)
rather than left to churn rejections forever.

`resync` in particular is capped tightly (a handful of requests, then a
slow trickle) specifically so a client cannot use it to poll the server
continuously.

## ICE configuration

`ice_config_request` (empty payload) asks for the current best-effort
WebRTC ICE server list; the server answers with `ice_config`
(`{"iceServers": [...]}`) directly on the same connection — this never
touches game state, so it isn't a room actor command and doesn't
consume the room's revision. See [docs/turn.md](./turn.md) for how STUN
and TURN are configured, and why this is a WebSocket round trip rather
than a separate HTTP endpoint.

## Error payloads

```json
{
  "version": 1,
  "type": "error",
  "requestId": "8f2c...",
  "payload": { "message": "that name is already taken in this room", "code": "name_taken" }
}
```

`code` is always present and stable; `message` is human-readable but never
a raw internal Go error. Known codes: `invalid_join`, `invalid_reconnect`,
`name_taken`, `room_full`, `game_started`, `room_closed` (join/reconnect
rejections, see below), `unsupported_version`, `invalid_envelope`
(protocol-level rejections), `bad_payload` / `unknown_message_type`
(malformed in-session commands), `rate_limited`, and
`invalid_stroke` / `invalid_chat` / `invalid_guess` / `invalid_voice_signal`
(see [Input validation](#input-validation) and
[Rate limiting](#rate-limiting)).

## Close codes

| Code | Meaning |
|---|---|
| 1000 (`StatusNormalClosure`) | Intentional close — a reconnect replacing this seat's prior connection, or the client leaving |
| 1008 (`StatusPolicyViolation`) | Join/reconnect rejected on business rules, or sustained rate-limit abuse |
| 4001 (`CloseUnsupportedVersion`) | The join frame's protocol version isn't supported |
| 4002 (`CloseInvalidEnvelope`) | The first frame wasn't a valid join envelope at all |
| 4003 (`CloseRoomClosed`) | The room was torn down out from under a still-connected client — it expired from inactivity, or the process is shutting down |

4001/4002/4003 are in the private-use range (RFC 6455 §7.4.2). 4001/4002
always follow a structured `error` frame with a matching `code`; 4003 can
arrive as a bare close frame (there may be no live room actor left to
send an `error` frame from), so the frontend treats these close codes as
fatal directly, not only via a preceding `error` payload's `code`.

## Malformed and mismatched frames mid-session

A frame that fails to parse, or that declares a different protocol
version, arriving after a session is already established is dropped —
never treated as fatal, and never causes a panic. Closing an otherwise-
healthy connection over one bad message would be far more disruptive than
ignoring it; only the *join* frame gets this strict treatment, since at
that point no session exists yet to protect.

## Join / reconnect protocol

Every WebSocket connection to `/ws/room/{code}` starts with exactly one
`join` frame, which is either a **new join** or a **reconnect**:

```json
// new join — a player the room has never seen
{"version": 1, "type": "join", "requestId": "...", "payload": {"name": "Alice", "emoji": "🦊"}}

// reconnect — claiming an existing seat (the host's first connection is
// always a reconnect, since the host seat is created at room-creation time)
{"version": 1, "type": "join", "requestId": "...", "payload": {"playerId": "...", "reconnectToken": "..."}}
```

The server tells the two apart by whether `playerId`/`reconnectToken` are
present; if either is, the whole frame is treated as a reconnect attempt.

### Identity

- `playerId`: 128 bits of `crypto/rand`, base64 URL-safe, no padding. Public,
  sent to every client in room snapshots.
- `reconnectToken`: 256 bits of `crypto/rand`, same encoding. Secret — only
  the server and the seat's owner ever see the raw value.
- The server stores only `sha256(reconnectToken)` and compares candidates in
  constant time. Neither value is derived from the room code or the player's
  name, so knowing the room code (which every invited player has) is not
  enough to guess or claim a seat.

### Server responses

- **New join, accepted:** a private `join_accepted` frame
  (`{playerId, reconnectToken}`), sent only to that connection, followed by
  the usual `state_snapshot`.
- **Reconnect, accepted:** just the `state_snapshot` — the client
  already has its credentials, it's how it reconnected. The existing seat's
  name, emoji, score, host status, and game state are preserved unchanged.
- **Rejected** (either path): a structured `error` frame with a stable
  `code`, then the connection is closed:
  - `invalid_join` — malformed or invalid new-join request
  - `invalid_reconnect` — unknown playerId, or a token that doesn't match
  - `name_taken` — another seat already has that name (case-insensitive)
  - `room_full` — roster already at capacity
  - `game_started` — new joins are lobby-only
  - `room_closed` — the room expired or the process is shutting down in
    the narrow window between `Hub.Get` finding it and this join reaching
    its now-stopped actor; treat the same as "room not found"

### Connection replacement

Reconnecting always wins the seat: any previous live connection for that
`playerId` is closed server-side (`reason: "replaced by reconnect"`) and can
no longer submit actions — every inbound message is tagged with the
connection's id, and the room drops anything tagged with a superseded id.

### Frontend persistence

The frontend stores `{playerId, reconnectToken}` in `sessionStorage`, keyed
by room code (`web/src/credentials.js`), and rewrites the URL to
`/join/<code>` on entry so a page refresh can look the credentials back up
and reconnect automatically instead of prompting for a name again. The
reconnect token is never rendered, logged, or put in an invite link — invite
links only ever carry the room code.
