# Third-Party Assets

Recorded/verified 2026-09-06.

## Fonts

| Asset | File | License | Source |
|---|---|---|---|
| Fredoka (variable, weights 400–700) | `web/public/fonts/fredoka.woff2` | SIL Open Font License 1.1 | https://github.com/google/fonts/tree/main/ofl/fredoka |

The full Fredoka license notice is bundled at
[`licenses/Fredoka-OFL.txt`](./licenses/Fredoka-OFL.txt) (canonical text:
https://github.com/google/fonts/blob/main/ofl/fredoka/OFL.txt).

## Sound effects

Fauxlands ships **no bundled audio asset files**. Every sound cue is
synthesized at runtime by an original Web Audio layer
(`web/src/shared/audio/sfx.js`) — this is the spec's explicit fallback for
when CC0 asset licensing cannot be independently verified in the build
environment. There is therefore no third-party audio to license.

If you later prefer sampled audio, verified CC0 packs such as Kenney's UI
Audio / Interface Sounds (https://kenney.nl/assets/interface-sounds) are a
good fit — bundle them locally (never hotlink) and add them to this table with
their license and download date.

## Icons

Interface icons come from `lucide-react` (ISC) via static named imports — see
THIRD_PARTY_NOTICES.md. No icon SVG files are vendored.

## Effects

Victory confetti uses `canvas-confetti` (ISC). No other visual-effect assets
are bundled; terrain, factions, structures, and arrows are drawn as inline SVG
in `web/src/features/game/`.
