import { useEffect } from 'react'
import confetti from 'canvas-confetti'

export default function GameOver({ state }) {
  const scores = [...(state.finalScores || [])].sort((a, b) => b.score - a.score)
  const top = scores.length ? scores[0].score : 0

  useEffect(() => {
    if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) return
    const end = Date.now() + 900
    const tick = () => {
      confetti({ particleCount: 5, spread: 70, origin: { y: 0.6 }, colors: ['#ff6b6b', '#7c5cff', '#22c1a4', '#ffc93c'] })
      if (Date.now() < end) requestAnimationFrame(tick)
    }
    tick()
  }, [])

  return (
    <div className="card col pop-in" style={{ textAlign: 'center', alignItems: 'center' }}>
      <div style={{ fontSize: 56 }}>🏆</div>
      <h2 style={{ margin: 0 }}>Game over</h2>
      <ol className="players" style={{ width: '100%' }}>
        {scores.map((p, i) => (
          <li key={p.id} className="player" style={{ background: p.score === top ? 'var(--amber)' : '#fff' }}>
            <span className="avatar">{p.emoji || '🎭'}</span>
            <span style={{ fontWeight: p.score === top ? 700 : 500 }}>
              {i === 0 ? '👑 ' : `${i + 1}. `}{p.name}
            </span>
            <span className="badge" style={{ marginLeft: 'auto' }}>{p.score} pts</span>
          </li>
        ))}
      </ol>
      <button className="btn-primary" onClick={() => location.assign('/')}>Play again</button>
    </div>
  )
}
