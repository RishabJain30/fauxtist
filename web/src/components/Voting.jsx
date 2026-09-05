import { useState } from 'react'

export default function Voting({ state, meId, send, disabled }) {
  const [picked, setPicked] = useState(null)
  const voted = state.hasVoted
  const cast = (target) => { send('cast_vote', { target }); setPicked(target) }

  return (
    <div className="card col pop-in">
      <div className="label">Vote for the impostor</div>
      <p className="muted">{state.votesCast}/{state.votesTotal} voted{voted ? ' · your vote is in' : ''}</p>
      <div className="col" style={{ gap: 10 }}>
        {state.players.map((p) => {
          const isMe = p.id === meId
          const isPicked = picked === p.id
          return (
            <button
              key={p.id}
              className="player"
              disabled={disabled || voted || isMe}
              onClick={() => cast(p.id)}
              style={{
                justifyContent: 'flex-start',
                background: isPicked ? 'var(--amber)' : '#fff',
                color: 'var(--ink)', fontSize: 18,
                opacity: p.connected === false ? 0.5 : 1,
              }}
            >
              <span className="avatar">{p.emoji || '🎭'}</span>
              {p.name}{isMe ? ' (you)' : ''}{isPicked ? ' ✓' : ''}
              {p.connected === false && <span className="muted"> (reconnecting…)</span>}
            </button>
          )
        })}
      </div>
    </div>
  )
}
