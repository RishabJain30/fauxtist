import Canvas from './Canvas.jsx'

export default function GameBoard({ state, meId, send }) {
  const myTurn = state.currentPlayer === meId && state.phase === 'drawing'
  const drawerName = state.players.find((p) => p.id === state.currentPlayer)?.name || '…'
  return (
    <div className="card col">
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <span>Round {state.round}/{state.totalRounds} · Lap {state.lap + 1}/{state.totalLaps}</span>
        <span className="badge">
          {state.youAreImpostor ? `You are the IMPOSTOR — category: ${state.category}` : `Word: ${state.word}`}
        </span>
      </div>
      <p className="muted">{myTurn ? 'Your turn — draw ONE stroke' : `${drawerName} is drawing…`}</p>
      <Canvas
        strokes={state.strokes}
        canDraw={myTurn}
        onStrokeComplete={(s) => send('stroke', s)}
      />
    </div>
  )
}
