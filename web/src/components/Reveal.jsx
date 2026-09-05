import { useEffect, useState } from 'react'

export default function Reveal({ state, meId, send }) {
  const [guess, setGuess] = useState('')
  const r = state.lastResult || {}
  const impostor = state.players.find((p) => p.id === r.impostorId)
  const iAmImpostor = r.impostorId === meId
  const awaitingGuess = r.caught && !r.impostorGuess && !r.impostorTimedOut
  const submit = (e) => { e.preventDefault(); if (guess.trim()) send('impostor_guess', { guess: guess.trim() }) }
  const secondsLeft = useCountdown(awaitingGuess ? r.guessDeadlineMs : null)

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

      {awaitingGuess && iAmImpostor && (
        <form className="col" onSubmit={submit} style={{ alignItems: 'center' }}>
          <p className="muted">
            You're caught — guess the word to steal the win
            {secondsLeft != null && <>: <b>{secondsLeft}s</b> left</>}
          </p>
          <div className="row">
            <input value={guess} onChange={(e) => setGuess(e.target.value)} placeholder="The secret word" />
            <button type="submit">Guess</button>
          </div>
        </form>
      )}
      {awaitingGuess && !iAmImpostor && (
        <p className="muted">
          Waiting for the impostor to guess the word{secondsLeft != null && <> ({secondsLeft}s left)</>}…
        </p>
      )}
      {r.caught && r.impostorTimedOut && <p className="muted">The impostor ran out of time to guess.</p>}
      {r.word && <p className="muted">The word was: <b>{r.word}</b></p>}
    </div>
  )
}

// useCountdown ticks down the seconds remaining until deadlineMs (an
// absolute epoch-ms timestamp from the server), or returns null when
// there's no deadline to show.
function useCountdown(deadlineMs) {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!deadlineMs) return
    const id = setInterval(() => setNow(Date.now()), 250)
    return () => clearInterval(id)
  }, [deadlineMs])
  if (!deadlineMs) return null
  return Math.max(0, Math.ceil((deadlineMs - now) / 1000))
}
