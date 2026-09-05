import { useEffect, useRef, useReducer, useCallback, useState } from 'react'
import { reduce, initialState, LOCAL_JOIN_FAILED } from './reducer.js'
import { T } from './protocol.js'

export function useRoomSocket(code, join) {
  const [state, dispatch] = useReducer(reduce, undefined, initialState)
  const [identity, setIdentity] = useState(null)
  const wsRef = useRef(null)
  const subsRef = useRef(new Set())

  useEffect(() => {
    if (!code || !join) return
    setIdentity(null)
    let gotRoomState = false
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws/room/${code}`)
    wsRef.current = ws
    ws.onopen = () => ws.send(JSON.stringify({ type: 'join', payload: join }))
    ws.onmessage = (e) => {
      let msg
      try { msg = JSON.parse(e.data) } catch { return }
      if (msg.type === T.JoinAccepted) {
        const p = msg.payload || {}
        setIdentity({ playerId: p.playerId, reconnectToken: p.reconnectToken })
      }
      if (msg.type === T.RoomState) gotRoomState = true
      dispatch(msg)
      subsRef.current.forEach((fn) => fn(msg))
    }
    ws.onclose = () => {
      // The server closes the connection right after a rejected join or
      // reconnect (see room.JoinErrorCode) — if we never got as far as a
      // room_state snapshot, this close means the join failed outright,
      // not a mid-game network drop.
      if (!gotRoomState) dispatch({ type: LOCAL_JOIN_FAILED })
    }
    return () => ws.close()
  }, [code, join])

  const send = useCallback((type, payload = {}) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, payload }))
  }, [])

  const subscribe = useCallback((fn) => {
    subsRef.current.add(fn)
    return () => subsRef.current.delete(fn)
  }, [])

  return { state, send, subscribe, identity }
}
