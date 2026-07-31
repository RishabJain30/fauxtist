# Invite Link + Voice Chat — Design Spec

Date: 2026-07-31
Status: Approved for planning

## Summary

Two new features for Fauxtist, delivered in order:

- **Part A — Invite link:** a shareable `/join/<CODE>` deep link plus a "Copy invite link" button in the lobby, so friends can join a room in one click. Frontend-only; fits the existing anonymous, no-accounts design.
- **Part B — Voice chat:** in-room voice using a WebRTC full mesh (peer-to-peer audio), with the existing Go WebSocket acting purely as the signaling relay. Join-muted with a mute/unmute toggle and a speaking indicator.

A persistent accounts + friends-list system is explicitly **out of scope** and noted as a separate future project.

## Goals

- Let a host share one link that drops a friend directly onto the join screen for their room.
- Let players in a room talk over voice, with audio flowing browser-to-browser (no media server, no infra cost).
- Keep voice entirely separate from game logic — the game engine never sees a voice message.

## Non-goals

- Accounts, login, friend requests, persistent friends list, online status. (Future project.)
- A media server (SFU) or third-party voice SDK.
- TURN relay for strict/symmetric NATs (documented limitation; free TURN is not realistically available).
- Push-to-talk (chose join-muted + toggle instead).
- Phase-gated voice (voice is available the whole time a player is in the room).

---

## Part A — Invite Link

### Behavior
- **Deep link:** visiting `/join/<CODE>` opens the app on the join screen with the room code pre-filled; the visitor only enters a name. The SPA fallback (`spaHandler`) already serves `index.html` for unknown paths in prod, and Vite serves it in dev — so no backend change is needed.
- **Copy button:** the lobby shows a "Copy invite link" button that writes `${location.origin}/join/${code}` to the clipboard via `navigator.clipboard.writeText`, with a "Copied!" confirmation. If the Clipboard API is unavailable, fall back to selecting the text in a read-only input.

### Frontend changes
- `App.jsx`: on first render, parse `location.pathname`. If it matches `/join/<CODE>`, initialize into "join mode" with the code pre-filled (skip the create/landing choice, show the name entry).
- `Landing.jsx` (or a small `JoinByCode` variant): accept an optional pre-filled code and hide the "Create room" path when arriving via an invite link.
- `Lobby.jsx`: add the "Copy invite link" button + copied confirmation.

### Edge cases
- Unknown/expired code in the link → the normal join flow surfaces the room-not-found error (the server returns 404 on the WS upgrade for a missing room; the client shows a clear "room not found" message).
- Malformed path → fall through to the normal landing screen.

---

## Part B — Voice Chat (WebRTC Mesh)

### Architecture
- **Full mesh:** each browser holds one `RTCPeerConnection` to every other voice participant. Audio is peer-to-peer; it never touches the server. Appropriate for the 4–8 player room size.
- **Signaling relay:** the Go room forwards signaling messages between specific peers over the existing WebSocket. Voice messages are handled in `room.handle` and routed via `sendTo`/`broadcast`, and **never call the game engine** — voice is a parallel concern.
- **STUN:** `stun:stun.l.google.com:19302` (free). No TURN in v1.

### Signaling protocol (new WS message types)
Client → server:
- `voice_join` — "I am enabling voice / present."
- `voice_leave` — "I am disabling voice."
- `voice_signal { to, kind, payload }` — `kind` ∈ `offer | answer | ice`; server forwards to `to`.
- `voice_state { muted, speaking }` — my current mic flags.

Server → client:
- `voice_peers { ids }` — current voice participants (sent to a client right after its `voice_join`, excluding itself).
- `voice_peer_joined { id }` / `voice_peer_left { id }` — presence changes.
- `voice_signal { from, kind, payload }` — a relayed signal.
- `voice_state { id, muted, speaking }` — a relayed mic-state update.

### Room state (server)
- The `Room` tracks a `voicePresent` set of player ids alongside `clients`.
- `voice_join`: add to set → reply `voice_peers` (set minus self) → broadcast `voice_peer_joined`.
- `voice_leave` **and** client disconnect: remove from set → broadcast `voice_peer_left`.
- `voice_signal`: forward to the `to` client with `from` filled in.
- `voice_state`: broadcast to all as `{id, muted, speaking}`.

### Glare avoidance
Deterministic offerer: for any pair, the peer whose player-id is lexicographically smaller creates the offer; the other waits for it. On receiving `voice_peers`, a joining client offers to each existing peer with a larger id and waits on the rest. On `voice_peer_joined`, existing clients offer to the newcomer only if their own id is smaller.

### Frontend
- **`useVoice` hook** owns all machinery:
  - `getUserMedia({ audio: true })` on first unmute; cache the local stream.
  - An `RTCPeerConnection` per peer id; add the local audio track; on `ontrack`, attach the remote stream to a hidden `<audio autoplay>` element.
  - Consume `voice_peers`, `voice_peer_joined`, `voice_peer_left`, `voice_signal` (via a raw-message subscription from the socket hook) and drive the offer/answer/ICE exchange, sending `voice_signal` back through `send`.
  - Mute toggle flips the local audio track's `enabled` (keeps the peer connection up); broadcasts `voice_state`.
  - Speaking indicator: a WebAudio `AnalyserNode` on the local stream; when smoothed volume crosses a threshold, broadcast `voice_state { speaking }` (throttled, only on change).
  - Cleanup: on `voice_peer_left` / unmount, close that `RTCPeerConnection` and remove its audio element.
- **Socket hook (`useRoomSocket`)**: add a lightweight raw-message subscription (`subscribe(fn)`) so `useVoice` can receive `voice_*` messages while the reducer keeps handling game messages. `send` is shared.
- **Reducer**: track display-only voice state — `voicePeers` (set of ids present in voice) and a per-player `{muted, speaking}` map — updated from `voice_peer_joined/left` and `voice_state`. Game screens read this to show 🔊/🔇/speaking icons.
- **`VoiceBar` component**: the Unmute/Mute button (requests permission on first use) and a compact per-player mic-status row. Rendered whenever a player is in a room (lobby through end).

### Edge cases
- **Permission denied / no mic:** catch the `getUserMedia` rejection, stay muted, show "Mic unavailable — you can still play". Voice failure never blocks the game.
- **Peer behind strict NAT:** the connection may never reach `connected`; document that unreachable audio is usually the peer's network (no TURN in v1). Surface a subtle "connecting…"/"couldn't connect" per-peer state; do not block anything.
- **Disconnect:** the room removes the player from `voicePresent` and broadcasts `voice_peer_left`; every client tears down that peer connection.
- **Autoplay policy:** remote `<audio>` uses `autoplay`; since the user has clicked Unmute (a gesture), playback is allowed. If a remote arrives before any gesture, resume the `AudioContext`/play on the next click.

---

## Testing

- **Go:** integration test that a `voice_signal` from client A addressed to client B is delivered only to B (with `from` set), and that `voice_join` yields a `voice_peers`/`voice_peer_joined` broadcast — all without any game-state change. Reuse the `httptest` + WS-client harness.
- **Frontend:** reducer unit tests for the voice display-state transitions (`voice_peer_joined/left`, `voice_state`).
- **Manual (browser):** the live WebRTC handshake, mic permission, mute toggle, and speaking indicator are validated across multiple browser windows, as with the earlier gameplay validation (real media can't be exercised headlessly).

## Delivery Order

1. **Plan 4A — Invite link:** deep link + copy button. Small, frontend-only, shippable on its own.
2. **Plan 4B — Voice chat:** signaling backend → `useVoice` hook + socket subscription → reducer display state → `VoiceBar` UI.

## Known Limitations (carried forward)

- No TURN → some strict-NAT players may be inaudible.
- Mesh cost grows with room size; fine at 4–8, not intended beyond that.
- Voice available during drawing could leak bluffing cues; left to players to self-regulate (per design choice).
