# Fauxlands Manual Test Plan

Automated tests cover the engine, protocol, privacy, and connection logic.
This plan covers what CI cannot meaningfully verify: real browsers, live
microphones, and multi-device layout. Run it with 2–3 browser windows (or
devices) against a local build or a deployment.

Tip: shorten phases for a fast manual pass by running the server with a small
preset (Quick) and, if driving locally, the standard build — the UI honours
the server's deadlines.

## Setup

1. `go run ./cmd/fauxtist` and `cd web && npm run dev`, open the Vite URL.
   (Or run the embedded production binary and open `:8080`.)

## Core flow

- [ ] **Create with emoji.** Host creates a room, picks a name + avatar. The
      chosen avatar shows on the host's card in the lobby (not the default).
- [ ] **Join by invite.** Copy the invite link; open it in a second window;
      join with a different name/avatar. Both appear in the lobby.
- [ ] **Ready + start.** Both ready up; host selects Quick; Start becomes
      enabled only with 3–6 players all ready; start the match.
- [ ] **Declaration + Faux.** Each player submits a declaration. One player
      marks theirs Faux and submits different hidden orders. At resolution the
      Faux visibly dissolves and the real orders play out; the Faux token shows
      spent afterward.
- [ ] **Hidden orders stay private.** With devtools open on one client, confirm
      no other player's hidden orders or Faux flag appear in received frames
      before `round_resolved`.
- [ ] **Resolution + summary.** The timeline narrates; the board reaches the
      final state; the round summary shows deltas.
- [ ] **Game over + rematch.** Play to the end; winner/standings show; Play
      Again readies players; the host starts a rematch and state fully resets
      (board, energy, influence, Faux tokens, streaks).

## Resilience

- [ ] **Refresh** a mid-match tab — the seat and private draft are restored.
- [ ] **Network loss** (devtools offline, then online) — reconnects with a
      "Reconnecting…" overlay and resyncs.
- [ ] **Close & reopen** the tab (or browser) within two hours — the landing
      page offers "Resume room ABCD" and rejoins.
- [ ] **Leave for now** — returns home, seat preserved, Resume works.
- [ ] **Resign permanently** — confirm dialog; territories go neutral; a fresh
      reload does NOT reconnect (credentials cleared).
- [ ] **Host exit** — the host disconnects; after grace, host migrates and the
      new host can act/start.
- [ ] **Late join** during a match becomes a read-only spectator (no private
      state, cannot post to player chat or use voice).

## Voice (manual — CI cannot validate live audio)

- [ ] **Enable mic** (explicit gesture), speak — a speaking indicator shows on
      your and others' cards.
- [ ] **Mute/unmute** and **push-to-talk** (hold Space, if enabled) work.
- [ ] **Permission denied** shows "Mic unavailable — you can still play" and
      never blocks gameplay; text chat still works.
- [ ] Reconnect re-establishes voice; leaving stops all local tracks.

## Layout & accessibility

- [ ] **Portrait phone (390×844)** — board and bottom panel are usable; targets
      are ≥44px; the map doesn't scroll the page while manipulated.
- [ ] **Keyboard only** — Tab to tiles / the territory list, act without a
      mouse; visible focus throughout.
- [ ] **Reduced motion** (OS or in-game) — animations are minimized; resolution
      still resolves.
- [ ] **Sound** on/off and volume behave; every audio cue has a visible
      equivalent.
- [ ] **No uncaught console errors** during a full session.
