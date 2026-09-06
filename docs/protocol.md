# Fauxlands WebSocket Protocol (v2)

Protocol **version 2**. It is intentionally incompatible with the previous
drawing-game protocol (v1, archived under `docs/archive/drawing-game/`): a v1
client is rejected cleanly at the join frame.

## Envelope

Every message in both directions is one JSON object:

```json
{ "version": 2, "type": "<type>", "roomId": "ABCD", "seq": 42, "requestId": "…", "payload": { } }
```

- `version` — must be 2. A mismatch is rejected (see close codes).
- `type` — the message type (see below).
- `roomId`, `seq` — stamped by the server on outbound messages. `seq` is the
  room's authoritative revision (see Sequencing).
- `requestId` — set by the client on every command; echoed on any error for
  that command, for correlation. Required and bounded server-side.
- `payload` — type-specific object.

## Connecting

1. Client opens `wss://<host>/ws/room/{code}`.
2. The **first** frame must be a `join`:
   - new player: `{ "name": "Robin", "emoji": "🦊" }`
   - reconnect: `{ "playerId": "…", "reconnectToken": "…" }`
   - spectator: `{ "asSpectator": true, "name": "…" }` (also implied when a new
     player joins after the match has started)
3. Server replies with `join_accepted` (new player / seat claim only), carrying
   `{ playerId, reconnectToken, spectator }`, then a full `state_snapshot`.

Join business-rule failures (`name_taken`, `room_full`, `spectators_full`,
`game_started`, `invalid_reconnect`, `room_closed`) arrive as an `error` frame +
a 1008 close. Transport/protocol failures use dedicated close codes:

| Close code | Meaning |
|---|---|
| 4001 | Unsupported protocol version |
| 4002 | Invalid join envelope |
| 4003 | Room closed (idle-expired or shutting down) |

## Sequencing

Public, snapshot-reconstructible state changes are **sequenced**: the server
bumps the room revision exactly once per distinct message and stamps it as
`seq`, so every recipient of one transition observes the same number even
though redacted payloads differ. The client applies them strictly in order and
requests a `resync` on a gap.

**Sequenced** (`SEQUENCED_TYPES`): `state_snapshot`, `lobby_update`,
`settings_changed`, `phase_changed`, `declaration_status`,
`declarations_revealed`, `planning_status`, `round_resolved`, `round_summary`,
`player_presence_changed`, `player_afk_changed`, `player_exited`,
`host_changed`, `spectator_update`, `rematch_status`, `game_over`.

**Unsequenced** (never gate ordering on these): `orders_saved` (private ack),
`error`, `chat_broadcast`, `map_ping`, `proposal_arrow`, `join_accepted`,
`leave_accepted`, `ice_config`, and all `voice_*` messages. A private draft
change (`set_orders`) therefore never bumps the public revision — it only ever
produces a private, unsequenced `orders_saved`.

`round_resolved` is one compact message carrying the whole ordered animation
timeline plus the final board — never dozens of per-step frames.

## Client → server

`set_ready`, `update_settings`, `start_match`, `submit_declaration`,
`set_orders`, `lock_orders`, `unlock_orders`, `map_ping`, `proposal_arrow`,
`chat_message`, `leave_for_now`, `resign_match`, `end_no_contest`,
`keep_waiting`, `rematch_ready`, `start_rematch`, `return_to_lobby`,
`claim_seat`, `remove_player` (host), `resync`, `voice_join`, `voice_leave`,
`voice_signal`, `voice_state`, `ice_config_request`.

Key payloads: `submit_declaration {command:{type,from,to,armies}}`,
`set_orders {commands:[…], faux:bool}`, `update_settings {preset}`,
`set_ready {ready}`, `map_ping {tile}`, `proposal_arrow {from,to}`,
`chat_message {text}`, `claim_seat {name,emoji}`, `remove_player {playerId}`.

## Server → client

`state_snapshot`, `join_accepted`, `lobby_update`, `settings_changed`,
`phase_changed`, `declaration_status`, `declarations_revealed`, `orders_saved`,
`planning_status`, `round_resolved`, `round_summary`,
`player_presence_changed`, `player_afk_changed`, `player_exited`,
`host_changed`, `spectator_update`, `rematch_status`, `game_over`,
`leave_accepted`, `chat_broadcast`, `map_ping`, `proposal_arrow`, `error`,
`voice_peers`, `voice_peer_joined`, `voice_peer_left`, `voice_state`,
`ice_config`.

## Snapshot and privacy

A `state_snapshot` is per-viewer and carries: phase + absolute
`phaseDeadlineMs` (+ `earlyDeadlineMs`, `paused`), round/totalRounds/preset,
the public board, public player views, spectator views, hostId, viewer role,
the **viewer's own** private declaration and orders, aggregate opponent
completion/lock counts only, publicly revealed declarations (command only —
never the Faux flag) from the reveal onward, the last resolution/summary,
game-over standings, bounded recent chat, and rematch readiness.

The server **never** sends another player's unrevealed declaration, hidden
orders, Faux selection, or reserved energy. Faux status is public only after
resolution. These invariants are covered by `internal/server/privacy_test.go`.
