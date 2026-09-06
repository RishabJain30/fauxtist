import { useEffect, useState } from 'react'
import { COMMAND_LABELS, legalMarchTargets, tileById, MARCH_MAX } from './orderDraft.js'

// CommandBuilder builds a single legal-looking command from the player's tiles
// and hands it to onAdd. It is advisory: legality shown here mirrors the
// server's rules for a fast UX, but the server re-validates everything.
export function CommandBuilder({ board, meId, selectedTile, onSelectTile, onAdd, disabled }) {
  const owned = (board || []).filter((t) => t.owner === meId)
  const [source, setSource] = useState(selectedTile || owned[0]?.id || '')
  const [type, setType] = useState('')
  const [target, setTarget] = useState('')
  const [armies, setArmies] = useState(1)

  // Keep the source in sync with a board click.
  useEffect(() => {
    if (selectedTile && tileById(board, selectedTile)?.owner === meId) {
      setSource(selectedTile)
    }
  }, [selectedTile, board, meId])

  const srcTile = tileById(board, source)
  const types = availableTypes(srcTile)
  useEffect(() => {
    if (!types.includes(type)) setType(types[0] || '')
  }, [source, types, type])

  const marchTargets = type === 'march' ? legalMarchTargets(board, source, meId) : []
  useEffect(() => {
    if (type === 'march' && !marchTargets.includes(target)) setTarget(marchTargets[0] || '')
  }, [type, source, target, marchTargets])

  const maxArmies = srcTile ? Math.min(MARCH_MAX, Math.max(1, srcTile.armies - 1)) : 1

  function add() {
    if (!type || !srcTile) return
    let cmd
    if (type === 'march') {
      if (!target) return
      cmd = { type, from: source, to: target, armies: Math.min(armies, maxArmies) }
    } else {
      cmd = { type, to: source }
    }
    onAdd(cmd)
  }

  if (owned.length === 0) {
    return <p className="muted">You control no territories that can act.</p>
  }

  return (
    <div className="command-builder">
      <label className="field">
        <span>Source</span>
        <select
          value={source}
          disabled={disabled}
          onChange={(e) => {
            setSource(e.target.value)
            onSelectTile?.(e.target.value)
          }}
        >
          {owned.map((t) => (
            <option key={t.id} value={t.id}>
              {tileLabel(t)} · {t.armies}⚔
            </option>
          ))}
        </select>
      </label>

      <div className="type-buttons" role="group" aria-label="Command type">
        {types.length === 0 && <span className="muted">No legal command from here.</span>}
        {types.map((tp) => (
          <button key={tp} className={`chip ${type === tp ? 'chip-active' : ''}`} disabled={disabled} onClick={() => setType(tp)}>
            {COMMAND_LABELS[tp]}
          </button>
        ))}
      </div>

      {type === 'march' && (
        <>
          <label className="field">
            <span>Destination</span>
            <select value={target} disabled={disabled} onChange={(e) => setTarget(e.target.value)}>
              {marchTargets.length === 0 && <option value="">no adjacent target</option>}
              {marchTargets.map((id) => (
                <option key={id} value={id}>
                  {tileLabel(tileById(board, id))}
                </option>
              ))}
            </select>
          </label>
          <label className="field">
            <span>Armies ({armies})</span>
            <input
              type="range"
              min="1"
              max={maxArmies}
              value={Math.min(armies, maxArmies)}
              disabled={disabled}
              onChange={(e) => setArmies(Number(e.target.value))}
            />
          </label>
        </>
      )}

      <button className="btn-primary" disabled={disabled || !type} onClick={add}>
        Add command
      </button>
    </div>
  )
}

function availableTypes(t) {
  if (!t) return []
  const out = []
  if (t.armies > 1) out.push('march')
  if (t.type !== 'capital') out.push('fortify')
  if (t.type === 'capital' || t.structure === 'fortress') out.push('recruit')
  if (t.structure === 'none' || !t.structure) {
    if (t.type === 'normal' || t.type === 'mine_site') out.push('build_fortress')
    if (t.type === 'mine_site') out.push('build_mine')
  }
  return out
}

function tileLabel(t) {
  if (!t) return '?'
  const kind = t.type === 'capital' ? 'Capital' : t.type === 'relic' ? 'Relic' : t.type === 'mine_site' ? 'Mine site' : 'Territory'
  return `${kind} ${t.id.replace('t_', '')}`
}
