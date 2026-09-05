import { useEffect, useRef, useReducer, useCallback, useState } from 'react'
import { reduce, initialState } from './reducer.js'
import { createRoomConnection } from './roomConnection.js'

// useRoomSocket is a thin React wrapper around one createRoomConnection
// instance: it owns the React state/reducer this hook exposes and starts/
// stops a fresh connection whenever `code`/`join` change, but the actual
// WebSocket lifecycle (connecting, backoff, sequencing, credential reuse)
// lives entirely in roomConnection.js, framework-agnostic and unit-tested
// on its own.
export function useRoomSocket(code, join) {
  const [state, dispatch] = useReducer(reduce, undefined, initialState)
  const [identity, setIdentity] = useState(null)
  const [connectionStatus, setConnectionStatus] = useState('connecting')
  const connRef = useRef(null)
  const subsRef = useRef(new Set())

  useEffect(() => {
    if (!code || !join) return
    setIdentity(null)
    setConnectionStatus('connecting')

    const conn = createRoomConnection(code, join, {
      onStatus: setConnectionStatus,
      onIdentity: setIdentity,
      // Every dispatched action (server envelope or local signal) both
      // updates the reducer and fans out to subscribe()rs — useVoice.js
      // is the one other consumer, reading raw envelopes for its own
      // independent voice_* protocol alongside the reducer.
      onDispatch: (action) => {
        dispatch(action)
        subsRef.current.forEach((fn) => fn(action))
      },
    })
    connRef.current = conn

    return () => {
      conn.stop()
      if (connRef.current === conn) connRef.current = null
    }
  }, [code, join])

  const send = useCallback((type, payload = {}) => {
    connRef.current?.send(type, payload)
  }, [])

  const subscribe = useCallback((fn) => {
    subsRef.current.add(fn)
    return () => subsRef.current.delete(fn)
  }, [])

  return { state, send, subscribe, identity, connectionStatus }
}
