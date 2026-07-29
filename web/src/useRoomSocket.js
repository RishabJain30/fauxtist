import { useEffect, useRef, useReducer, useCallback } from 'react'
import { reduce, initialState } from './reducer.js'

export function useRoomSocket(code, join) {
  const [state, dispatch] = useReducer(reduce, undefined, initialState)
  const wsRef = useRef(null)

  useEffect(() => {
    if (!code || !join) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws/room/${code}`)
    wsRef.current = ws
    ws.onopen = () => ws.send(JSON.stringify({ type: 'join', payload: join }))
    ws.onmessage = (e) => {
      try { dispatch(JSON.parse(e.data)) } catch { /* ignore malformed */ }
    }
    return () => ws.close()
  }, [code, join])

  const send = useCallback((type, payload = {}) => {
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type, payload }))
  }, [])

  return { state, send }
}
