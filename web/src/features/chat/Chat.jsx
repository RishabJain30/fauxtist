import { useEffect, useRef, useState } from 'react'
import { Send } from 'lucide-react'

// Chat is the public room chat. Text is rendered as text content (never HTML),
// so nothing here needs sanitizing. Spectators can read but not post while a
// match is active — the input is hidden for read-only viewers.
export function Chat({ messages, nameOf, send, canPost, disabled }) {
  const [text, setText] = useState('')
  const listRef = useRef(null)

  useEffect(() => {
    const el = listRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [messages])

  function submit(e) {
    e.preventDefault()
    const t = text.trim()
    if (!t) return
    send('chat_message', { text: t })
    setText('')
  }

  return (
    <div className="chat">
      <div className="chat-list" ref={listRef} role="log" aria-label="Chat" aria-live="polite">
        {(messages || []).map((m, i) => (
          <div key={i} className="chat-msg">
            <span className="chat-from">{m.name || nameOf(m.from)}</span>
            <span className="chat-text">{m.text}</span>
          </div>
        ))}
        {(!messages || messages.length === 0) && <p className="muted small">No messages yet.</p>}
      </div>
      {canPost && (
        <form className="chat-form" onSubmit={submit}>
          <input
            className="chat-input"
            value={text}
            maxLength={300}
            placeholder="Say something…"
            disabled={disabled}
            onChange={(e) => setText(e.target.value)}
            aria-label="Chat message"
          />
          <button className="icon-btn" type="submit" aria-label="Send" disabled={disabled}>
            <Send size={16} />
          </button>
        </form>
      )}
    </div>
  )
}
