import Canvas from './Canvas.jsx'

export default function GameBoard({ state, meId, send, disabled }) {
  const myTurn = state.currentPlayer === meId && state.phase === 'drawing' && !disabled
  const drawer = state.players.find((p) => p.id === state.currentPlayer)
  const miniAvatar = { width: 26, height: 26, fontSize: 15, borderRadius: 8 }

  return (
    <div className="card col pop-in">
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <span className="pill">Round {state.round}/{state.totalRounds} · Lap {state.lap + 1}/{state.totalLaps}</span>
        {state.youAreImpostor
          ? <span className="badge" style={{ background: 'var(--coral)', color: '#fff' }}>🕵️ You're the IMPOSTOR · {state.category}</span>
          : <span className="badge">Word: {state.word}</span>}
      </div>
      <div className="row" style={{ justifyContent: 'center' }}>
        {myTurn
          ? <span className="pill" style={{ background: 'var(--amber)' }}>✏️ Your turn — draw ONE stroke</span>
          : (
            <span className="pill">
              <span className="avatar" style={miniAvatar}>{drawer?.emoji || '🎭'}</span> {drawer?.name || '…'} is drawing…
              {drawer?.connected === false && <span className="muted"> (reconnecting…)</span>}
            </span>
          )}
      </div>
      <Canvas strokes={state.strokes} canDraw={myTurn} onStrokeComplete={(s) => send('stroke', s)} />
    </div>
  )
}
