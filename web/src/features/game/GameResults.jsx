import { useEffect } from 'react'
import confetti from 'canvas-confetti'
import { BRAND } from '../../app/brand.js'

const REASON_LABELS = {
  domination: 'Domination Victory',
  influence: 'Victory by Influence',
  forfeit: 'Victory by Forfeit',
  shared: 'Shared Victory',
  no_contest: 'No Contest',
}

// GameResults is the Game Over screen: winner(s), final standings, match
// highlights, influence history, and the rematch / lobby / leave controls.
export function GameResults({ result, isHost, meId, rematchReady, nameOf, send, onLeave, disabled, reducedMotion, sfx }) {
  const won = result?.winners?.includes(meId)

  useEffect(() => {
    if (won && !reducedMotion) {
      sfx?.('victory')
      confetti({ particleCount: 120, spread: 75, origin: { y: 0.4 } })
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  if (!result) return <div className="card">Tallying the final board…</div>

  const winners = (result.winners || []).map(nameOf)
  const banner =
    result.reason === 'no_contest'
      ? 'No contest — the match ended without a winner.'
      : winners.length === 0
        ? REASON_LABELS[result.reason]
        : `${winners.join(' & ')} — ${REASON_LABELS[result.reason] || 'Victory'}`

  const iAmReady = rematchReady?.includes(meId)

  return (
    <div className="game-results">
      <h2 className="results-banner">{banner}</h2>

      <h3>Final standings</h3>
      <table className="summary-table">
        <thead>
          <tr>
            <th scope="col">#</th>
            <th scope="col">Player</th>
            <th scope="col">Influence</th>
            <th scope="col">Relics</th>
            <th scope="col">Territory</th>
            <th scope="col">Armies</th>
            <th scope="col">Energy</th>
          </tr>
        </thead>
        <tbody>
          {(result.standings || []).map((s) => (
            <tr key={s.player} className={s.player === meId ? 'row-me' : ''}>
              <td>{s.rank}</td>
              <th scope="row">{nameOf(s.player)}{s.forfeited ? ' (left)' : ''}</th>
              <td>{s.influence}</td>
              <td>{s.relicsControlled}</td>
              <td>{s.territories}</td>
              <td>{s.armies}</td>
              <td>{s.energy}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <details className="results-highlights">
        <summary>Match highlights</summary>
        <ul>
          {Object.entries(result.stats || {}).map(([pid, st]) => (
            <li key={pid}>
              <strong>{nameOf(pid)}</strong>: {st.captures} captures, {st.armiesLost} armies lost, {st.fortressesBuilt} fortresses, {st.minesBuilt} mines
              {st.fauxUsedRound ? `, Faux in round ${st.fauxUsedRound}` : ''}
            </li>
          ))}
        </ul>
      </details>

      <div className="results-actions">
        <button className={`btn-primary ${iAmReady ? 'is-on' : ''}`} disabled={disabled} onClick={() => send('rematch_ready')}>
          {iAmReady ? 'Ready for rematch ✓' : 'Play again'}
        </button>
        {isHost && (
          <button className="btn-secondary" disabled={disabled} onClick={() => send('start_rematch')}>
            Start rematch
          </button>
        )}
        {isHost && (
          <button className="btn-secondary" disabled={disabled} onClick={() => send('return_to_lobby')}>
            Back to lobby
          </button>
        )}
        <button className="btn-ghost" onClick={onLeave}>Leave</button>
      </div>
      <p className="muted small">{rematchReady?.length || 0} ready for another {BRAND.name} match.</p>
    </div>
  )
}
