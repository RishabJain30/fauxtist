import { T, encodeCommand, parseServerMessage } from './protocol.js'
import { decideSequence } from './sequencing.js'
import { loadCredentials, saveCredentials, clearCredentials } from './credentials.js'
import { LOCAL_JOIN_FAILED, LOCAL_VOTE_CAST, STATE_SNAPSHOT_RECEIVED } from './reducer.js'

// Roughly immediate, 500ms, 1s, 2s, 4s, then capped at 10s — each with
// jitter applied so many clients dropped by the same event don't all
// retry in lockstep.
export const BACKOFF_DELAYS_MS = [0, 500, 1000, 2000, 4000, 10000]

// Stop auto-retrying once we've been unable to reconnect for longer than
// the server's own reconnect grace period (its default — see
// internal/room/durations.go): past that point the seat may already be
// gone, so continuing to retry silently forever would be misleading.
export const RECONNECT_GRACE_MS = 60_000

// Error codes that mean retrying is pointless: the seat, name, or room
// itself is the problem, not the connection.
export const FATAL_ERROR_CODES = new Set([
  'invalid_reconnect',
  'name_taken',
  'room_full',
  'game_started',
  'invalid_join',
  'unsupported_version',
  'room_closed',
])

// FATAL_CLOSE_CODES are raw WebSocket close codes that mean "don't retry"
// even with no preceding Error envelope — the room expired or the process
// is shutting down (4003), or the join frame itself was rejected at the
// protocol level (4001/4002) before any Error envelope could help.
const FATAL_CLOSE_CODES = new Set([4001, 4002, 4003])

export function backoffDelay(attempt, random = Math.random) {
  const base = BACKOFF_DELAYS_MS[Math.min(attempt, BACKOFF_DELAYS_MS.length - 1)]
  if (base === 0) return 0
  return Math.min(base * (0.5 + random()), 10000)
}

function defaultUrlFor(code) {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${location.host}/ws/room/${code}`
}

const noopEventTarget = { addEventListener() {}, removeEventListener() {} }

// createRoomConnection owns one room's WebSocket connection lifecycle end
// to end: connecting, applying snapshots/incremental events through the
// sequencer, and detecting and recovering from drops with backoff.
// Framework-agnostic on purpose — useRoomSocket.js is a thin React
// wrapper around one instance of this, and this module's behavior is
// unit-tested directly with a fake WebSocket and fake timers, without any
// DOM or React rendering, matching the rest of this codebase's tests.
//
// `handlers` — { onStatus(status), onDispatch(action), onIdentity(identity|null) } —
// are how this reports back to whoever owns it; nothing here reads or
// writes React state directly.
//
// `deps` lets tests substitute the WebSocket constructor, the clock, the
// credential store, the online/offline event source, and the URL
// builder; every one defaults to the real browser global.
export function createRoomConnection(code, join, handlers, deps = {}) {
  const {
    WebSocketImpl = globalThis.WebSocket,
    setTimeoutImpl = setTimeout,
    clearTimeoutImpl = clearTimeout,
    now = Date.now,
    random = Math.random,
    storage,
    urlFor = defaultUrlFor,
    onlineEvents = typeof window !== 'undefined' ? window : noopEventTarget,
  } = deps
  const { onStatus, onDispatch, onIdentity } = handlers

  let stopped = false
  let ws = null
  let timer = null
  let attempt = 0
  let everConnected = false
  let firstDisconnectAt = null
  let intentional = false
  let fatal = false
  let lastAppliedSeq = null
  let resyncing = false
  // generation increments once per actual new WebSocket (connect() call) —
  // never for a resync, which reuses the existing socket. Carried on every
  // dispatched state_snapshot action so a consumer (useVoice.js) can tell
  // "this snapshot confirms a brand-new connection, presence server-side
  // was reset" apart from "this snapshot is just a resync refresh".
  let generation = 0
  let status = 'connecting'
  let creds = loadCredentials(code, storage) || join

  function setStatus(s) {
    if (stopped) return
    status = s
    onStatus(s)
  }

  function joinPayload() {
    return creds.playerId
      ? { playerId: creds.playerId, reconnectToken: creds.reconnectToken }
      : { name: creds.name, emoji: creds.emoji }
  }

  function scheduleRetry() {
    if (stopped || intentional || fatal) return
    if (firstDisconnectAt == null) firstDisconnectAt = now()
    if (now() - firstDisconnectAt > RECONNECT_GRACE_MS) {
      setStatus('failed')
      return
    }
    setStatus(everConnected ? 'reconnecting' : 'connecting')
    const delay = backoffDelay(attempt, random)
    attempt += 1
    timer = setTimeoutImpl(connect, delay)
  }

  function connect() {
    if (stopped) return
    generation += 1
    const socket = new WebSocketImpl(urlFor(code))
    ws = socket

    socket.onopen = () => {
      if (stopped || ws !== socket) return
      const { raw } = encodeCommand(T.Join, joinPayload())
      socket.send(raw)
    }

    socket.onmessage = (e) => {
      if (stopped || ws !== socket) return
      const env = parseServerMessage(e.data)
      if (!env) return

      if (env.type === T.JoinAccepted) {
        const { playerId, reconnectToken } = env.payload
        creds = { playerId, reconnectToken }
        saveCredentials(code, playerId, reconnectToken, storage)
        onIdentity({ playerId, reconnectToken })
        return
      }

      if (env.type === T.Error && FATAL_ERROR_CODES.has(env.payload.code)) {
        fatal = true
        if (env.payload.code === 'invalid_reconnect') clearCredentials(code, storage)
        onDispatch(env)
        return
      }

      if (env.type === T.StateSnapshot) {
        if (decideSequence(lastAppliedSeq, env) === 'stale-snapshot') return
        lastAppliedSeq = env.seq
        resyncing = false
        everConnected = true
        attempt = 0
        firstDisconnectAt = null
        onDispatch({ type: STATE_SNAPSHOT_RECEIVED, payload: env.payload, generation })
        setStatus('connected')
        return
      }

      const verdict = decideSequence(lastAppliedSeq, env)
      if (verdict === 'gap') {
        resyncing = true
        setStatus('resyncing')
        const { raw } = encodeCommand(T.Resync, {})
        socket.send(raw)
        return
      }
      if (verdict === 'duplicate-or-old') return
      if (resyncing) return // mid-resync: drop incrementals until the fresh snapshot lands
      if (typeof env.seq === 'number') lastAppliedSeq = env.seq
      onDispatch(env)
    }

    socket.onclose = (event) => {
      if (stopped || ws !== socket) return
      if (FATAL_CLOSE_CODES.has(event?.code)) fatal = true
      if (fatal) {
        if (!everConnected) onDispatch({ type: LOCAL_JOIN_FAILED })
        setStatus('failed')
        return
      }
      if (intentional) {
        setStatus('closed')
        return
      }
      scheduleRetry()
    }
  }

  function onOnline() {
    if (stopped || intentional || fatal || !timer) return
    clearTimeoutImpl(timer)
    timer = null
    connect()
  }
  onlineEvents.addEventListener('online', onOnline)

  connect()

  return {
    // send encodes and writes one command if (and only if) the connection
    // is fully up — no queueing while disconnected/resyncing, per the
    // reliability milestone's explicit requirement: replaying a stroke or
    // vote later could produce an invalid or duplicated transition.
    send(type, payload = {}) {
      if (status !== 'connected' || !ws || ws.readyState !== WebSocketImpl.OPEN) return
      const { raw } = encodeCommand(type, payload)
      ws.send(raw)
      if (type === T.CastVote) onDispatch({ type: LOCAL_VOTE_CAST })
    },
    stop() {
      stopped = true
      intentional = true
      if (timer) clearTimeoutImpl(timer)
      onlineEvents.removeEventListener('online', onOnline)
      ws?.close(1000, 'leaving')
    },
  }
}
