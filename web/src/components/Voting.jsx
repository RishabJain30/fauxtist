import { useState } from 'react'

export default function Voting({ state, meId, send }) {
  const [voted, setVoted] = useState(null)
  const cast = (target) => { send('cast_vote', { target }); setVoted(target) }

  return (
    <div className="card col pop-in">
      <div className="label">Vote for the impostor</div>
      <p className="muted">{state.votesCast}/{state.votesTotal} voted{voted ? ' · your vote is in' : ''}</p>
      <div className="col" style={{ gap: 10 }}>
        {state.players.map((p) => {
          const isMe = p.id === meId
          const picked = voted === p.id
          return (
            <button
              key={p.id}
              className="player"
              disabled={!!voted || isMe}
              onClick={() => cast(p.id)}
              style={{
                justifyContent: 'flex-start',
                background: picked ? 'var(--amber)' : '#fff',
                color: 'var(--ink)', fontSize: 18,
              }}
            >
              <span className="avatar">{p.emoji || '🎭'}</span>
              {p.name}{isMe ? ' (you)' : ''}{picked ? ' ✓' : ''}
            </button>
          )
        })}
      </div>
    </div>
  )
}
