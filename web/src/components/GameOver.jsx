export default function GameOver({ state }) {
  const scores = [...(state.finalScores || [])].sort((a, b) => b.score - a.score)
  return (
    <div className="center">
      <div className="card col">
        <h2>Game over</h2>
        <ol className="players">
          {scores.map((p) => <li key={p.id}>{p.name} — {p.score}</li>)}
        </ol>
        <button onClick={() => location.reload()}>Play again</button>
      </div>
    </div>
  )
}
