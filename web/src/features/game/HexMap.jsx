import { HexTile } from './HexTile.jsx'
import { factionOf } from './factions.js'
import { BRAND } from '../../app/brand.js'

// HexMap renders the whole board as inline SVG, layered back-to-front:
// background, terrain, ownership, structures, armies, then order/proposal
// arrows and selection effects on top. Ownership is drawn with reusable
// <defs> patterns and gradients; strokes use non-scaling-stroke so they stay
// crisp at any zoom.
export function HexMap({ board, layout, factionByOwner, selectedId, highlightedIds, dimmedIds, arrows, onSelect }) {
  const highlighted = highlightedIds || new Set()
  const dimmed = dimmedIds || new Set()

  return (
    <svg className="hex-map" viewBox={layout.viewBox} role="group" aria-label={`${BRAND.name} board`} preserveAspectRatio="xMidYMid meet">
      <title>{BRAND.name} board</title>
      <desc>A hex map of territories, relics, mine sites, and capitals. Ownership is shown by faction colour, pattern, and sigil. Use the territory list to play without the map.</desc>
      <defs>
        <radialGradient id="terrain-plain" cx="40%" cy="35%">
          <stop offset="0%" stopColor="#f3e7cf" />
          <stop offset="100%" stopColor="#e3d2b0" />
        </radialGradient>
        <radialGradient id="terrain-relic" cx="50%" cy="40%">
          <stop offset="0%" stopColor="#efe6ff" />
          <stop offset="100%" stopColor="#d8c6f2" />
        </radialGradient>
        <radialGradient id="terrain-mine" cx="50%" cy="40%">
          <stop offset="0%" stopColor="#e6ecf3" />
          <stop offset="100%" stopColor="#c9d3df" />
        </radialGradient>
        <Pattern id="pat-dots"><circle cx="3" cy="3" r="1.4" /></Pattern>
        <Pattern id="pat-waves"><path d="M0 4 q 3 -3 6 0" fill="none" stroke="currentColor" strokeWidth="1.2" /></Pattern>
        <Pattern id="pat-leaf"><path d="M3 1 q 3 3 0 5 q -3 -2 0 -5" /></Pattern>
        <Pattern id="pat-diag"><path d="M0 6 L6 0" stroke="currentColor" strokeWidth="1.4" /></Pattern>
        <Pattern id="pat-rays"><path d="M3 0 L3 6 M0 3 L6 3" stroke="currentColor" strokeWidth="1" /></Pattern>
        <Pattern id="pat-grid"><path d="M0 0 H6 M0 0 V6" fill="none" stroke="currentColor" strokeWidth="1" /></Pattern>
        <marker id="arrow-head" markerWidth="6" markerHeight="6" refX="4" refY="3" orient="auto">
          <path d="M0 0 L6 3 L0 6 z" fill="currentColor" />
        </marker>
      </defs>

      {/* Layers 2-6: terrain, ownership, structures, armies (all inside HexTile) */}
      <g className="layer-tiles">
        {board.map((tile) => {
          const hex = layout.byId[tile.id]
          if (!hex) return null
          return (
            <HexTile
              key={tile.id}
              hex={hex}
              tile={tile}
              ownerFaction={tile.owner ? factionByOwner[tile.owner] || factionOf('') : factionOf('')}
              selected={selectedId === tile.id}
              highlighted={highlighted.has(tile.id)}
              dimmed={dimmed.has(tile.id)}
              onSelect={onSelect}
            />
          )
        })}
      </g>

      {/* Layer 6: order / declaration / proposal arrows */}
      <g className="layer-arrows">
        {(arrows || []).map((a, i) => {
          const from = layout.byId[a.from]
          const to = layout.byId[a.to]
          if (!from || !to) return null
          return <Arrow key={`${a.from}-${a.to}-${a.kind}-${i}`} from={from} to={to} kind={a.kind} color={a.color} />
        })}
      </g>
    </svg>
  )
}

// Pattern wraps a small tiling pattern the ownership layer paints with; each
// tile passes the faction colour via the parent's `color` on <polygon>.
function Pattern({ id, children }) {
  return (
    <pattern id={id} width="6" height="6" patternUnits="userSpaceOnUse" className="own-pattern" fill="currentColor" stroke="none" color="#20263a">
      {children}
    </pattern>
  )
}

function Arrow({ from, to, kind, color }) {
  // A gentle quadratic curve between the two hex centres.
  const mx = (from.cx + to.cx) / 2
  const my = (from.cy + to.cy) / 2 - dist(from, to) * 0.14
  const cls = kind === 'proposal' ? 'arrow-proposal' : kind === 'faux' ? 'arrow-faux' : 'arrow-order'
  return (
    <path
      d={`M ${from.cx} ${from.cy} Q ${mx} ${my} ${to.cx} ${to.cy}`}
      className={cls}
      style={color ? { color } : undefined}
      fill="none"
      markerEnd="url(#arrow-head)"
      vectorEffect="non-scaling-stroke"
    />
  )
}

function dist(a, b) {
  return Math.hypot(a.cx - b.cx, a.cy - b.cy)
}
