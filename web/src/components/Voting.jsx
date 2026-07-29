import { useState } from 'react'

export default function Voting({ state, meId, send }) {
  const [voted, setVoted] = useState(false)
  const cast = (target) => { send('cast_vote', { target }); setVoted(true) }
  return (
    <div className="card col">
      <h3>Vote for the impostor</h3>
      <p className="muted">{state.votesCast}/{state.votesTotal} voted</p>
      <div className="col">
        {state.players.map((p) => (
          <button key={p.id} disabled={voted || p.id === meId} onClick={() => cast(p.id)}>{p.name}</button>
        ))}
      </div>
    </div>
  )
}
