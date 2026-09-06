import { useState } from 'react'
import { Copy, Share2, Crown, LogOut } from 'lucide-react'
import { inviteURL } from '../../invite.js'
import { BRAND } from '../../app/brand.js'
import { Tutorial } from '../game/Tutorial.jsx'

const PRESETS = [
  { id: 'quick', label: 'Quick', detail: '6 rounds · ~10 min' },
  { id: 'standard', label: 'Standard', detail: '8 rounds · ~15–20 min' },
  { id: 'relaxed', label: 'Relaxed', detail: '8 rounds · unhurried' },
]

// Lobby is the pre-match room: roster, spectators, readiness, host settings,
// and the invite link. The host starts once 3–6 connected players are ready.
export function Lobby({ state, meId, code, send, onLeave, disabled }) {
  const [copied, setCopied] = useState(false)
  const isHost = state.hostId === meId
  const players = state.players || []
  const me = players.find((p) => p.id === meId)
  const url = inviteURL(location.origin, code)

  const connectedPlayers = players.filter((p) => p.connected)
  const allReady = connectedPlayers.length > 0 && connectedPlayers.every((p) => p.ready)
  const enoughPlayers = players.length >= 3 && players.length <= 6
  const canStart = isHost && enoughPlayers && allReady

  const startHint = !enoughPlayers
    ? 'Need 3–6 players'
    : !allReady
      ? 'All connected players must be Ready'
      : ''

  function copy() {
    navigator.clipboard?.writeText(url).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    })
  }
  function share() {
    if (navigator.share) navigator.share({ title: BRAND.name, text: `Join my ${BRAND.name} room`, url }).catch(() => {})
    else copy()
  }

  return (
    <div className="lobby">
      <div className="lobby-head card">
        <div>
          <h2>Room <span className="room-code">{code}</span></h2>
          <p className="muted small">{BRAND.tagline}</p>
        </div>
        <div className="lobby-invite">
          <button className="btn-secondary" onClick={copy}><Copy size={15} aria-hidden="true" /> {copied ? 'Copied!' : 'Copy link'}</button>
          <button className="btn-ghost" onClick={share}><Share2 size={15} aria-hidden="true" /> Share</button>
        </div>
      </div>

      <div className="lobby-grid">
        <div className="card">
          <h3>Players ({players.length}/6)</h3>
          <ul className="lobby-players">
            {players.map((p) => (
              <li key={p.id} className={`lobby-player ${!p.connected ? 'player-disconnected' : ''}`}>
                <span className="player-emoji">{p.emoji}</span>
                <span className="player-name">{p.name}</span>
                {p.id === state.hostId && <Crown size={13} className="host-badge" aria-label="Host" />}
                <span className={`ready-pill ${p.ready ? 'is-ready' : ''}`}>{p.ready ? 'Ready' : 'Not ready'}</span>
                {isHost && p.id !== meId && (
                  <button className="icon-btn" aria-label={`Remove ${p.name}`} disabled={disabled} onClick={() => send('remove_player', { playerId: p.id })}>×</button>
                )}
              </li>
            ))}
          </ul>
          {(state.spectators || []).length > 0 && (
            <p className="muted small">Watching: {(state.spectators || []).map((s) => s.name).join(', ')}</p>
          )}
        </div>

        <div className="card">
          <h3>Match</h3>
          <div className="preset-list" role="radiogroup" aria-label="Match preset">
            {PRESETS.map((p) => (
              <label key={p.id} className={`preset-option ${state.preset === p.id ? 'preset-active' : ''}`}>
                <input
                  type="radio"
                  name="preset"
                  checked={state.preset === p.id}
                  disabled={!isHost || disabled}
                  onChange={() => send('update_settings', { preset: p.id })}
                />
                <span className="preset-label">{p.label}</span>
                <span className="preset-detail">{p.detail}</span>
              </label>
            ))}
          </div>

          {me && (
            <button className={`btn-primary ready-btn ${me.ready ? 'is-on' : ''}`} disabled={disabled} onClick={() => send('set_ready', { ready: !me.ready })}>
              {me.ready ? "I'm ready ✓" : 'Ready up'}
            </button>
          )}
          {isHost && (
            <button className="btn-primary" disabled={disabled || !canStart} onClick={() => send('start_match')}>
              Start match
            </button>
          )}
          {isHost && !canStart && <p className="muted small">{startHint}</p>}
        </div>
      </div>

      <div className="lobby-foot">
        <Tutorial />
        <button className="btn-ghost" onClick={onLeave}><LogOut size={15} aria-hidden="true" /> Leave</button>
      </div>
    </div>
  )
}
