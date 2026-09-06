import { useState } from 'react'
import { Lock, Unlock, Trash2, Eye } from 'lucide-react'
import { CommandBuilder } from './CommandBuilder.jsx'
import { COMMAND_LABELS, draftEnergy, slotsRemaining, draftToWire, realCommands } from './orderDraft.js'

// OrderPanel drives the DECLARATION and SECRET_PLANNING phases: building the
// single public declaration, then the hidden real orders and the optional Faux
// Order, with a live energy/slot preview and lock control.
export function OrderPanel({ phase, board, meId, you, myDeclaration, myOrders, selectedTile, onSelectTile, send, disabled, sfx }) {
  if (phase === 'declaration') {
    return <DeclarationBuilder board={board} meId={meId} you={you} myDeclaration={myDeclaration} selectedTile={selectedTile} onSelectTile={onSelectTile} send={send} disabled={disabled} sfx={sfx} />
  }
  if (phase === 'secret_planning') {
    return <PlanningBuilder board={board} meId={meId} you={you} myDeclaration={myDeclaration} myOrders={myOrders} selectedTile={selectedTile} onSelectTile={onSelectTile} send={send} disabled={disabled} sfx={sfx} />
  }
  return null
}

function DeclarationBuilder({ board, meId, you, myDeclaration, selectedTile, onSelectTile, send, disabled, sfx }) {
  const [declared, setDeclared] = useState(myDeclaration || null)

  function submit(cmd) {
    send('submit_declaration', { command: cmd })
    setDeclared(cmd)
    sfx?.('order_placed')
  }

  return (
    <div className="order-panel">
      <h3>Your declaration</h3>
      <p className="muted">Commit one order. Everyone will see it when the phase ends — but you can turn it into a lie later.</p>
      {declared ? (
        <div className="declared-card">
          <span className="cmd-line">{describeCommand(declared)}</span>
          <button className="btn-secondary" disabled={disabled} onClick={() => setDeclared(null)}>Change</button>
        </div>
      ) : (
        <CommandBuilder board={board} meId={meId} selectedTile={selectedTile} onSelectTile={onSelectTile} onAdd={submit} disabled={disabled} />
      )}
      <p className="hint">Energy: ⚡{you?.energy ?? 0}</p>
    </div>
  )
}

function PlanningBuilder({ board, meId, you, myDeclaration, myOrders, selectedTile, onSelectTile, send, disabled, sfx }) {
  const declaration = myDeclaration || null
  const canFaux = you?.fauxAvailable && declaration && declaration.type && declaration.type !== 'hold'
  const [hidden, setHidden] = useState(() => myOrders?.commands || [])
  const [faux, setFaux] = useState(() => !!myOrders?.faux)
  const [locked, setLocked] = useState(() => !!myOrders?.locked)

  const remaining = slotsRemaining(declaration, hidden, faux)
  const energy = draftEnergy(declaration, hidden, faux)
  const overspend = energy > (you?.energy ?? 0)

  function addCommand(cmd) {
    if (remaining <= 0) return
    setHidden((h) => [...h, cmd])
    sfx?.('order_placed')
  }
  function removeCommand(i) {
    setHidden((h) => h.filter((_, idx) => idx !== i))
    sfx?.('order_removed')
  }
  function save() {
    send('set_orders', { commands: draftToWire(hidden), faux })
  }
  function lock() {
    send('set_orders', { commands: draftToWire(hidden), faux })
    send('lock_orders')
    setLocked(true)
    sfx?.('orders_locked')
  }
  function unlock() {
    send('unlock_orders')
    setLocked(false)
  }

  const full = realCommands(declaration, hidden, faux)

  return (
    <div className="order-panel">
      <h3>Secret planning</h3>
      {declaration && (
        <div className={`declared-card ${faux ? 'declared-faux' : ''}`}>
          <Eye size={14} aria-hidden="true" />
          <span className="cmd-line">Public: {describeCommand(declaration)}</span>
          {faux && <span className="faux-tag">FAUX</span>}
        </div>
      )}
      {canFaux && (
        <label className="faux-toggle">
          <input type="checkbox" checked={faux} disabled={disabled || locked} onChange={(e) => setFaux(e.target.checked)} />
          Make my declaration a Faux Order (it won&apos;t execute — I&apos;ll do something else)
        </label>
      )}

      <ol className="order-list" aria-label="Your real orders this round">
        {full.map((c, i) => (
          <li key={i} className="order-row">
            <span className="order-index">{i + 1}</span>
            <span className="cmd-line">{describeCommand(c)}</span>
            {i >= (faux ? 0 : 1) && !locked && (
              <button className="icon-btn" aria-label="Remove" disabled={disabled} onClick={() => removeCommand(i - (faux ? 0 : 1))}>
                <Trash2 size={14} />
              </button>
            )}
          </li>
        ))}
        {Array.from({ length: remaining }).map((_, i) => (
          <li key={`empty-${i}`} className="order-row order-empty">
            <span className="order-index">{full.length + i + 1}</span>
            <span className="cmd-line muted">Hold (empty slot)</span>
          </li>
        ))}
      </ol>

      <p className={`energy-line ${overspend ? 'over' : ''}`}>Energy: ⚡{energy} / {you?.energy ?? 0}{overspend ? ' — too much!' : ''}</p>

      {!locked && remaining > 0 && (
        <CommandBuilder board={board} meId={meId} selectedTile={selectedTile} onSelectTile={onSelectTile} onAdd={addCommand} disabled={disabled} />
      )}

      <div className="order-actions">
        {!locked ? (
          <>
            <button className="btn-secondary" disabled={disabled || overspend} onClick={save}>Save</button>
            <button className="btn-primary" disabled={disabled || overspend} onClick={lock}>
              <Lock size={14} aria-hidden="true" /> Lock orders
            </button>
          </>
        ) : (
          <button className="btn-secondary" disabled={disabled} onClick={unlock}>
            <Unlock size={14} aria-hidden="true" /> Unlock
          </button>
        )}
      </div>
    </div>
  )
}

export function describeCommand(c) {
  if (!c || !c.type) return 'Hold'
  const label = COMMAND_LABELS[c.type] || c.type
  if (c.type === 'march') return `${label} ${c.armies}⚔ ${short(c.from)} → ${short(c.to)}`
  if (c.type === 'hold') return 'Hold'
  return `${label} @ ${short(c.to)}`
}

function short(id) {
  return id ? id.replace('t_', '') : '?'
}
