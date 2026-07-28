# Fauxtist — Design Spec

Date: 2026-07-28
Status: Approved for planning

## Summary

A browser-based, real-time multiplayer party game built to learn Go concurrency patterns (goroutines, channels, WebSockets) with a React frontend. Players join a shared room and play a "secret impostor" drawing/bluffing game inspired by the party game *Fake Artist Goes to New York*.

This is a personal side project, unrelated to any current employer codebase, living in its own repo.

## Goals

- Learn idiomatic Go real-time systems design (actor-per-room pattern, WebSocket hub/broadcast, channel-based state ownership).
- Ship something friends can actually play together, reachable via a shared link.
- Keep v1 scope small enough to actually finish: no accounts, no database, no voice chat.

## Non-goals (v1)

- Voice chat (WebRTC) — deferred; the drawing/chat/voting loop must work well on its own first.
- User accounts / persistent stats across sessions — anonymous room codes only.
- Reconnect across server restarts — rooms are in-memory and ephemeral by design.
- Mobile app — responsive web only.

## Game Rules

- 4–8 players per room (minimum enforced: 4).
- Host creates a room and gets a shareable code/link.
- **Round start**: server picks a `{category, word}` pair from a static word bank (e.g. category "Animal", word "Giraffe") and randomly assigns one player as the **Impostor**. Every other player sees the word; the impostor sees only the category.
- **Drawing phase**: fixed turn order. Each player, in turn, adds exactly one stroke (a few seconds) to a single shared canvas, for 2 laps (each player draws twice per round). Everyone watches the canvas build up live.
- **Discussion phase**: text chat where players discuss who seemed like they didn't actually know the word.
- **Voting phase**: every player (including the impostor, to blend in) votes for who they think the impostor is.
- **Reveal**: if the majority correctly identifies the impostor, the impostor gets one guess at the secret word — a correct guess steals the win anyway. Otherwise the impostor wins the round outright.
- **Scoring** (fixed for v1, not configurable):
  - Impostor caught, fails to guess the word: every non-impostor player scores 1 point.
  - Impostor caught, guesses the word correctly: impostor scores 2 points, no one else scores.
  - Impostor evades detection (no majority vote against them): impostor scores 2 points.
- Scores accumulate across rounds; the impostor role rotates each round, never repeating a player until everyone has been impostor once. Match length defaults to one round per player (so everyone is impostor exactly once), host-configurable at room creation.

## Architecture

Single Go binary + React SPA. No database in v1 — all game state is in-memory and ephemeral.

- **Backend (Go)**: one WebSocket endpoint, `/ws/room/:code`. Each room is its own goroutine acting as an **actor**: it owns all game state and is the only thing that mutates it, receiving player actions over a channel and broadcasting resulting events to connected players. This avoids shared-memory locking (no mutex around game state) and follows the standard Go WebSocket "hub" pattern extended with real game logic instead of chat relay.
- **Frontend (React + Vite)**: a single-page app — landing page (create/join room), game screen (canvas, turn indicator, word/category panel, player list, chat, voting UI).
- **Deployment**: the React production build is embedded into the Go binary via `go:embed`, so one container serves both the static frontend and the WebSocket API. No CORS, no second service. Ships as a single Docker image to **Render's free web service tier**. Trade-off accepted: the free tier spins the service down after ~15 minutes of inactivity, adding a ~30s cold start to the first connection of a session — acceptable for a "let's play" party game, not acceptable if this ever needs to feel instant-on (revisit hosting if that changes).

## Components

**Backend**
- `GameEngine` — pure game logic: `(state, action) → (newState, outboundEvents)`. Has no knowledge of networking, which makes it fully unit-testable in isolation. This is the core "game" and the piece to keep cleanest; everything else is plumbing around it.
- `Room` — the actor goroutine wrapping one `GameEngine` instance, the connected player list, and an inbox channel for incoming actions.
- `Hub` / `Registry` — creates rooms, generates unique short room codes, routes incoming WebSocket connections to the correct room's inbox, and sweeps idle/empty rooms after a timeout.
- `WordBank` — static list of `{category, word}` pairs; tracks used words per match to avoid immediate repeats, resets if exhausted.

**Frontend**
- `useRoomSocket` — hook owning the WebSocket connection lifecycle and dispatching incoming server messages into local state.
- `Lobby` — create-room / join-room screens.
- `GameBoard` — shared canvas, turn indicator, per-round timer.
- `PlayerList` — sidebar showing each player's connection/turn/vote status.
- `DiscussionChat` — text chat panel for the discussion phase.
- `VotingPanel` — vote casting UI and round-result reveal.

## Message Protocol

WebSocket messages use a `{type, payload}` JSON envelope.

**Client → server**: `join`, `start_game`, `stroke`, `stroke_end`, `chat_message`, `cast_vote`, `impostor_guess`.

**Server → client**: `room_state` (full snapshot, sent on join/rejoin), `player_joined`, `player_left`, `round_started` (word included only for non-impostors, category only for the impostor), `stroke_broadcast`, `turn_changed`, `phase_changed`, `vote_update`, `round_result`, `game_over`.

## Edge Cases

- **Disconnect/rejoin**: on join, the server issues a per-player reconnect token (stored client-side, e.g. `sessionStorage`). Reconnecting with a valid token within a grace window (60s) restores the player's seat and current state; no valid token means a fresh join.
- **Server-authoritative validation**: `GameEngine` rejects any action that doesn't match the current phase or isn't that player's turn (e.g. drawing when it isn't your turn, voting outside the voting phase). The client is never trusted for game-state correctness.
- **Idle rooms**: swept by the `Hub` after ~10 minutes with zero active connections.
- **Minimum players**: `start_game` is rejected if fewer than 4 players are present.
- **Room capacity**: `join` is rejected once a room already has 8 players.
- **Word exhaustion**: the `WordBank`'s used-word tracker resets automatically if it runs out mid-match.

## Testing

- `GameEngine`: table-driven unit tests covering every phase transition, turn rotation, vote tallying, and the impostor-guess-steals-win case. This is where the bulk of test coverage should live since it's pure logic with no I/O.
- `Room` / `Hub`: integration tests using `net/http/httptest` plus a real WebSocket client to verify multi-client join/broadcast behavior end to end.
- Frontend: a handful of Vitest tests around the WebSocket message reducer/state-dispatch logic.

## Repo Layout

Single repo at `~/Practice Project/fauxtist`:

```
fauxtist/
  server/     # Go module: GameEngine, Room, Hub, WordBank, main.go (embeds web/dist)
  web/        # Vite + React app
  docs/
```

## Cost

- Hosting: **$0/month** on Render's free tier (accepted trade-off: cold start after idle periods).
- No third-party API costs — the word bank is static data, no AI/Spotify/YouTube dependency in this design.
- Optional custom domain later: ~$10–15/year (not required — the free `*.onrender.com` subdomain works fine for sharing a link with friends).

## Future Enhancements (explicitly out of scope for v1)

- Voice chat via WebRTC, once the core loop is proven fun.
- Custom/community word packs.
- Spectator mode.
- Lightweight accounts for cross-session stats, if reconnect-by-identity ever becomes worth the complexity.
