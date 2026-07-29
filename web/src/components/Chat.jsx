import { useState } from 'react'

export default function Chat({ state, send, canEndDiscussion, onEnd }) {
  const [text, setText] = useState('')
  const submit = (e) => {
    e.preventDefault()
    if (text.trim()) { send('chat_message', { text: text.trim() }); setText('') }
  }
  const nameOf = (id) => state.players.find((p) => p.id === id)?.name || id
  return (
    <div className="card col">
      <h3>Discussion</h3>
      <div style={{ maxHeight: 180, overflowY: 'auto' }}>
        {state.chat.map((m, i) => <div key={i}><b>{nameOf(m.from)}:</b> {m.text}</div>)}
      </div>
      <form className="row" onSubmit={submit}>
        <input value={text} onChange={(e) => setText(e.target.value)} placeholder="Who's faking it?" />
        <button type="submit">Send</button>
      </form>
      {canEndDiscussion && <button onClick={onEnd}>Start voting</button>}
    </div>
  )
}
