// RoundSummary shows the per-player deltas for the round just resolved.
export function RoundSummary({ summary, nameOf }) {
  if (!summary) return null
  const rows = summary.players || []
  return (
    <div className="round-summary">
      <h3>Round {summary.round} summary</h3>
      <table className="summary-table">
        <thead>
          <tr>
            <th scope="col">Player</th>
            <th scope="col">Energy</th>
            <th scope="col">Influence</th>
            <th scope="col">Territory</th>
            <th scope="col">Armies lost</th>
            <th scope="col">Relics</th>
            <th scope="col">Faux</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.player}>
              <th scope="row">{nameOf(r.player)}</th>
              <td>{fmt(r.energyDelta)}</td>
              <td>{fmt(r.influenceDelta)}</td>
              <td>{fmt(r.territoryDelta)}</td>
              <td>{r.armiesLost}</td>
              <td>{r.relicsControlled}{r.dominationStreak >= 1 ? ` (streak ${r.dominationStreak})` : ''}</td>
              <td>{r.fauxUsed ? '🎭' : ''}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function fmt(n) {
  if (n > 0) return `+${n}`
  return `${n}`
}
