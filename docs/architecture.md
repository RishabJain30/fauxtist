# Fauxlands Architecture

One Go binary serves a WebSocket API and an embedded React/Vite SPA. No
database, no Redis, no external game-state service — every room lives in memory
in a single server process.

```
Browser (React SPA)
  ├─ app/            App + RoomScreen shell, brand.js
  ├─ features/       landing, lobby, game (hex board + panels), chat, voice, settings
  ├─ connection      protocol.js, sequencing.js, roomConnection.js, useRoomSocket.js
  └─ state           reducer.js, credentials.js
        │  WebSocket (protocol v2)  +  POST /api/rooms
        ▼
Go server (one process, one binary)
  ├─ internal/server   HTTP + WS handlers, origin/headers/heartbeat/turn, room-create limit
  ├─ internal/hub      room registry, idle sweeper, capacity cap
  ├─ internal/room     ONE actor goroutine per room: phase driver, presence, snapshots, broadcast
  ├─ internal/game     pure engine: state, maps, orders, validation, resolution, victory
  ├─ internal/identity secure player ids + reconnect tokens (constant-time verify)
  ├─ internal/wsproto  envelope + typed payloads + validation
  └─ internal/webui    go:embed of web/dist
```

## Server-authoritative actor model

Each room is a single goroutine (`internal/room`). It is the **sole** mutator
of the engine, presence, and timers; every other goroutine (HTTP handlers,
timer callbacks) communicates through channels. This makes the whole room
race-free by construction — the same property the previous drawing game relied
on, preserved here.

- **Engine** (`internal/game`) is pure and networking-independent: it has no
  socket, no timer, no wall clock. It validates every command, resolves a whole
  round atomically from an immutable planning-start snapshot (no dice, no
  dependence on map/message ordering — everything iterates sorted ids), and
  exposes only copy-safe state snapshots.
- **Room** drives the phase machine on server-authoritative absolute deadlines,
  with a generation guard that ignores stale timer callbacks, a pause when no
  active player is connected (so a match never plays itself out empty), an
  early-lock countdown, and per-viewer snapshot redaction.
- **Hub** registers rooms, caps their count, and sweeps idle empty rooms.

## Frontend

- `roomConnection.js` owns one room's socket end-to-end (connect, backoff,
  sequencing, credential reuse, resync) and is framework-agnostic;
  `useRoomSocket.js` is a thin React wrapper. `sequencing.js` decides
  apply/gap/duplicate per envelope.
- The reducer applies a `state_snapshot` as a full atomic replace and never
  mixes two snapshots' worth of state.
- The hex board is inline SVG (honeycomb-grid for geometry only — the server
  owns adjacency and every rule), inside a zoom/pan viewport. A synchronized
  territory list mirrors the board for pointer-free play.
- Credentials live in `localStorage` with a bounded expiry so "resume room"
  survives a full browser close; reconnect tokens are never placed in URLs,
  logged, or rendered.

## Determinism & privacy invariants (tested)

- Combat/resolution is deterministic and order-independent
  (`internal/game/resolution_test.go`, `engine_test.go`, `maps_test.go`,
  `victory_test.go`, `orders_test.go`).
- Snapshots never leak another player's declaration, hidden orders, or Faux
  before resolution (`internal/server/privacy_test.go`).
- Public sequenced events are gap-free and strictly increasing
  (`internal/server/flow_test.go`, `web/src/sequencing.test.js`).

## Deployment shape

`Dockerfile` builds the SPA, embeds it into a static Go binary, and runs it on
a distroless non-root base with `/healthz` and `/readyz`. `render.yaml`
deploys it as one free Render web service. See the README for details and
honest free-tier limitations.
