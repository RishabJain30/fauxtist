import { memo } from 'react'
import { cornersToPoints } from './mapGeometry.js'
import { factionOf } from './factions.js'

// HexTile renders one board hex: its terrain, ownership (colour + pattern +
// sigil, never colour alone), garrison, structure, and any relic/mine detail.
// Memoized so a timer tick or an unrelated tile's change never rerenders it.
function HexTileImpl({ hex, tile, ownerFaction, selected, highlighted, dimmed, onSelect }) {
  const points = cornersToPoints(hex.corners)
  const owned = !!tile.owner
  const fac = owned ? ownerFaction : factionOf('')
  const isRelic = tile.type === 'relic'
  const isMineSite = tile.type === 'mine_site'
  const isCapital = tile.type === 'capital'

  const label = tileAccessibleName(tile, fac)

  return (
    <g
      className={`hex ${selected ? 'hex-selected' : ''} ${highlighted ? 'hex-highlight' : ''} ${dimmed ? 'hex-dim' : ''}`}
      role="button"
      tabIndex={0}
      aria-label={label}
      onClick={() => onSelect?.(tile.id)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault()
          onSelect?.(tile.id)
        }
      }}
    >
      <title>{label}</title>
      {/* Terrain base */}
      <polygon points={points} className="hex-terrain" fill={`url(#terrain-${isRelic ? 'relic' : isMineSite ? 'mine' : 'plain'})`} vectorEffect="non-scaling-stroke" />
      {/* Ownership tint + pattern */}
      {owned && (
        <>
          <polygon points={points} fill={fac.color} opacity="0.32" vectorEffect="non-scaling-stroke" />
          <polygon points={points} fill={`url(#pat-${fac.pattern})`} opacity="0.5" vectorEffect="non-scaling-stroke" />
        </>
      )}
      {/* Ownership border */}
      <polygon points={points} className="hex-border" fill="none" stroke={owned ? fac.color : 'rgba(60,70,90,0.5)'} strokeWidth={owned ? 2.5 : 1} vectorEffect="non-scaling-stroke" />

      {/* Relic glow / mine crystal */}
      {isRelic && <circle cx={hex.cx} cy={hex.cy - 8} r="7" className="relic-glow" />}
      {isMineSite && tile.structure !== 'mine' && <polygon points={crystal(hex.cx, hex.cy - 8)} className="mine-crystal" />}

      {/* Structures */}
      {tile.structure === 'fortress' && <text x={hex.cx} y={hex.cy - 4} className="tile-structure" textAnchor="middle">⌂</text>}
      {tile.structure === 'mine' && <text x={hex.cx} y={hex.cy - 4} className="tile-structure" textAnchor="middle">⛏</text>}

      {/* Faction sigil (ownership without relying on colour) */}
      {owned && <text x={hex.cx - 12} y={hex.cy - 6} className="tile-sigil" textAnchor="middle" fill={fac.color}>{fac.sigil}</text>}
      {isCapital && <text x={hex.cx + 12} y={hex.cy - 6} className="tile-capital" textAnchor="middle">★</text>}

      {/* Army counter */}
      {tile.armies > 0 && (
        <g className="army-counter" transform={`translate(${hex.cx}, ${hex.cy + 12})`}>
          <circle r="11" className="army-chip" fill={owned ? fac.color : '#5b6472'} />
          <text className="army-count" textAnchor="middle" dy="4">{tile.armies}</text>
        </g>
      )}
    </g>
  )
}

function crystal(cx, cy) {
  return `${cx},${cy - 6} ${cx + 5},${cy} ${cx},${cy + 6} ${cx - 5},${cy}`
}

function tileAccessibleName(tile, fac) {
  const kind =
    tile.type === 'capital'
      ? 'Capital'
      : tile.type === 'relic'
        ? 'Relic'
        : tile.type === 'mine_site'
          ? 'Mine site'
          : 'Territory'
  const owner = tile.owner ? `${fac.label}` : 'neutral'
  const structure = tile.structure && tile.structure !== 'none' ? `, ${tile.structure}` : ''
  return `${kind}, ${owner}, ${tile.armies} ${tile.armies === 1 ? 'army' : 'armies'}${structure}`
}

export const HexTile = memo(HexTileImpl)
