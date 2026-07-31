import { useMemo, useState } from 'react'
import { parseInviteCode } from './invite.js'
import { useRoomSocket } from './useRoomSocket.js'
import { useVoice } from './useVoice.js'
import Landing from './components/Landing.jsx'
import Lobby from './components/Lobby.jsx'
import GameBoard from './components/GameBoard.jsx'
import Chat from './components/Chat.jsx'
import Voting from './components/Voting.jsx'
import Reveal from './components/Reveal.jsx'
import GameOver from './components/GameOver.jsx'
import VoiceBar from './components/VoiceBar.jsx'

export default function App() {
  const initialCode = useMemo(() => parseInviteCode(location.pathname), [])
  const [entry, setEntry] = useState(null)
  if (!entry) return <Landing onEnter={setEntry} initialCode={initialCode} />
  return <Room entry={entry} />
}

function Room({ entry }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send, subscribe } = useRoomSocket(entry.code, join)
  const meId = useMemo(() => {
    if (join.reconnectToken) return join.reconnectToken
    return `${entry.code}-${join.name}`
  }, [entry, join])
  const voice = useVoice({ meId, send, subscribe })

  if (state.phase === 'connecting') {
    return <div className="center"><div className="card">Connecting…</div></div>
  }

  const isHost = state.hostId === meId
  let content
  if (state.phase === 'lobby') {
    content = <Lobby state={state} meId={meId} code={entry.code} onStart={() => send('start_game')} />
  } else if (state.phase === 'game_over') {
    content = <GameOver state={state} />
  } else {
    content = (
      <>
        {state.error && <div className="card" style={{ color: '#ff6b6b' }}>{state.error}</div>}
        {state.phase === 'drawing' && <GameBoard state={state} meId={meId} send={send} />}
        {state.phase === 'discussion' && (
          <Chat state={state} meId={meId} send={send} canEndDiscussion={isHost} onEnd={() => send('end_discussion')} />
        )}
        {state.phase === 'voting' && <Voting state={state} meId={meId} send={send} />}
        {state.phase === 'reveal' && <Reveal state={state} meId={meId} send={send} />}
      </>
    )
  }

  return (
    <div className="center">
      <div className="col" style={{ width: 'min(880px,94vw)' }}>
        <VoiceBar voice={voice} state={state} meId={meId} />
        {content}
      </div>
    </div>
  )
}
