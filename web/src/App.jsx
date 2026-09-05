import { useCallback, useEffect, useMemo, useState } from 'react'
import { parseInviteCode } from './invite.js'
import { loadCredentials, saveCredentials, clearCredentials } from './credentials.js'
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
  const [entry, setEntry] = useState(() => {
    if (!initialCode) return null
    const creds = loadCredentials(initialCode)
    if (!creds) return null
    // A stored seat for this exact code (e.g. after a refresh, since we
    // rewrite the URL to /join/<code> on entry): reconnect straight in
    // rather than showing Landing and asking for a name again.
    return { code: initialCode, join: { playerId: creds.playerId, reconnectToken: creds.reconnectToken } }
  })

  const enter = useCallback((e) => {
    history.replaceState(null, '', `/join/${e.code}`)
    setEntry(e)
  }, [])

  const leave = useCallback(() => {
    history.replaceState(null, '', '/')
    setEntry(null)
  }, [])

  if (!entry) return <Landing onEnter={enter} initialCode={initialCode} />
  return <Room entry={entry} onLeave={leave} />
}

function Room({ entry, onLeave }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send, subscribe, identity } = useRoomSocket(entry.code, join)
  const meId = identity?.playerId ?? entry.join.playerId ?? null

  useEffect(() => {
    if (identity) saveCredentials(entry.code, identity.playerId, identity.reconnectToken)
  }, [entry.code, identity])

  useEffect(() => {
    if (state.phase === 'join_failed') clearCredentials(entry.code)
  }, [state.phase, entry.code])

  const leaveRoom = useCallback(() => {
    clearCredentials(entry.code)
    onLeave()
  }, [entry.code, onLeave])

  const voice = useVoice({ meId, send, subscribe })

  if (state.phase === 'connecting') {
    return <div className="center"><div className="card">Connecting…</div></div>
  }
  if (state.phase === 'join_failed') {
    return (
      <div className="center">
        <div className="card col">
          <p>{state.error || 'Could not join this room.'}</p>
          <button className="btn-primary" onClick={leaveRoom}>Back to start</button>
        </div>
      </div>
    )
  }

  const isHost = state.hostId === meId
  let content
  if (state.phase === 'lobby') {
    content = <Lobby state={state} meId={meId} code={entry.code} onStart={() => send('start_game')} />
  } else if (state.phase === 'game_over') {
    content = <GameOver state={state} meId={meId} send={send} onLeave={leaveRoom} />
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
      <div className="col" style={{ width: 'min(760px,94vw)' }}>
        <VoiceBar voice={voice} state={state} meId={meId} />
        {content}
      </div>
    </div>
  )
}
