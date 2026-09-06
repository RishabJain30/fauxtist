# Third-Party Software Notices

Fauxlands builds on the following open-source software. Exact resolved
versions are pinned in `web/package-lock.json` and `go.sum`. Dependencies
recorded/verified 2026-09-06.

## Frontend (npm — see web/package.json)

| Package | Version | License | Source |
|---|---|---|---|
| react | 19.x | MIT | https://github.com/facebook/react |
| react-dom | 19.x | MIT | https://github.com/facebook/react |
| honeycomb-grid | 4.1.5 | MIT | https://github.com/flauwekeul/honeycomb |
| react-zoom-pan-pinch | 4.2.0 | MIT | https://github.com/BetterTyped/react-zoom-pan-pinch |
| @radix-ui/react-dialog | 1.x | MIT | https://www.radix-ui.com/primitives |
| @radix-ui/react-alert-dialog | 1.x | MIT | https://www.radix-ui.com/primitives |
| lucide-react | 1.x | ISC | https://lucide.dev/license |
| canvas-confetti | 1.9.x | ISC | https://github.com/catdad/canvas-confetti |

Dev tooling (not shipped in the bundle): vite, vitest, @vitejs/plugin-react,
oxlint (all MIT/permissive), and — if installed for browser tests —
`@playwright/test` (Apache-2.0).

Notes on the spec's suggested additions:
- **motion** and **howler** were evaluated but not adopted: ordinary UI motion
  uses CSS transitions / the Web Animations API, and sound uses an original
  Web Audio layer (`web/src/shared/audio/sfx.js`), so neither package is a
  dependency.
- **@radix-ui/react-tooltip** was not adopted: interactive icon buttons carry
  `aria-label`s for their accessible names, and no visible desktop tooltip was
  needed for this release.

## Backend (Go modules — see go.mod / go.sum)

| Module | License | Source |
|---|---|---|
| nhooyr.io/websocket | ISC | https://github.com/nhooyr/websocket |
| golang.org/x/time | BSD-3-Clause | https://cs.opensource.google/go/x/time |

## Fonts and assets

See [THIRD_PARTY_ASSETS.md](./THIRD_PARTY_ASSETS.md).
