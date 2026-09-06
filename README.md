# Fauxlands

**Every order tells a story. Some are lies.**

Fauxlands is a free, accountless, browser-based 2D multiplayer strategy game
for 3–6 friends. Players negotiate in the open, reveal one apparently binding
order, submit the rest secretly, and resolve everything at once — and once per
match, each player may turn their public declaration into a **Faux Order** and
secretly do something else. It is server-authoritative and deterministic (no
combat dice), and a Standard match runs about 15–20 minutes.

> The visible name and tagline live in one file — `web/src/app/brand.js` — so
> the game can be rebranded without touching anything else. (The GitHub
> repository, Go module, import paths, and git remotes keep the original
> `fauxtist` name for continuity.)

Full rules: **[docs/game-rules.md](docs/game-rules.md)** ·
Protocol: **[docs/protocol.md](docs/protocol.md)** ·
Architecture: **[docs/architecture.md](docs/architecture.md)**

## Rules in a minute

Each round: **Income → Negotiation → Declaration → Reveal → Secret Planning →
Resolution → Summary.** You resolve three real orders per round — March,
Fortify, Recruit, Build Fortress/Mine, or Hold. Control relics for Influence;
hold three of five relics for two round-ends in a row to win by **Domination**,
or lead on Influence after the final round. Your capital can never be captured.
Once per match, make your public declaration a Faux Order — it looks binding,
executes nothing, and lets you do something else instead.

## Local development

Backend and frontend run as two dev processes; Vite proxies `/api` and `/ws`
to the Go server (see `web/vite.config.js`).

```bash
# Terminal 1 — backend, from the repo root
go run ./cmd/fauxtist

# Terminal 2 — frontend, from web/
cd web
npm install
npm run dev
```

Open the URL Vite prints. Copy [.env.example](./.env.example) to `.env` only if
you want to override a default — every setting has one.

## Commands

```bash
# Backend
gofmt -l .            # formatting (should print nothing)
go vet ./...
go test ./...
go test -race ./...

# Frontend, from web/
npm test
npm run lint
npm run build
```

## Docker

The production image builds the frontend, embeds it into a static Go binary via
`go:embed`, and runs it as a non-root user on a distroless base:

```bash
docker build -t fauxlands .
docker run --rm -p 8080:8080 fauxlands
# then: curl -fsS localhost:8080/healthz
```

## Deploy on Render (free tier)

[`render.yaml`](./render.yaml) defines one free Docker web service with a
`/healthz` health check. Render sets `$PORT` automatically. Set
`FAUXTIST_ALLOWED_ORIGINS` (your domain, bare host) in the dashboard once you
have one; set `FAUXTIST_TURN_SHARED_SECRET` only if you run Coturn. No secrets
are committed.

References: [free services](https://render.com/docs/free) ·
[WebSockets](https://render.com/docs/websocket) ·
[custom domains](https://render.com/docs/custom-domains).

**Custom domain:** add it in Render, point DNS as instructed, then set
`FAUXTIST_ALLOWED_ORIGINS` to that bare host (e.g. `play.example.com`). In
production the server refuses to start without an allowlist, and only serves
`wss://` behind Render's TLS.

### Honest free-tier limitations

- Render's free service **spins down after inactivity**; a **cold start can
  take ~1 minute**, and Render may restart the service.
- A restart or redeploy **destroys all active in-memory rooms** (there is no
  database — by design).
- Free-tier bandwidth and build-minute limits apply.
- Only a custom domain costs money; the beta itself needs no paid service.

## Environment variables

Every variable has a default; none are required for local dev.

| Variable | Default | Purpose |
|---|---|---|
| `PORT` | 8080 | HTTP port (Render sets this). |
| `FAUXTIST_ALLOWED_ORIGINS` | local-dev hosts | Comma-separated bare hosts for the WS origin allowlist. Required in production. |
| `FAUXTIST_ENV` | — | `production` enforces the origin allowlist. |
| `FAUXTIST_MAX_ROOMS` | 100 | Max concurrent rooms (measured hobby value). |
| `FAUXTIST_EMPTY_ROOM_TTL_MS` | 900000 | Idle-empty room lifetime before sweeping. |
| `FAUXTIST_ROOM_SWEEP_INTERVAL_MS` | 60000 | Sweeper cadence. |
| `FAUXTIST_RECONNECT_GRACE_MS` | 60000 | Reconnect grace before removal/migration. |
| `FAUXTIST_EARLY_COUNTDOWN_MS` | 3000 | "All locked" countdown. |
| `FAUXTIST_SOLO_WAIT_MS` | 60000 | One-player-left prompt window. |
| `FAUXTIST_HEARTBEAT_INTERVAL_MS` | 25000 | WS ping cadence. |
| `FAUXTIST_HEARTBEAT_TIMEOUT_MS` | 10000 | Dead-connection timeout. |
| `FAUXTIST_TRUSTED_PROXY_HOPS` | 1 on Render, else 0 | Trusted `X-Forwarded-For` hops for the per-IP limiter. |
| `FAUXTIST_STUN_URLS` | Google STUN | STUN servers for WebRTC voice. |
| `FAUXTIST_TURN_URLS` / `FAUXTIST_TURN_SHARED_SECRET` / `FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS` | unset | Optional Coturn TURN relay (see docs/turn.md). |

Per-phase gameplay timing comes from the chosen preset and is not env-tunable.

## Voice, STUN, and TURN

Voice is optional peer-to-peer WebRTC and **never routes audio through the Go
server** (it only relays signaling). It falls back gracefully to text chat if a
mic is denied or unavailable.

- STUN-only is **best effort**: it connects most users, but some restrictive
  networks need a TURN relay.
- Coturn is open source, but reliable TURN hosting and relay bandwidth are
  **not guaranteed free**. The optional Coturn config is preserved
  ([docs/turn.md](docs/turn.md)); the game works fully without it.

We do not claim production-grade uptime or universal voice connectivity for
zero cost.

## Room lifecycle & leaving

- **Reconnect** (dropped connection): your seat and private draft are kept;
  the client reconnects with exponential backoff and resyncs.
- **Leave for now:** return home but keep your seat and credentials; your
  faction auto-Holds on deadlines; the landing page offers Resume (credentials
  live in `localStorage` with a two-hour expiry, never in URLs or logs).
- **Resign & leave permanently:** a confirmed, destructive action — your
  territories go neutral, your capital goes inactive, your reconnect token is
  invalidated, and the host migrates if needed.

If everyone disconnects, the phase clock pauses and resumes when someone
returns; the match never plays itself out in an empty room.

## Security model

Server-authoritative throughout: the client never decides ownership, income,
combat, victory, host, or another player's actions. Secure random player ids
and reconnect tokens (constant-time verification), origin allowlisting, CSP and
security headers, HTTP and per-connection WebSocket rate limiting, a per-IP
room-creation limiter with configurable trusted-proxy handling, message/frame
size bounds, and graceful shutdown are all enforced. Logs never contain tokens,
private orders or Faux selections before reveal, chat text, SDP, or ICE
credentials.

## Accessibility

Keyboard-playable via a synchronized territory list (every board action works
without a pointer), visible focus, semantic HUD/forms, ownership shown by
colour **plus** pattern **plus** sigil (never colour alone), `aria-live`
announcements for phase/timer/connection/lock changes, reduced-motion support
(OS and in-game), a high-contrast option, and a visible equivalent for every
audio cue.

## Non-goals

No bots, accounts, matchmaking, leaderboards, monetization, procedural maps,
map editor, tech trees, asymmetric faction powers, fog of war, naval mechanics,
alliances/teams, persistent replays, native apps, multi-instance deployment, or
database/Redis persistence. See the pivot spec for the full list.

## Third-party notices

Software: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) · Assets & fonts:
[THIRD_PARTY_ASSETS.md](THIRD_PARTY_ASSETS.md) (Fredoka is bundled under the SIL
OFL — [licenses/Fredoka-OFL.txt](licenses/Fredoka-OFL.txt)).

This repository has no root project licence, and none is assumed or added here.
