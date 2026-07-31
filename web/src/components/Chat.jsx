import { useState } from 'react'

export default function Chat({ state, send, canEndDiscussion, onEnd }) {
  const [text, setText] = useState('')
  const submit = (e) => {
    e.preventDefault()
    if (text.trim()) { send('chat_message', { text: text.trim() }); setText('') }
  }
  const playerOf = (id) => state.players.find((p) => p.id === id)

  return (
    <div className="card col pop-in">
      <div className="label">Discussion — who's faking it?</div>
      <div style={{ maxHeight: 200, overflowY: 'auto', display: 'flex', flexDirection: 'column', gap: 6 }}>
        {state.chat.map((m, i) => {
          const p = playerOf(m.from)
          return (
            <div key={i} className="row" style={{ gap: 8 }}>
              <span className="avatar" style={{ width: 28, height: 28, fontSize: 16, borderRadius: 8 }}>{p?.emoji || '🎭'}</span>
              <b>{p?.name || m.from}:</b> {m.text}
            </div>
          )
        })}
        {state.chat.length === 0 && <p className="muted">Say something — call out who seemed lost.</p>}
      </div>
      <form className="row" onSubmit={submit}>
        <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Type a message…" style={{ flex: 1 }} />
        <button type="submit">Send</button>
      </form>
      {canEndDiscussion && <button className="btn-primary" onClick={onEnd}>Start voting →</button>}
    </div>
  )
}
