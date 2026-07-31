import { useState } from 'react'
import { inviteURL } from '../invite.js'

export default function Lobby({ state, meId, code, onStart }) {
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
    <div className="center">
      <div className="card col">
        <h2>Room {code}</h2>
        <p className="muted">Share this code or link. Need 4–8 players.</p>
        <div className="row">
          <button onClick={copy}>{copied ? 'Copied!' : 'Copy invite link'}</button>
        </div>
        <ul className="players">
          {state.players.map((p) => (
            <li key={p.id} className={p.id === meId ? 'me' : ''}>
              {p.name} {p.id === state.hostId && <span className="badge">host</span>}
            </li>
          ))}
        </ul>
        {isHost
          ? <button onClick={onStart} disabled={!enough}>{enough ? 'Start game' : 'Waiting for players…'}</button>
          : <p className="muted">Waiting for the host to start…</p>}
      </div>
    </div>
  )
}
