import { useState } from 'react'
import { createRoom } from '../api.js'
import { EMOJIS, defaultEmoji } from '../emoji.js'

export default function Landing({ onEnter, initialCode }) {
  const invited = !!initialCode
  const [name, setName] = useState('')
  const [emoji, setEmoji] = useState(defaultEmoji())
  const [code, setCode] = useState(initialCode || '')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const host = async () => {
    if (!name.trim()) return setErr('Enter a name')
    setBusy(true); setErr('')
    try {
      const { code, hostToken } = await createRoom(name.trim())
      onEnter({ code, join: { name: name.trim(), emoji, reconnectToken: hostToken } })
    } catch { setErr('Could not create room'); setBusy(false) }
  }
  const join = () => {
    if (!name.trim()) return setErr('Enter a name')
    if (!code.trim()) return setErr('Enter a room code')
    onEnter({ code: code.trim().toUpperCase(), join: { name: name.trim(), emoji } })
  }

  return (
    <div className="center">
      <div className="card col">
        <h1 className="logo">Fauxtist</h1>
        {invited
          ? <p className="muted">You're joining room <b>{initialCode}</b>. Pick a look and enter a name.</p>
          : <p className="muted">One of you is faking it. Draw one stroke at a time — don't get caught.</p>}

        <input placeholder="Your name" value={name} onChange={(e) => setName(e.target.value)} />

        <div>
          <div className="label" style={{ marginBottom: 8 }}>Pick your avatar</div>
          <div className="row" style={{ gap: 8 }}>
            {EMOJIS.map((e) => (
              <button
                key={e}
                type="button"
                aria-pressed={emoji === e}
                onClick={() => setEmoji(e)}
                style={{
                  width: 44, height: 44, padding: 0, fontSize: 24,
                  border: '2px solid var(--ink)', borderRadius: 12,
                  background: emoji === e ? 'var(--amber)' : '#fff',
                  boxShadow: emoji === e ? '0 0 0 3px var(--violet)' : '0 3px 0 rgba(43,35,64,.12)',
                }}
              >
                {e}
              </button>
            ))}
          </div>
        </div>

        {!invited && <button className="btn-primary" onClick={host} disabled={busy}>Create room</button>}

        <div className="row">
          <input
            placeholder="Room code"
            value={code}
            readOnly={invited}
            onChange={(e) => setCode(e.target.value)}
            style={{ flex: 1 }}
          />
          <button className="btn-ghost" onClick={join} disabled={busy}>Join</button>
        </div>

        {err && <p style={{ color: 'var(--coral)' }}>{err}</p>}
      </div>
    </div>
  )
}
