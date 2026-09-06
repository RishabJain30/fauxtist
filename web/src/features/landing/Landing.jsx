import { useState } from 'react'
import { createRoom } from '../../api.js'
import { EMOJIS } from '../../emoji.js'
import { listResumableRooms, saveCredentials } from '../../credentials.js'
import { BRAND } from '../../app/brand.js'
import { Tutorial } from '../game/Tutorial.jsx'

const JOIN_ERRORS = {
  room_not_found: 'No room with that code.',
  capacity_reached: 'The server is at capacity — try again shortly.',
  rate_limited: 'Slow down a moment, then try again.',
  invalid_name: 'Pick a different name.',
  invalid_emoji: 'Pick an avatar from the list.',
}

// Landing is the entry screen: create or join a room, pick a name and avatar,
// resume a saved seat, or read the rules.
export function Landing({ onEnter, initialCode }) {
  const [tab, setTab] = useState(initialCode ? 'join' : 'create')
  const [name, setName] = useState('')
  const [emoji, setEmoji] = useState(EMOJIS[0])
  const [code, setCode] = useState(initialCode || '')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(null)
  const resumable = listResumableRooms()

  async function create() {
    if (!name.trim()) return setError('Enter a name.')
    setBusy(true)
    setError(null)
    try {
      const { code: roomCode, playerId, reconnectToken } = await createRoom(name.trim(), emoji)
      // The host seat already exists (created by POST); connect as a reconnect
      // with the returned credentials rather than a fresh join.
      saveCredentials(roomCode, playerId, reconnectToken)
      onEnter({ code: roomCode, join: { playerId, reconnectToken } })
    } catch (e) {
      setError(JOIN_ERRORS[e.code] || 'Could not create a room.')
      setBusy(false)
    }
  }

  function join(asSpectator = false) {
    const c = code.trim().toUpperCase()
    if (!c) return setError('Enter a room code.')
    if (!asSpectator && !name.trim()) return setError('Enter a name.')
    onEnter({ code: c, join: { name: name.trim() || 'Watcher', emoji, asSpectator } })
  }

  return (
    <div className="landing">
      <div className="landing-hero">
        <h1 className="brand-title">{BRAND.name}</h1>
        <p className="brand-tagline">{BRAND.tagline}</p>
      </div>

      {resumable.length > 0 && (
        <div className="resume-cards">
          {resumable.map((rc) => (
            <button key={rc} className="resume-card" onClick={() => onEnter({ code: rc, join: {} })}>
              Resume room <strong>{rc}</strong>
            </button>
          ))}
        </div>
      )}

      <div className="card landing-card">
        <div className="tabs" role="tablist">
          <button role="tab" aria-selected={tab === 'create'} className={tab === 'create' ? 'tab-active' : ''} onClick={() => setTab('create')}>Create</button>
          <button role="tab" aria-selected={tab === 'join'} className={tab === 'join' ? 'tab-active' : ''} onClick={() => setTab('join')}>Join</button>
        </div>

        <label className="field">
          <span>Your name</span>
          <input value={name} maxLength={24} onChange={(e) => setName(e.target.value)} placeholder="e.g. Robin" />
        </label>

        <div className="field">
          <span>Avatar</span>
          <div className="emoji-picker" role="radiogroup" aria-label="Avatar">
            {EMOJIS.map((e) => (
              <button
                key={e}
                role="radio"
                aria-checked={emoji === e}
                className={`emoji-choice ${emoji === e ? 'emoji-active' : ''}`}
                onClick={() => setEmoji(e)}
              >
                {e}
              </button>
            ))}
          </div>
        </div>

        {tab === 'join' && (
          <label className="field">
            <span>Room code</span>
            <input value={code} maxLength={8} onChange={(e) => setCode(e.target.value.toUpperCase())} placeholder="ABCD" />
          </label>
        )}

        {error && <p className="form-error" role="alert">{error}</p>}

        {tab === 'create' ? (
          <button className="btn-primary" disabled={busy} onClick={create}>{busy ? 'Creating…' : 'Create room'}</button>
        ) : (
          <div className="order-actions">
            <button className="btn-primary" onClick={() => join(false)}>Join</button>
            <button className="btn-ghost" onClick={() => join(true)}>Watch</button>
          </div>
        )}
      </div>

      <div className="landing-foot">
        <Tutorial />
      </div>
    </div>
  )
}
