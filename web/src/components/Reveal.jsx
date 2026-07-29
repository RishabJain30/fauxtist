import { useState } from 'react'

export default function Reveal({ state, meId, send }) {
  const [guess, setGuess] = useState('')
  const r = state.lastResult || {}
  const impostorName = state.players.find((p) => p.id === r.impostorId)?.name || '?'
  const iAmImpostor = r.impostorId === meId
  const submit = (e) => { e.preventDefault(); if (guess.trim()) send('impostor_guess', { guess: guess.trim() }) }
  return (
    <div className="card col">
      <h3>{r.caught ? `Caught! ${impostorName} was the impostor.` : `${impostorName} got away with it!`}</h3>
      {r.caught && iAmImpostor && !r.impostorGuess && (
        <form className="col" onSubmit={submit}>
          <p>You're caught — guess the word to steal the win:</p>
          <div className="row">
            <input value={guess} onChange={(e) => setGuess(e.target.value)} placeholder="The secret word" />
            <button type="submit">Guess</button>
          </div>
        </form>
      )}
      {r.caught && !iAmImpostor && <p className="muted">Waiting for the impostor to guess the word…</p>}
      {r.word && <p className="muted">The word was: <b>{r.word}</b></p>}
    </div>
  )
}
