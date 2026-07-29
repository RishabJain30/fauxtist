export default function Lobby({ state, meId, code, onStart }) {
  const isHost = state.hostId === meId
  const enough = state.players.length >= 4
  return (
    <div className="center">
      <div className="card col">
        <h2>Room {code}</h2>
        <p className="muted">Share this code. Need 4–8 players.</p>
        <ul className="players">
          {state.players.map((p) => (
            <li key={p.id} className={p.id === meId ? 'me' : ''}>
              {p.name} {p.id === state.hostId && <span className="badge">host</span>}
            </li>
          ))}
        </ul>
        {isHost
          ? <button onClick={onStart} disabled={!enough}>{enough ? 'Start game' : 'Waiting for players…'}</button>
          : <p className="muted">Waiting for the host to start…</p>}
      </div>
    </div>
  )
}
