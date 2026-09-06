import { useCallback, useMemo, useState } from 'react'
import { parseInviteCode } from '../invite.js'
import { loadCredentials } from '../credentials.js'
import { Landing } from '../features/landing/Landing.jsx'
import { RoomScreen } from './RoomScreen.jsx'

// App routes between the landing page and a room. An invite path (/join/CODE)
// with a saved seat reconnects straight in.
export default function App() {
  const initialCode = useMemo(() => parseInviteCode(location.pathname), [])
  const [entry, setEntry] = useState(() => {
    if (!initialCode) return null
    const creds = loadCredentials(initialCode)
    if (!creds) return null
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
  return <RoomScreen entry={entry} onLeave={leave} />
}
