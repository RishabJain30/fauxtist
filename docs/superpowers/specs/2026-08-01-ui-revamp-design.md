# UI Revamp — "Playful Pop" Design Spec

Date: 2026-08-01
Status: Approved for planning

## Summary

Replace Fauxtist's plain dark UI with a cohesive, playful "party game" look ("Playful Pop"): cream paper background, bold rounded typography, chunky outlined cards and push-down buttons, saturated accent colors, player-chosen emoji avatars, and tasteful motion (including a confetti moment for the winner). Applied across all eight screens.

The change is primarily presentational. The one functional addition is **player-chosen emoji avatars**, which requires a small additive data field (`emoji`) on the player, threaded through the join flow. Game rules, reducer logic, voice/WebRTC behavior, and the message protocol are otherwise unchanged.

## Goals

- Make the app look and feel like a fun party game, not a plain form.
- Keep it self-contained and free (self-hosted font, one tiny MIT animation lib).
- Change look without touching game logic — the working game stays working.

## Non-goals

- No new game mechanics, phases, or rules.
- No theme switcher / multiple themes — Playful Pop replaces the current styling.
- No redesign of the WebRTC/voice signaling or the game engine.
- No accounts (emoji is ephemeral, per-session, like the nickname).

## Design System

Centralized in `web/src/styles.css` as CSS custom properties + shared classes, so all screens draw from one source.

- **Palette**: `--paper:#fff7ef` (bg), `--ink:#2b2340` (text/outlines), accents `--coral:#ff6b6b`, `--violet:#7c5cff`, `--teal:#22c1a4`, `--amber:#ffc93c`, `--sky:#4aa8ff`. Muted text `#8a7fa6`.
- **Typography**: **Fredoka** (weights 400–700), **self-hosted** — woff2 files in `web/public/fonts/` with an `@font-face` in `styles.css`, so the font is embedded in the Go binary (`go:embed`) and needs no third-party request. Fallback: `system-ui, sans-serif`.
- **Primitives (shared classes)**:
  - `.card` — paper fill, 3px `--ink` border, ~24px radius, offset drop-shadow (`8px 8px 0 rgba(...)`).
  - `.btn` variants (`primary` violet, `mic` teal, `ghost` white) — 3px border, rounded, press-down on `:active`, slight lift on `:hover`.
  - `.pill` / `.badge` — rounded, outlined, small shadow.
  - `.roomcode` — large, ink chip with amber text, slight rotation.
  - `.avatar` — rounded square/circle holding the player emoji, colored fill.

## Emoji Avatars (players pick)

- **Picker**: the join screen shows a grid of 12 curated animal emoji (🦊 🐙 🐸 🦉 🐨 🦁 🐵 🦄 🐼 🐧 🦔 🐝). The player selects one (defaulting to the first) alongside entering their name.
- **Data flow (additive)**:
  - `wsproto.JoinPayload` gains `Emoji string`.
  - Client sends `{ name, emoji, reconnectToken? }` on join.
  - Server `readJoin` parses `emoji`; the `room.Client` carries it; `Room.Join` passes it to `engine.UpsertPlayer(Player{ID, Name, Emoji})`.
  - `game.Player` gains `Emoji string` (JSON `emoji`), so it flows out via `room_state`, `lobby_update`, `round_result`, and `game_over` with no reducer changes (players are stored as-is).
- **Rendering**: lobby player list, voice-bar pills, and (optionally) the turn indicator show the player's emoji avatar. `meId`-derived fallback if a player somehow has no emoji: a default 🎭.
- **Frontend helper**: `EMOJIS` list + a tiny pure module so the picker and any default logic are unit-testable.

## Per-Screen Application

All eight components restyled with the shared system:

1. **Landing** — logo lockup + tagline; name input; emoji picker grid; Create/Join buttons. Invite mode (`initialCode`) still hides Create and pre-fills the code.
2. **Lobby** — big `.roomcode` chip; Copy-invite button (keeps "Copied!" confirm); player rows with avatar + name + 👑 host; chunky Start button (disabled state styled).
3. **VoiceBar** — mic enable/mute button; per-player pills with avatar + name, a **pulsing ring when speaking**, dimmed/🔇 when muted.
4. **GameBoard + Canvas** — the canvas sits in a framed "board"; word-or-category tag (impostor sees category), turn indicator ("✏️ Sam is drawing…"), round·lap header. Canvas drawing behavior unchanged.
5. **Chat** — styled message list + input; "Start voting" host button styled.
6. **Voting** — player vote buttons as avatar cards; tally counter.
7. **Reveal** — playful caught/escaped headline treatment; impostor guess input; word reveal.
8. **GameOver** — winner highlighted (top of a simple scoreboard) with a **🎉 confetti burst** on mount; Play-again button.

## Motion

- Buttons: press-down on `:active`, subtle lift on `:hover` (CSS only).
- Cards: gentle ease/scale-in on phase change (CSS keyframe on mount).
- Speaking: pulsing ring on the active speaker's voice pill (CSS animation, driven by existing `voiceStates`/hook `speaking`).
- Win: confetti burst on GameOver via **`canvas-confetti`** (MIT, ~7kb) — a dev/runtime dependency added to `web`.
- All motion respects `prefers-reduced-motion` (disable non-essential animation).

## What Does Not Change

- `internal/game` engine rules and tests.
- Reducer logic (players carry the new `emoji` field transparently).
- WebRTC/voice signaling and behavior.
- Message protocol, except the additive `emoji` on join and on the player object.

## Testing

- **Backend**: extend a room/server test to assert a joined player's `emoji` round-trips into `room_state`/`lobby_update`. Existing engine/relay tests stay green.
- **Frontend**: unit test the emoji helper (list + default pick); existing `reducer`/`invite` tests stay green; `vite build` clean.
- **Manual (browser)**: visual pass across all screens in multiple windows — pick emoji on join, see avatars in lobby/voice/game, confirm speaking pulse and the game-over confetti.

## Delivery Order (for the plan)

1. Design system + self-hosted font in `styles.css` (foundation).
2. Emoji avatars: backend field + join plumbing (with round-trip test), then the join-screen picker + helper.
3. Restyle screens in dependency order: Landing → Lobby → VoiceBar → GameBoard/Canvas → Chat → Voting → Reveal → GameOver (+ confetti).
4. Motion polish + `prefers-reduced-motion`.
5. Manual visual validation.

## Known Limitations

- Emoji is ephemeral per session (no persistence) — consistent with the anonymous, no-accounts design.
- Duplicate emoji picks are allowed (players are still distinguished by name/id); not worth enforcing uniqueness for v1.
- Self-hosted font adds two small woff2 files to the repo/binary (acceptable; keeps it offline-capable).
