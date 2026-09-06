import { useState } from 'react'
import { tileById, adjacentTiles, ownedTiles } from './orderDraft.js'

// NegotiationPanel lets a player draw non-binding proposal arrows and ping
// tiles during negotiation. Nothing here commits anything — it is pure
// table-talk, clearly labelled as a proposal.
export function NegotiationPanel({ board, meId, selectedTile, send, disabled }) {
  const owned = ownedTiles(board, meId)
  const [from, setFrom] = useState(selectedTile || owned[0]?.id || '')
  const targets = adjacentTiles(board, from)
  const [to, setTo] = useState(targets[0] || '')

  function propose() {
    if (from && to) send('proposal_arrow', { from, to })
  }
  function ping() {
    if (selectedTile || from) send('map_ping', { tile: selectedTile || from })
  }

  return (
    <div className="negotiation-panel">
      <h3>Negotiation</h3>
      <p className="muted">Make your case. Propose moves and ping tiles — none of it is binding.</p>
      <label className="field">
        <span>From</span>
        <select value={from} disabled={disabled} onChange={(e) => setFrom(e.target.value)}>
          {owned.map((t) => (
            <option key={t.id} value={t.id}>{t.id.replace('t_', '')}</option>
          ))}
        </select>
      </label>
      <label className="field">
        <span>To</span>
        <select value={to} disabled={disabled} onChange={(e) => setTo(e.target.value)}>
          {targets.map((id) => (
            <option key={id} value={id}>{tileById(board, id)?.id.replace('t_', '')}</option>
          ))}
        </select>
      </label>
      <div className="order-actions">
        <button className="btn-secondary" disabled={disabled || !to} onClick={propose}>Propose arrow</button>
        <button className="btn-ghost" disabled={disabled} onClick={ping}>Ping tile</button>
      </div>
    </div>
  )
}
