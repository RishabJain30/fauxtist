import { useCallback, useMemo, useState } from 'react'
import { parseInviteCode } from './invite.js'
import { loadCredentials, clearCredentials } from './credentials.js'
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

// Statuses where the room screen (last known snapshot) still renders, just
// with a small overlay banner and gameplay disabled — as opposed to
// 'connecting' on a session that has never yet reached a snapshot, or
// 'failed', which have nothing else worth showing behind them.
const TEMPORARY_STATUSES = new Set(['reconnecting', 'resyncing'])

function Room({ entry, onLeave }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send, subscribe, identity, connectionStatus } = useRoomSocket(entry.code, join)
  const meId = identity?.playerId ?? entry.join.playerId ?? null

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
  if (connectionStatus === 'failed') {
    return (
      <div className="center">
        <div className="card col">
          <p>Lost the connection to this room and couldn't reconnect.</p>
          <button className="btn-primary" onClick={leaveRoom}>Back to start</button>
        </div>
      </div>
    )
  }

  const isHost = state.hostId === meId
  const disabled = connectionStatus !== 'connected'
  let content
  if (state.phase === 'lobby') {
    content = <Lobby state={state} meId={meId} code={entry.code} onStart={() => send('start_game')} disabled={disabled} />
  } else if (state.phase === 'game_over') {
    content = <GameOver state={state} meId={meId} send={send} onLeave={leaveRoom} disabled={disabled} />
  } else {
    content = (
      <>
        {state.error && <div className="card" style={{ color: '#ff6b6b' }}>{state.error}</div>}
        {state.phase === 'drawing' && <GameBoard state={state} meId={meId} send={send} disabled={disabled} />}
        {state.phase === 'discussion' && (
          <Chat state={state} meId={meId} send={send} canEndDiscussion={isHost} onEnd={() => send('end_discussion')} disabled={disabled} />
        )}
        {state.phase === 'voting' && <Voting state={state} meId={meId} send={send} disabled={disabled} />}
        {state.phase === 'reveal' && <Reveal state={state} meId={meId} send={send} disabled={disabled} />}
      </>
    )
  }

  return (
    <div className="center">
      <div className="col" style={{ width: 'min(760px,94vw)' }}>
        {TEMPORARY_STATUSES.has(connectionStatus) && (
          <div className="card" style={{ textAlign: 'center', padding: 10 }}>
            {connectionStatus === 'resyncing' ? 'Syncing…' : 'Reconnecting…'}
          </div>
        )}
        <VoiceBar voice={voice} state={state} meId={meId} />
        {content}
      </div>
    </div>
  )
}
