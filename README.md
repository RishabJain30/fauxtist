# Fauxtist

A browser-based, real-time multiplayer party game: a "secret impostor"
drawing/bluffing game inspired by *Fake Artist Goes to New York*. Players
join a shared room by code, draw on one shared canvas in turn, discuss,
and vote on who the impostor was.

Single Go binary + a React SPA embedded into it — one process, one
container, no database, no accounts. Rooms are in-memory and ephemeral by
design: they disappear on their own once abandoned, and a restart or
redeploy clears every active game. See
[Single-instance limitations](#single-instance-limitations).

## Game rules

- 4–8 players per room.
- The host creates a room and shares its 4-character code.
- **Round start**: the server picks a `{category, word}` pair and
  randomly assigns one player as the **Impostor**. Everyone else sees the
  word; the impostor sees only the category.
- **Drawing**: fixed turn order, two laps — each player adds one stroke
  to the shared canvas per turn, twice per round.
- **Discussion**: a text chat panel to argue over who didn't seem to
  actually know the word.
- **Voting**: everyone (including the impostor, to blend in) votes for
  who they think the impostor is.
- **Reveal**: if caught, the impostor gets one guess at the secret word —
  guessing right steals the round back. If not caught, the impostor wins
  the round outright.
- **Scoring**: caught + wrong guess → every non-impostor scores 1.
  Caught + right guess → impostor scores 2, no one else does. Not caught
  → impostor scores 2. The impostor role rotates every round until
  everyone has played it once (that's the match length, fixed to the
  player count).

## Architecture

- **Backend (Go)** — one WebSocket endpoint, `/ws/room/{code}`. Each room
  is its own goroutine acting as an actor: it owns all game state and is
  the only thing that ever mutates it, receiving actions over channels
  and broadcasting the resulting events — no mutex around game state.
  - `internal/game` — pure game logic (`state, action → newState, events`),
    no networking knowledge, fully unit-testable in isolation.
  - `internal/room` — the actor: one goroutine per room, wrapping the
    engine, connected clients, presence/reconnect-grace tracking, input
    validation, and per-connection rate limiting.
  - `internal/hub` — creates rooms, mints unique room codes, and sweeps
    idle/empty rooms (see [Room lifecycle](#room-lifecycle)).
  - `internal/server` — HTTP/WebSocket routes, the join/reconnect
    handshake, heartbeat, security headers, and TURN credential minting.
  - `internal/wsproto` — the versioned wire envelope and payload types,
    shared by every layer above.
- **Frontend (React + Vite)** — a single-page app: landing (create/join),
  the game screen (canvas, turn indicator, chat, voting, voice), and a
  framework-agnostic connection controller (`web/src/roomConnection.js`)
  handling reconnect/backoff/sequencing independently of React.
- **Protocol** — see [docs/protocol.md](./docs/protocol.md) for the full
  wire format: envelope, revisioning/sequencing, the canonical state
  snapshot, redaction rules, reconnect lifecycle, heartbeat, error codes,
  and close codes.

## Prerequisites

- Go (version in [go.mod](./go.mod))
- Node.js 22+ and npm
- Docker (only needed for the container build/run path)

## Local development

Run the backend and frontend as two separate dev processes; Vite proxies
`/api` and `/ws` to the Go server (see `web/vite.config.js`).

```bash
# Terminal 1 — backend, from the repo root
go run ./cmd/fauxtist

# Terminal 2 — frontend, from web/
cd web
npm install
npm run dev
```

Open the URL Vite prints (typically `http://localhost:5173`). Copy
[.env.example](./.env.example) to `.env` if you want to override any
default — every setting has one, so this is optional for local dev.

### Running tests

```bash
# Backend
go vet ./...
go test ./...
go test -race ./...

# Frontend, from web/
npm test
npm run lint
npm run build
```

### Docker build/run

The production Dockerfile builds the frontend, embeds it into the Go
binary via `go:embed`, and runs it as a non-root user on a minimal
distroless base:

```bash
docker build -t fauxtist:local .
docker run --rm -p 8080:8080 fauxtist:local
# http://localhost:8080
```

## Environment variables

Every variable has a working default; none are required for local
development. See [.env.example](./.env.example) for a copyable template.

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | `8080` | HTTP/WebSocket listen port |
| `FAUXTIST_EMPTY_ROOM_TTL_MS` | `900000` (15 min) | How long an empty room may sit idle before the sweeper removes it |
| `FAUXTIST_ROOM_SWEEP_INTERVAL_MS` | `60000` (1 min) | How often the sweeper checks |
| `FAUXTIST_MAX_ROOMS` | `500` | Maximum simultaneously registered rooms; `POST /api/rooms` returns `503 capacity_reached` past this |
| `FAUXTIST_REVEAL_MS` | `6000` | How long a round's result holds on screen before advancing |
| `FAUXTIST_RECONNECT_GRACE_MS` | `60000` | How long a disconnected seat is preserved before lobby removal / host-migration eligibility |
| `FAUXTIST_DISCONNECTED_TURN_MS` | `10000` | How long the room waits for a disconnected current drawer before skipping their turn |
| `FAUXTIST_IMPOSTOR_GUESS_MS` | `30000` | Deadline for a caught impostor to guess before it's resolved as wrong |
| `FAUXTIST_HEARTBEAT_INTERVAL_MS` | `25000` | Server ping interval per connection |
| `FAUXTIST_HEARTBEAT_TIMEOUT_MS` | `10000` | How long a pong may take before the connection is considered dead |
| `FAUXTIST_ALLOWED_ORIGINS` | *(unset)* | Comma-separated bare hosts (no scheme/path) allowed to open cross-origin WebSockets. **Required in production** — see [Security model](#security-model) |
| `FAUXTIST_ENV` | *(unset)* | Set to `production` to mark a non-Render deployment as production (Render sets this automatically via `RENDER_EXTERNAL_HOSTNAME`) |
| `RENDER_EXTERNAL_HOSTNAME` | *(set by Render)* | Automatically added to the allowed-origins list, and marks the process as production |
| `ALLOWED_ORIGIN` | *(unset)* | Legacy single-origin variable; prefer `FAUXTIST_ALLOWED_ORIGINS` |
| `FAUXTIST_STUN_URLS` | `stun:stun.l.google.com:19302` | Comma-separated STUN server URLs for voice |
| `FAUXTIST_TURN_URLS` | *(unset)* | Comma-separated TURN server URLs (see [docs/turn.md](./docs/turn.md)) |
| `FAUXTIST_TURN_SHARED_SECRET` | *(unset)* | Coturn static-auth-secret; TURN entries are only ever returned if this and `FAUXTIST_TURN_URLS` are both set |
| `FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS` | `3600` | How long a minted TURN credential is valid |

## Security model

- **Identity**: a player's `playerId` and `reconnectToken` are both 128/256
  bits of `crypto/rand`, independent of the room code or display name.
  Only `sha256(reconnectToken)` is ever stored server-side, compared in
  constant time. See [docs/protocol.md](./docs/protocol.md#join--reconnect-protocol).
- **Room codes** are drawn from `crypto/rand` (not `math/rand`), with
  bounded collision retries.
- **Input validation** happens at every boundary: HTTP body size/
  content-type/shape for `POST /api/rooms`, WebSocket frame size and
  envelope shape before a message ever reaches a room actor, and
  per-command-type semantic limits (stroke bounds, chat/guess length,
  voice-signal target/kind/size) — see
  [docs/protocol.md](./docs/protocol.md#input-validation).
- **Rate limiting**: five independent per-connection token buckets
  (control commands, strokes, chat, voice signaling, resync) reject
  floods without ever mutating room state; sustained abuse gets the
  connection disconnected. See
  [docs/protocol.md](./docs/protocol.md#rate-limiting).
- **WebSocket origins** are allowlisted, not wildcarded — production
  refuses to start without at least one explicitly configured origin
  (`FAUXTIST_ALLOWED_ORIGINS` or Render's automatic hostname). A bare `*`
  origin pattern is exactly the cross-site WebSocket hijacking footgun
  the underlying library's own docs warn against.
- **HTTP responses** carry baseline security headers (CSP,
  `X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`, and
  HSTS whenever the request is known to have arrived over HTTPS); nothing
  credential-bearing is ever cached (`Cache-Control: no-store`).
- **Logs** never contain reconnect tokens, full payloads, SDP/ICE
  credentials, secret words, votes, or a URL query string with
  credentials in it — nothing sensitive is ever placed in a URL query
  string in the first place.
- **TURN credentials** are Coturn REST-API-style time-limited HMAC pairs;
  the shared secret itself never reaches a client or a log line. See
  [docs/turn.md](./docs/turn.md).

## Room lifecycle

A room is created by `POST /api/rooms` and lives as its own goroutine
until it's explicitly shut down or the sweeper reaps it:

- **Activity** is tracked per room: creation, every join/reconnect, every
  disconnect, and every accepted command reset its idle clock.
- **Expiry**: a background sweeper (`FAUXTIST_ROOM_SWEEP_INTERVAL_MS`)
  checks every room; one with zero currently connected players *and*
  idle past `FAUXTIST_EMPTY_ROOM_TTL_MS` is shut down and removed. A room
  with even one connected player is never expired, no matter how idle the
  game itself is. The check and the removal happen atomically on the
  room's own goroutine, so a reconnect racing the sweeper always wins.
- **Capacity**: at most `FAUXTIST_MAX_ROOMS` rooms may exist at once;
  `POST /api/rooms` past that returns `503` with
  `{"code": "capacity_reached"}`.
- **Room-creation rate limiting**: `POST /api/rooms` is rate-limited per
  client address (a small burst, then one every 10s) plus one modest
  global bucket, so a single script can't create enough rooms to exhaust
  `FAUXTIST_MAX_ROOMS` and lock out real players. A rejected request
  returns `429` with `{"code": "rate_limited"}` and creates no room.
- **Shutdown**: an expired or explicitly closed room cancels its timers
  and closes every connected client with close code `4003`
  (`room_closed` — see [docs/protocol.md](./docs/protocol.md#close-codes));
  the frontend treats this as fatal and does not attempt to reconnect.
- **Process shutdown**: `SIGINT`/`SIGTERM` trigger a graceful HTTP
  shutdown (in-flight plain HTTP requests get up to 10s to finish, and
  `/readyz` starts failing immediately so a load balancer stops routing
  new traffic here), followed by closing every room and every one of
  their WebSocket connections.

This process holds every room in memory only — restarting or redeploying
it ends every active game. See
[Single-instance limitations](#single-instance-limitations).

## Health and readiness

- `GET /healthz` — lightweight liveness: always `200` once the process is
  up. No dependency checks (this app has no external dependencies to
  check).
- `GET /readyz` — `200` once routes are wired up, `503` once graceful
  shutdown has begun. Point a load balancer's readiness probe here, not
  at `/healthz`, if you want it to stop routing traffic during shutdown.

## Voice / TURN setup

Voice chat is peer-to-peer WebRTC. STUN alone (the default) works for
most players; some networks need a TURN relay. Fauxtist never runs a
TURN server itself — it hands clients short-lived credentials for a
[Coturn](https://github.com/coturn/coturn) deployment you run separately.
See [docs/turn.md](./docs/turn.md) for the full setup and the credential
mechanics, and for why this is a WebSocket request/response rather than
an HTTP endpoint.

Without any TURN configuration, voice simply stays STUN-only —
best-effort, never blocking, never an error state.

## Deployment to Render

[render.yaml](./render.yaml) defines a single Docker web service.

1. Push this repo to GitHub and create a Render Blueprint (or web
   service) from it — `render.yaml` is picked up automatically.
2. Deploy. Confirm `GET /healthz` and `GET /readyz` both return `200`.
3. In Render's dashboard, add your custom domain to the service.
4. Add the DNS records Render's dashboard shows you (typically a CNAME).
5. Wait for Render to issue the TLS certificate for that domain.
6. Set `FAUXTIST_ALLOWED_ORIGINS` to your final custom domain (bare host,
   e.g. `play.example.com` — no `https://`). Without this, the process
   still runs, but production origin validation fails closed at startup
   if `FAUXTIST_ALLOWED_ORIGINS` is unset and only the (untrusted-for-
   this-purpose) Render-provided hostname would otherwise apply — set it
   explicitly once you have a real domain.
7. Test: load the site over HTTPS, confirm the WebSocket connects over
   `wss://`, refresh mid-game and confirm it reconnects, and grant
   microphone access to confirm voice chat's permission prompt appears.

Render's **free plan** is suitable for demos — it's fine for a friend
group to spin up a game on demand. It also spins the service down after
inactivity, meaning the first connection after a while has a cold-start
delay and any rooms that existed are gone. For reliable public play with
long-lived sessions, use an **always-on** plan instead — the free plan's
own inactivity-driven restarts are a second source of lost rooms beyond
this app's own in-memory design.

## Single-instance limitations

Fauxtist is intentionally a single-instance, in-memory, ephemeral party
game — not a persistent platform. By design, it does **not** have:

- Redis, a database, or any persistence — a process restart or
  redeploy removes every active room.
- Multi-instance coordination — running more than one replica means
  players can land on different instances and never find each other's
  room.
- User accounts, match history, or matchmaking.
- Spectators.
- A TURN relay of its own (see [Voice / TURN setup](#voice--turn-setup)).

## Troubleshooting

- **WebSocket won't connect in production**: check `FAUXTIST_ALLOWED_ORIGINS`
  is set to your actual domain (bare host, no scheme) — production
  refuses to start at all with no valid origin configured. Check the
  startup log line for the resolved value.
- **"the server is at capacity" on room creation**: `FAUXTIST_MAX_ROOMS`
  reached; raise it or wait for idle rooms to be swept.
- **A room disappeared unexpectedly**: either it was idle and empty past
  `FAUXTIST_EMPTY_ROOM_TTL_MS`, or the process restarted/redeployed — see
  [Single-instance limitations](#single-instance-limitations).
- **Voice connects for some players but not others**: likely a
  restrictive NAT with no TURN configured — see
  [docs/turn.md](./docs/turn.md).
- **Frontend shows a bare "Build the frontend for the full UI" page**:
  `internal/webui/dist` (what `go build` embeds) wasn't populated from
  `web/dist` (what `npm run build` produces) — the Dockerfile does this
  copy automatically; a plain `go build` outside Docker does not, by
  design, since local development normally runs `go run` and `npm run dev`
  as two separate processes instead.
