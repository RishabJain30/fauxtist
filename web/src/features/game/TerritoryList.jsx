import { factionOf } from './factions.js'

// TerritoryList is the semantic, keyboard-navigable mirror of the board: every
// tile as a selectable list item, so the whole game is playable without a
// pointer or the SVG map. Selecting an item is the same as clicking its hex.
export function TerritoryList({ board, meId, factionByOwner, selectedId, onSelect }) {
  const mine = board.filter((t) => t.owner === meId)
  const others = board.filter((t) => t.owner !== meId)

  return (
    <details className="territory-list">
      <summary>Territory list (keyboard-friendly)</summary>
      <div className="territory-groups">
        <Group title="Yours" tiles={mine} factionByOwner={factionByOwner} selectedId={selectedId} onSelect={onSelect} />
        <Group title="Other tiles" tiles={others} factionByOwner={factionByOwner} selectedId={selectedId} onSelect={onSelect} />
      </div>
    </details>
  )
}

function Group({ title, tiles, factionByOwner, selectedId, onSelect }) {
  if (tiles.length === 0) return null
  return (
    <div className="territory-group">
      <h4>{title}</h4>
      <ul>
        {tiles.map((t) => {
          const fac = t.owner ? factionByOwner[t.owner] || factionOf('') : factionOf('')
          return (
            <li key={t.id}>
              <button
                className={`territory-item ${selectedId === t.id ? 'selected' : ''}`}
                aria-pressed={selectedId === t.id}
                onClick={() => onSelect(t.id)}
              >
                <span style={{ color: fac.color }} aria-hidden="true">{fac.sigil}</span>
                <span>{describeTile(t, fac)}</span>
              </button>
            </li>
          )
        })}
      </ul>
    </div>
  )
}

function describeTile(t, fac) {
  const kind = t.type === 'capital' ? 'Capital' : t.type === 'relic' ? 'Relic' : t.type === 'mine_site' ? 'Mine site' : 'Territory'
  const owner = t.owner ? fac.label : 'neutral'
  const structure = t.structure && t.structure !== 'none' ? ` · ${t.structure}` : ''
  return `${t.id.replace('t_', '')} · ${kind} · ${owner} · ${t.armies}⚔${structure}`
}
