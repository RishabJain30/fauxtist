# Fauxlands — Game Rules

*Every order tells a story. Some are lies.*

Fauxlands is a server-authoritative, deterministic 2D hex strategy game for
3–6 players. There is no software fee, no account, and no combat dice.

## The hook

Each round, players negotiate in the open, then commit **one public
declaration** — an order everyone can see. Everyone submits their remaining
orders **secretly**, and all orders resolve **simultaneously**. Once per match,
a player may turn their public declaration into a **Faux Order**: it looks
binding, but it never executes — they quietly do something else instead.

## Players and length

- 3–6 active human players; up to 10 read-only spectators per room.
- No accounts, no bots, no elimination — losing your last non-capital tile
  never removes you.
- A match is 6 rounds (Quick) or 8 rounds (Standard / Relaxed). Standard
  targets ~15–20 minutes.

## Round phases

`LOBBY → INCOME → NEGOTIATION → DECLARATION → DECLARATION_REVEAL →
SECRET_PLANNING → RESOLUTION → ROUND_SUMMARY → (next INCOME | GAME_OVER)`

Every timed phase has a server-authoritative absolute deadline; your client
only displays the remaining time. If everyone completes a phase early, a
visible 3-second "all locked" countdown runs, after which submissions are
irreversible.

### Timing presets

| Preset | Rounds | Income | Negotiation | Declaration | Reveal | Planning | Resolution (max) | Summary |
|---|---|---|---|---|---|---|---|---|
| Quick | 6 | 2s | 20s | 10s | 3s | 25s | 12s | 6s |
| Standard | 8 | 3s | 35s | 15s | 3s | 35s | 15s | 8s |
| Relaxed | 8 | 3s | 50s | 20s | 4s | 50s | 18s | 10s |

## The board

Authored, data-driven hex maps — one balanced template per player count
(~25 / 31 / 37 / 43 playable hexes for 3/4/5/6 players). Each map has one
capital per player, exactly five relics, `players + 1` mine sites, and one
connected graph, with spawns balanced so the distance to the nearest relic,
mine site, and opponent varies by at most one hex between spawn slots. At match
start the server randomly assigns players to spawns and rotates/mirrors the map.

**Tile types:** Normal, Capital, Relic, Mine site.
**Structures:** none, Fortress (+1 permanent defence, can recruit), Mine
(+1 energy per round). One structure per territory; never on a relic or
capital.

**Capital sanctuary:** a capital always belongs to its owner, can never be
captured or entered by an enemy, can recruit and send armies out, and keeps a
player in the match forever.

## Starting state

Each player begins with a capital (3 armies), one adjacent owned territory
(2 armies), 4 energy, 0 influence, one unused Faux Order, and three real
command slots per round.

## Economy

From round two on, each non-forfeited player receives `3 base energy + 1 per
completed Mine`, capped at 12. Income applies exactly once per round; a
newly-built Mine pays from the next round.

## Commands (three real slots per round)

| Command | Cost | Effect |
|---|---|---|
| March | 0 | Move 1–3 armies to an adjacent tile (≥1 must remain; one March per origin). Friendly = reinforce, otherwise attack. Enemy capitals can't be targeted. |
| Fortify | 1 | +2 defence on one owned non-capital tile this resolution. |
| Recruit | 3 | +2 armies at your capital or an owned Fortress (they defend now, march next round). Max one per round. |
| Build Fortress | 3 | On an owned normal tile or unused mine site; resolves after combat; refunded if the tile is lost. |
| Build Mine | 4 | On an owned unused mine site; resolves after combat; refunded if the tile is lost. |
| Hold | 0 | Nothing. Fills unused/timed-out slots. |

## Declaration and the Faux Order

During DECLARATION you privately choose one command; all declarations reveal at
once. A normal declaration is binding and is your first real command — you
submit the other two during SECRET_PLANNING.

During SECRET_PLANNING you may mark your declaration **Faux** (once per match,
only a real non-Hold declaration). It looks identical to opponents, executes
nothing, costs nothing, and reserves no armies — so you submit all three real
commands hidden instead. If your Faux declaration was a March, those armies are
free to use in a real command. At resolution the Faux is revealed and dissolves,
then your real orders play out. Your Faux token is then publicly spent.

## Deterministic simultaneous resolution

The server computes the whole round atomically from an immutable planning-start
snapshot, in a fixed order: reveal Faux → validate → reserve energy → Recruit →
Fortify → remove outgoing March armies → friendly reinforcements → aggregate
hostile arrivals → resolve every battle → apply ownership/armies → remove
captured structures → apply/refund Builds → award relic influence → update
Domination streaks → evaluate victory. No dice; results never depend on message
order or map iteration order.

### Combat

At each contested tile, the defender's effective strength is its garrison
(plus friendly incoming) `+1` per Fortress `+2` per Fortify. Each attacking
player's incoming armies combine into one force.

- An attacker captures only with a **unique** highest strength **strictly
  greater** than the defender's. Survivors = `max(1, winner − second-highest)`.
- If attackers tie for highest, **every** tied attack fails.
- If the defender ties or beats the strongest attacker, the defender holds with
  `max(1, garrison − strongest attacker)` armies.
- Empty neutral tiles have defence 0; a neutral relic has a 1-army guardian.
- Defensive bonuses decide battles but never become armies. Armies that
  departed still execute their March even if their origin is captured.

## Victory

At every round end, each controlled relic grants 1 Influence. Controlling ≥3 of
5 relics builds a **Domination streak**; holding ≥3 for two consecutive
round-ends wins immediately. Otherwise, after the final round the most Influence
wins, breaking ties by: relics controlled → non-capital territories → total
armies → remaining energy → shared victory.

Victory reasons: Domination, Influence, Forfeit (only one active player left),
Shared, No Contest (nobody left).

## Leaving

- **Reconnect:** a dropped connection keeps your seat; you rejoin and resync.
- **Leave for now:** go home but keep your seat and credentials; your faction
  auto-Holds on deadlines; the landing page offers Resume.
- **Resign:** permanent — your territories go neutral, your capital goes
  inactive, your credentials are invalidated, and the host migrates if needed.
