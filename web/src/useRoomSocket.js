import { useEffect, useRef, useReducer, useCallback } from 'react'
import { reduce, initialState } from './reducer.js'

export function useRoomSocket(code, join) {
  const [state, dispatch] = useReducer(reduce, undefined, initialState)
  const wsRef = useRef(null)
  const subsRef = useRef(new Set())

  useEffect(() => {
    if (!code || !join) return
    const proto = location.protocol === 'https:' ? 'wss' : 'ws'
    const ws = new WebSocket(`${proto}://${location.host}/ws/room/${code}`)
    wsRef.current = ws
    ws.onopen = () => ws.send(JSON.stringify({ type: 'join', payload: join }))
    ws.onmessage = (e) => {
      let msg
      try { msg = JSON.parse(e.data) } catch { return }
      dispatch(msg)
      subsRef.current.forEach((fn) => fn(msg))
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

  return { state, send, subscribe }
}
