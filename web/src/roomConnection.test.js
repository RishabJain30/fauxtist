import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createRoomConnection, backoffDelay, BACKOFF_DELAYS_MS } from './roomConnection.js'
import { T, PROTOCOL_VERSION } from './protocol.js'
import { STATE_SNAPSHOT_RECEIVED, LOCAL_JOIN_FAILED, LOCAL_VOTE_CAST } from './reducer.js'

// A minimal fake WebSocket standing in for the real browser API: tests
// drive it explicitly (open/message/serverClose) instead of a real socket
// ever existing, so the whole suite runs with no network and (combined
// with fake timers) no real waiting.
class FakeWebSocket {
  constructor(url) {
    this.url = url
    this.readyState = FakeWebSocket.CONNECTING
    this.sent = []
    FakeWebSocket.instances.push(this)
  }
  send(data) {
    this.sent.push(data)
  }
  close() {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code: 1000 })
  }
  open() {
    this.readyState = FakeWebSocket.OPEN
    this.onopen?.()
  }
  message(envelope) {
    this.onmessage?.({ data: JSON.stringify({ version: PROTOCOL_VERSION, ...envelope }) })
  }
  serverClose(code = 1006) {
    this.readyState = FakeWebSocket.CLOSED
    this.onclose?.({ code })
  }
}
FakeWebSocket.CONNECTING = 0
FakeWebSocket.OPEN = 1
FakeWebSocket.CLOSING = 2
FakeWebSocket.CLOSED = 3

function memoryStorage() {
  const m = new Map()
  return {
    getItem: (k) => (m.has(k) ? m.get(k) : null),
    setItem: (k, v) => m.set(k, v),
    removeItem: (k) => m.delete(k),
  }
}

function fakeEventTarget() {
  const listeners = new Map()
  return {
    addEventListener: (type, fn) => listeners.set(type, fn),
    removeEventListener: (type) => listeners.delete(type),
    fire: (type) => listeners.get(type)?.(),
  }
}

function snapshotEnvelope(seq, payload = { phase: 'lobby', players: [], hostId: 'host-1' }) {
  return { type: T.StateSnapshot, seq, payload }
}

let statuses, dispatched, identities, storage, onlineTarget

beforeEach(() => {
  vi.useFakeTimers()
  statuses = []
  dispatched = []
  identities = []
  storage = memoryStorage()
  onlineTarget = fakeEventTarget()
  FakeWebSocket.instances = []
})
afterEach(() => {
  vi.useRealTimers()
})

function setUp(join = { name: 'Alice', emoji: '🦊' }) {
  const conn = createRoomConnection('ROOM', join, {
    onStatus: (s) => statuses.push(s),
    onDispatch: (a) => dispatched.push(a),
    onIdentity: (i) => identities.push(i),
  }, {
    WebSocketImpl: FakeWebSocket,
    storage,
    urlFor: (code) => `ws://test/${code}`,
    onlineEvents: onlineTarget,
  })
  return conn
}

describe('createRoomConnection', () => {
  it('joins with the given name/emoji on first connect', () => {
    setUp({ name: 'Alice', emoji: '🦊' })
    const ws = FakeWebSocket.instances[0]
    ws.open()
    const sent = JSON.parse(ws.sent[0])
    expect(sent.type).toBe(T.Join)
    expect(sent.version).toBe(PROTOCOL_VERSION)
    expect(sent.payload).toEqual({ name: 'Alice', emoji: '🦊' })
  })

  it('reaches connected and applies the snapshot atomically on the first attach', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    expect(statuses.at(-1)).toBe('connected')
    expect(dispatched.at(-1)).toEqual({ type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'lobby', players: [], hostId: 'host-1' } })
    conn.stop()
  })

  // --- Requirement #18: reconnect reuses credentials, never a new player ---

  it('reuses seat credentials from join_accepted on every subsequent reconnect, never the original name/emoji', () => {
    const conn = setUp({ name: 'Alice', emoji: '🦊' })
    let ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.JoinAccepted, payload: { playerId: 'p1', reconnectToken: 'tok1' } })
    ws.message(snapshotEnvelope(1))
    expect(identities.at(-1)).toEqual({ playerId: 'p1', reconnectToken: 'tok1' })

    // Unexpected drop — the automatic reconnect must send the seat's
    // credentials, not another fresh join with the original name.
    ws.serverClose()
    vi.runOnlyPendingTimers() // immediate first retry
    ws = FakeWebSocket.instances[1]
    ws.open()
    const sent = JSON.parse(ws.sent[0])
    expect(sent.payload).toEqual({ playerId: 'p1', reconnectToken: 'tok1' })
    conn.stop()
  })

  // --- Requirement #15/#16: unexpected close reconnects, intentional close does not ---

  it('starts the reconnect policy after an unexpected close', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    ws.serverClose()
    expect(statuses.at(-1)).toBe('reconnecting')
    expect(FakeWebSocket.instances.length).toBe(1) // retry is scheduled, not yet fired
    conn.stop()
  })

  it('does not reconnect after an intentional stop()', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    conn.stop()
    expect(FakeWebSocket.instances[0].readyState).toBe(FakeWebSocket.CLOSED)
    vi.runAllTimers()
    expect(FakeWebSocket.instances.length).toBe(1) // no retry attempt was ever scheduled
  })

  // --- Requirement #17: fatal errors stop retrying ---

  it('does not retry after a fatal invalid_reconnect rejection, and clears stored credentials', () => {
    storage.setItem('fauxtist:credentials:ROOM', JSON.stringify({ playerId: 'p1', reconnectToken: 'stale' }))
    const conn = setUp({ playerId: 'p1', reconnectToken: 'stale' })

    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.Error, payload: { message: 'invalid or expired reconnect token', code: 'invalid_reconnect' } })
    ws.serverClose()

    expect(statuses.at(-1)).toBe('failed')
    expect(dispatched.some((a) => a.type === LOCAL_JOIN_FAILED)).toBe(true)
    expect(storage.getItem('fauxtist:credentials:ROOM')).toBeNull()
    vi.runAllTimers()
    expect(FakeWebSocket.instances.length).toBe(1) // never retried
    conn.stop()
  })

  it('does not retry after a raw close carrying a fatal protocol close code', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    ws.serverClose(4003) // room expired/shut down — no Error envelope precedes it

    expect(statuses.at(-1)).toBe('failed')
    vi.runAllTimers()
    expect(FakeWebSocket.instances.length).toBe(1) // never retried
    conn.stop()
  })

  // --- Requirement #19: fake-timer backoff, no real waiting ---

  it('backs off with the documented schedule and resets the attempt counter after reconnecting', () => {
    expect(backoffDelay(0)).toBe(0)
    for (let i = 1; i < BACKOFF_DELAYS_MS.length; i++) {
      const base = BACKOFF_DELAYS_MS[i]
      const d = backoffDelay(i, () => 0.5) // midpoint of the 0.5x-1.5x jitter band => exactly base
      expect(d).toBe(base)
    }
    expect(backoffDelay(99, () => 1)).toBeLessThanOrEqual(10000)

    const conn = setUp()
    let ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))

    // Two consecutive drops before ever reconnecting: the second retry's
    // delay must reflect attempt 1, not reset back to attempt 0.
    ws.serverClose()
    vi.advanceTimersByTime(BACKOFF_DELAYS_MS[1] * 1.5 + 10)
    ws = FakeWebSocket.instances[1]
    ws.serverClose() // fails before ever reconnecting again
    expect(FakeWebSocket.instances.length).toBe(2)
    vi.advanceTimersByTime(1) // the immediate-retry slot from attempt 0 has already been used
    expect(FakeWebSocket.instances.length).toBe(2)
    vi.advanceTimersByTime(BACKOFF_DELAYS_MS[1] * 1.5 + 10)
    expect(FakeWebSocket.instances.length).toBe(3)
    conn.stop()
  })

  it('gives up and reports failed once retries have run past the reconnect grace window', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    ws.serverClose()

    let iterations = 0
    while (statuses.at(-1) !== 'failed' && iterations < 50) {
      vi.runOnlyPendingTimers()
      FakeWebSocket.instances.at(-1).serverClose()
      iterations++
    }
    expect(statuses.at(-1)).toBe('failed')
    conn.stop()
  })

  // --- online event: immediate retry ---

  it('retries immediately on an online event instead of waiting out the backoff delay', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    ws.serverClose()
    ws.serverClose() // no-op: already closed, just guards against double handling
    expect(FakeWebSocket.instances.length).toBe(1)
    onlineTarget.fire('online')
    expect(FakeWebSocket.instances.length).toBe(2) // reconnected without advancing any timer
    conn.stop()
  })

  // --- Requirement #21: commands are disabled while disconnected/resyncing ---

  it('drops send() while not connected, without queueing it for later', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    conn.send('chat_message', { text: 'too early' })
    expect(ws.sent).toHaveLength(1) // only the join frame — the command never queued or sent

    ws.message(snapshotEnvelope(1))
    conn.send('chat_message', { text: 'now' })
    expect(ws.sent).toHaveLength(2)

    // A sequence gap flips us to resyncing; sends must be dropped again
    // until the fresh snapshot is applied.
    ws.message({ type: T.TurnChanged, seq: 99, payload: { currentPlayer: 'x' } })
    conn.send('chat_message', { text: 'mid-resync' })
    expect(ws.sent).toHaveLength(3) // the resync request itself, no new chat frame
    conn.stop()
  })

  it('marks hasVoted locally the moment a vote is sent', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    conn.send('cast_vote', { target: 'p2' })
    expect(dispatched.some((a) => a.type === LOCAL_VOTE_CAST)).toBe(true)
    conn.stop()
  })

  // --- sequencing integration: gap requests a resync, duplicates are ignored ---

  it('requests a resync on a sequence gap and applies the fresh snapshot when it arrives', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    ws.message({ type: T.TurnChanged, seq: 5, payload: { currentPlayer: 'p2' } }) // gap: expected 2
    expect(statuses.at(-1)).toBe('resyncing')
    const resyncReq = JSON.parse(ws.sent.at(-1))
    expect(resyncReq.type).toBe(T.Resync)

    ws.message(snapshotEnvelope(5, { phase: 'drawing', players: [], hostId: 'host-1', currentPlayer: 'p2' }))
    expect(statuses.at(-1)).toBe('connected')
    expect(dispatched.at(-1)).toEqual({ type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'drawing', players: [], hostId: 'host-1', currentPlayer: 'p2' } })
    conn.stop()
  })

  it('ignores a duplicate incremental event', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    const before = dispatched.length
    ws.message({ type: T.TurnChanged, seq: 1, payload: { currentPlayer: 'dup' } }) // == lastApplied
    expect(dispatched.length).toBe(before) // never reached the reducer
    conn.stop()
  })

  // --- StrictMode-style double start/stop safety ---
  // Simulates React StrictMode's deliberate mount -> cleanup -> mount:
  // the first instance must be fully inert after stop(), so nothing it
  // still has in flight can affect the second, independent instance.

  it('leaves no live socket or effect from a stopped instance after a StrictMode-style double mount', () => {
    const first = setUp()
    const firstWs = FakeWebSocket.instances[0]
    first.stop() // cleanup fires before the effect even got to open()

    const second = setUp()
    const secondWs = FakeWebSocket.instances[1]
    secondWs.open()
    secondWs.message(snapshotEnvelope(1))
    expect(statuses.at(-1)).toBe('connected')

    // The first instance's now-late events must be no-ops.
    firstWs.open()
    firstWs.message(snapshotEnvelope(7))
    expect(statuses.at(-1)).toBe('connected') // unaffected by the stale instance
    expect(dispatched.filter((a) => a.type === STATE_SNAPSHOT_RECEIVED)).toHaveLength(1)
    second.stop()
  })
})
