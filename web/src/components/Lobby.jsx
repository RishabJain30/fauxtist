import { useState } from 'react'
import { inviteURL } from '../invite.js'

export default function Lobby({ state, meId, code, onStart, disabled }) {
  const isHost = state.hostId === meId
  const enough = state.players.length >= 4
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    const url = inviteURL(location.origin, code)
    try {
      await navigator.clipboard.writeText(url)
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    } catch {
      window.prompt('Copy this invite link:', url)
    }
  }

  return (
    <div className="card col">
      <div className="label">Lobby</div>
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <h2 style={{ margin: 0 }}>Room <span className="roomcode">{code}</span></h2>
        <button className="btn-ghost" onClick={copy}>{copied ? '✓ Copied!' : '🔗 Copy invite link'}</button>
      </div>
      <p className="muted">Gather 4–8 players. One of you will be faking it.</p>
      <ul className="players">
        {state.players.map((p) => (
          <li key={p.id} className="player" style={{ opacity: p.connected === false ? 0.5 : 1 }}>
            <span className="avatar" style={{ background: p.id === meId ? 'var(--amber)' : '#fff' }}>{p.emoji || '🎭'}</span>
            <span className={p.id === meId ? 'me' : ''}>{p.name}</span>
            {p.connected === false && <span className="muted">reconnecting…</span>}
            {p.id === state.hostId && <span className="badge" style={{ marginLeft: 'auto' }}>👑 host</span>}
          </li>
        ))}
      </ul>
      {isHost
        ? <button className="btn-primary" onClick={onStart} disabled={!enough || disabled}>{enough ? 'Start game →' : `Waiting for players… (${state.players.length}/4)`}</button>
        : <p className="muted">Waiting for the host to start…</p>}
    </div>
  )
}
