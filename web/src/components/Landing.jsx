import { useState } from 'react'
import { createRoom } from '../api.js'

export default function Landing({ onEnter }) {
  const [name, setName] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState('')

  const host = async () => {
    if (!name.trim()) return setErr('Enter a name')
    setBusy(true); setErr('')
    try {
      const { code, hostToken } = await createRoom(name.trim())
      onEnter({ code, join: { name: name.trim(), reconnectToken: hostToken } })
    } catch { setErr('Could not create room'); setBusy(false) }
  }
  const join = () => {
    if (!name.trim()) return setErr('Enter a name')
    if (!code.trim()) return setErr('Enter a room code')
    onEnter({ code: code.trim().toUpperCase(), join: { name: name.trim() } })
  }

  return (
    <div className="center">
      <div className="card col">
        <h1>Fauxtist</h1>
        <p className="muted">One of you is faking it. Draw one stroke at a time — don't get caught.</p>
        <input placeholder="Your name" value={name} onChange={(e) => setName(e.target.value)} />
        <div className="row">
          <button onClick={host} disabled={busy}>Create room</button>
        </div>
        <div className="row">
          <input placeholder="Room code" value={code} onChange={(e) => setCode(e.target.value)} />
          <button onClick={join} disabled={busy}>Join</button>
        </div>
        {err && <p style={{ color: '#ff6b6b' }}>{err}</p>}
      </div>
    </div>
  )
}
