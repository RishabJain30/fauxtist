import { useState } from 'react'

export default function Reveal({ state, meId, send }) {
  const [guess, setGuess] = useState('')
  const r = state.lastResult || {}
  const impostor = state.players.find((p) => p.id === r.impostorId)
  const iAmImpostor = r.impostorId === meId
  const submit = (e) => { e.preventDefault(); if (guess.trim()) send('impostor_guess', { guess: guess.trim() }) }

  return (
    <div className="card col pop-in" style={{ textAlign: 'center', alignItems: 'center' }}>
      <div style={{ fontSize: 56 }}>{r.caught ? '🎯' : '🕵️‍♂️'}</div>
      <h2 style={{ margin: 0 }}>
        {r.caught ? 'Caught!' : 'Got away with it!'}
      </h2>
      <div className="row" style={{ justifyContent: 'center' }}>
        <span className="avatar">{impostor?.emoji || '🎭'}</span>
        <span><b>{impostor?.name || '?'}</b> was the impostor</span>
      </div>

      {r.caught && iAmImpostor && !r.impostorGuess && (
        <form className="col" onSubmit={submit} style={{ alignItems: 'center' }}>
          <p className="muted">You're caught — guess the word to steal the win:</p>
          <div className="row">
            <input value={guess} onChange={(e) => setGuess(e.target.value)} placeholder="The secret word" />
            <button type="submit">Guess</button>
          </div>
        </form>
      )}
      {r.caught && !iAmImpostor && !r.impostorGuess && <p className="muted">Waiting for the impostor to guess the word…</p>}
      {r.word && <p className="muted">The word was: <b>{r.word}</b></p>}
    </div>
  )
}
