import { useCallback, useEffect, useMemo } from 'react'
import { useRoomSocket } from '../useRoomSocket.js'
import { useVoice } from '../useVoice.js'
import { clearCredentials } from '../credentials.js'
import { useLocalPrefs, prefersReducedMotion } from '../shared/hooks/useLocalPrefs.js'
import { initAudio, setSoundEnabled, setSfxVolume, play } from '../shared/audio/sfx.js'
import { Lobby } from '../features/lobby/Lobby.jsx'
import { GameScreen } from '../features/game/GameScreen.jsx'
import { BRAND } from './brand.js'

const TEMPORARY_STATUSES = new Set(['reconnecting', 'resyncing'])

// RoomScreen owns one room session: the socket, voice, local preferences, and
// the top-level connection overlays. It renders the lobby or the game screen.
export function RoomScreen({ entry, onLeave }) {
  const join = useMemo(() => entry.join, [entry])
  const { state, send, subscribe, identity, connectionStatus } = useRoomSocket(entry.code, join)
  const meId = identity?.playerId ?? entry.join.playerId ?? null
  const [rawPrefs, setPref] = useLocalPrefs()
  const prefs = useMemo(() => ({ ...rawPrefs, reducedMotion: prefersReducedMotion(rawPrefs) }), [rawPrefs])

  // Apply sound settings and initialize audio on the first user gesture
  // (browser autoplay policy).
  useEffect(() => {
    setSoundEnabled(prefs.sound)
    setSfxVolume(prefs.sfxVolume)
  }, [prefs.sound, prefs.sfxVolume])
  useEffect(() => {
    const once = () => {
      initAudio()
      window.removeEventListener('pointerdown', once)
    }
    window.addEventListener('pointerdown', once)
    return () => window.removeEventListener('pointerdown', once)
  }, [])

  const sfx = useCallback((name) => play(name), [])

  const voice = useVoice({ meId, send, subscribe })

  const leaveForNow = useCallback(() => {
    send('leave_for_now')
    onLeave() // keep credentials — the landing page offers Resume
  }, [send, onLeave])

  const resign = useCallback(() => {
    send('resign_match')
    clearCredentials(entry.code)
    onLeave()
  }, [send, entry.code, onLeave])

  const leavePermanent = useCallback(() => {
    clearCredentials(entry.code)
    onLeave()
  }, [entry.code, onLeave])

  if (state.phase === 'connecting') {
    return <Centered><p>Waking the server and connecting to <strong>{entry.code}</strong>…</p><p className="muted small">A free-tier server can take up to a minute to wake.</p></Centered>
  }
  if (state.phase === 'join_failed') {
    return (
      <Centered>
        <p>{state.error || 'Could not join this room.'}</p>
        <button className="btn-primary" onClick={leavePermanent}>Back to start</button>
      </Centered>
    )
  }
  if (connectionStatus === 'failed') {
    return (
      <Centered>
        <p>Lost the connection to this room and couldn&apos;t reconnect.</p>
        <button className="btn-primary" onClick={leavePermanent}>Back to start</button>
      </Centered>
    )
  }

  const disabled = connectionStatus !== 'connected'

  return (
    <div className={`room-root ${prefs.highContrast ? 'high-contrast' : ''}`}>
      {TEMPORARY_STATUSES.has(connectionStatus) && (
        <div className="conn-overlay" role="status" aria-live="polite">
          {connectionStatus === 'resyncing' ? 'Syncing…' : 'Reconnecting…'}
        </div>
      )}
      {state.phase === 'lobby' ? (
        <Lobby state={state} meId={meId} code={entry.code} send={send} onLeave={leavePermanent} disabled={disabled} />
      ) : (
        <GameScreen
          state={state}
          meId={meId}
          send={send}
          subscribe={subscribe}
          voice={voice}
          prefs={prefs}
          setPref={setPref}
          sfx={sfx}
          onLeaveForNow={leaveForNow}
          onResign={resign}
          disabled={disabled}
        />
      )}
    </div>
  )
}

function Centered({ children }) {
  return (
    <div className="center">
      <div className="card col centered-card">
        <h1 className="brand-title small-title">{BRAND.name}</h1>
        {children}
      </div>
    </div>
  )
}
