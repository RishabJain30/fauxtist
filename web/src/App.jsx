import { useMemo, useState } from 'react'
import { parseInviteCode } from './invite.js'
import { useRoomSocket } from './useRoomSocket.js'
import Landing from './components/Landing.jsx'
import Lobby from './components/Lobby.jsx'
import GameBoard from './components/GameBoard.jsx'
import Chat from './components/Chat.jsx'
import Voting from './components/Voting.jsx'
import Reveal from './components/Reveal.jsx'
import GameOver from './components/GameOver.jsx'

export default function App() {
  const initialCode = useMemo(() => parseInviteCode(location.pathname), [])
  const [entry, setEntry] = useState(null) // { code, join }
  if (!entry) return <Landing onEnter={setEntry} initialCode={initialCode} />
  return <Room entry={entry} />
}

function Room({ entry }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send } = useRoomSocket(entry.code, join)

  const meId = useMemo(() => {
    if (join.reconnectToken) return join.reconnectToken
    return `${entry.code}-${join.name}`
  }, [entry, join])

  if (state.phase === 'connecting') return <div className="center"><div className="card">Connecting…</div></div>
  if (state.phase === 'lobby') return <Lobby state={state} meId={meId} code={entry.code} onStart={() => send('start_game')} />
  if (state.phase === 'game_over') return <GameOver state={state} />

  const isHost = state.hostId === meId
  return (
    <div className="center">
      <div className="col" style={{ width: 'min(880px,94vw)' }}>
        {state.error && <div className="card" style={{ color: '#ff6b6b' }}>{state.error}</div>}
        {(state.phase === 'drawing') && <GameBoard state={state} meId={meId} send={send} />}
        {state.phase === 'discussion' && (
          <Chat state={state} meId={meId} send={send} canEndDiscussion={isHost} onEnd={() => send('end_discussion')} />
        )}
        {state.phase === 'voting' && <Voting state={state} meId={meId} send={send} />}
        {state.phase === 'reveal' && <Reveal state={state} meId={meId} send={send} />}
      </div>
    </div>
  )
}
