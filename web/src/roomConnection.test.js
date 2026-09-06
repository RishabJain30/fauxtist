import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createRoomConnection, backoffDelay, BACKOFF_DELAYS_MS } from './roomConnection.js'
import { T, PROTOCOL_VERSION } from './protocol.js'
import { STATE_SNAPSHOT_RECEIVED, LOCAL_JOIN_FAILED } from './reducer.js'

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
    setItem: (k, v) => m.set(k, String(v)),
    removeItem: (k) => m.delete(k),
    key: (i) => [...m.keys()][i] ?? null,
    get length() {
      return m.size
    },
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
    expect(dispatched.at(-1)).toEqual({ type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'lobby', players: [], hostId: 'host-1' }, generation: 1 })
    conn.stop()
  })

  // --- identity from join_accepted, including spectator seats ---

  it('reports identity (playerId, reconnectToken, spectator=false) on a normal join_accepted', () => {
    const conn = setUp({ name: 'Alice', emoji: '🦊' })
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.JoinAccepted, payload: { playerId: 'p1', reconnectToken: 'tok1' } })
    expect(identities.at(-1)).toEqual({ playerId: 'p1', reconnectToken: 'tok1', spectator: false })
    conn.stop()
  })

  it('reports spectator:true on a spectator join_accepted', () => {
    const conn = setUp({ name: 'Sam', emoji: '👀', asSpectator: true })
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.JoinAccepted, payload: { playerId: 'sp1', reconnectToken: 'tok', spectator: true } })
    expect(identities.at(-1)).toEqual({ playerId: 'sp1', reconnectToken: 'tok', spectator: true })
    conn.stop()
  })

  it('persists seat credentials to storage on join_accepted', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.JoinAccepted, payload: { playerId: 'p1', reconnectToken: 'tok1' } })
    const raw = storage.getItem('fauxlands:credentials:ROOM')
    expect(raw).toBeTruthy()
    const saved = JSON.parse(raw)
    expect(saved.playerId).toBe('p1')
    expect(saved.reconnectToken).toBe('tok1')
    conn.stop()
  })

  // --- Requirement #18: reconnect reuses credentials, never a new player ---

  it('reuses seat credentials from join_accepted on every subsequent reconnect, never the original name/emoji', () => {
    const conn = setUp({ name: 'Alice', emoji: '🦊' })
    let ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.JoinAccepted, payload: { playerId: 'p1', reconnectToken: 'tok1' } })
    ws.message(snapshotEnvelope(1))
    expect(identities.at(-1)).toEqual({ playerId: 'p1', reconnectToken: 'tok1', spectator: false })

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

  // Regression coverage for useVoice.js's reconnect handling: it tells a
  // genuine new-socket reconnect apart from a same-socket resync purely by
  // comparing this generation number across snapshots, so it must actually
  // change on the former and stay put on the latter.
  it('carries a new generation on a reconnected snapshot, but not on a same-socket resync', () => {
    const conn = setUp()
    let ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    const firstGeneration = dispatched.at(-1).generation
    expect(firstGeneration).toBeTypeOf('number')

    // Same socket, just a resync — generation must not move.
    ws.message({ type: T.PhaseChanged, seq: 5, payload: { phase: 'planning' } }) // gap: forces a resync
    ws.message(snapshotEnvelope(5))
    expect(dispatched.at(-1).generation).toBe(firstGeneration)

    // A real drop and reconnect — a brand new socket — must bump it.
    ws.serverClose()
    vi.runOnlyPendingTimers()
    ws = FakeWebSocket.instances[1]
    ws.open()
    ws.message(snapshotEnvelope(6)) // the room's revision only ever goes up, reconnect or not
    expect(dispatched.at(-1).generation).toBeGreaterThan(firstGeneration)
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
    storage.setItem('fauxlands:credentials:ROOM', JSON.stringify({ playerId: 'p1', reconnectToken: 'stale' }))
    const conn = setUp({ playerId: 'p1', reconnectToken: 'stale' })

    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.Error, payload: { message: 'invalid or expired reconnect token', code: 'invalid_reconnect' } })
    ws.serverClose()

    expect(statuses.at(-1)).toBe('failed')
    expect(dispatched.some((a) => a.type === LOCAL_JOIN_FAILED)).toBe(true)
    expect(storage.getItem('fauxlands:credentials:ROOM')).toBeNull()
    vi.runAllTimers()
    expect(FakeWebSocket.instances.length).toBe(1) // never retried
    conn.stop()
  })

  it('treats a spectators_full error as fatal and never retries', () => {
    const conn = setUp({ name: 'Sam', emoji: '👀', asSpectator: true })
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message({ type: T.Error, payload: { message: 'no spectator slots left', code: 'spectators_full' } })
    ws.serverClose()

    expect(statuses.at(-1)).toBe('failed')
    expect(dispatched.some((a) => a.type === LOCAL_JOIN_FAILED)).toBe(true)
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
    ws.message({ type: T.PhaseChanged, seq: 99, payload: { phase: 'planning' } })
    conn.send('chat_message', { text: 'mid-resync' })
    expect(ws.sent).toHaveLength(3) // the resync request itself, no new chat frame
    conn.stop()
  })

  // --- sequencing integration: gap requests a resync, duplicates are ignored ---

  it('requests a resync on a sequence gap and applies the fresh snapshot when it arrives', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    ws.message({ type: T.PhaseChanged, seq: 5, payload: { phase: 'planning' } }) // gap: expected 2
    expect(statuses.at(-1)).toBe('resyncing')
    const resyncReq = JSON.parse(ws.sent.at(-1))
    expect(resyncReq.type).toBe(T.Resync)

    ws.message(snapshotEnvelope(5, { phase: 'planning', players: [], hostId: 'host-1' }))
    expect(statuses.at(-1)).toBe('connected')
    expect(dispatched.at(-1)).toEqual({ type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'planning', players: [], hostId: 'host-1' }, generation: 1 })
    conn.stop()
  })

  // Regression: the server used to bump its revision once per accepted
  // command instead of once per event, so a command that cascades into
  // several ordered events stamped every event in the cascade with the same
  // seq. This sequencer correctly treated the second event as a duplicate of
  // the first and silently dropped it. This exercises that path at the one
  // layer (roomConnection -> decideSequence -> onDispatch) a real browser
  // client runs.
  it('applies every event in a multi-event cascade, not just the first', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))

    ws.message({ type: T.PhaseChanged, seq: 2, payload: { phase: 'declaration', round: 1 } })
    ws.message({ type: T.DeclarationStatus, seq: 3, payload: { submitted: 0, required: 4 } })

    expect(dispatched.filter((a) => a.type === T.PhaseChanged)).toHaveLength(1)
    expect(dispatched.filter((a) => a.type === T.DeclarationStatus)).toHaveLength(1)
    expect(dispatched.at(-1).seq).toBe(3)
    expect(dispatched.at(-1).payload).toEqual({ submitted: 0, required: 4 })
    conn.stop()
  })

  it('ignores a duplicate incremental event', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1))
    const before = dispatched.length
    ws.message({ type: T.PhaseChanged, seq: 1, payload: { phase: 'dup' } }) // == lastApplied
    expect(dispatched.length).toBe(before) // never reached the reducer
    conn.stop()
  })

  // --- Lifecycle broadcasts sent back to back with strictly increasing seq:
  // every one must be applied, not collapsed as a duplicate. ---

  it('applies both a presence change and the following planning_status when a player disconnects', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1, {
      phase: 'planning', players: [{ id: 'p4', connected: true }], hostId: 'host-1', ordersSubmitted: 0, requiredCount: 4,
    }))

    ws.message({ type: T.PlayerPresenceChanged, seq: 2, payload: { id: 'p4', connected: false } })
    ws.message({ type: T.PlanningStatus, seq: 3, payload: { submitted: 0, locked: 0, required: 3 } })

    const presence = dispatched.find((a) => a.type === T.PlayerPresenceChanged)
    const planning = dispatched.find((a) => a.type === T.PlanningStatus)
    expect(presence).toBeTruthy()
    expect(planning).toBeTruthy() // would be silently dropped as a duplicate seq before the server fix
    expect(planning.payload).toEqual({ submitted: 0, locked: 0, required: 3 })
    conn.stop()
  })

  it('applies player_exited, host_changed, and lobby_update in order when the lobby host is replaced', () => {
    const conn = setUp()
    const ws = FakeWebSocket.instances[0]
    ws.open()
    ws.message(snapshotEnvelope(1, {
      phase: 'lobby', players: [{ id: 'host-1', connected: false }, { id: 'bob', connected: true }], hostId: 'host-1',
    }))

    ws.message({ type: T.PlayerExited, seq: 2, payload: { id: 'host-1' } })
    ws.message({ type: T.HostChanged, seq: 3, payload: { hostId: 'bob' } })
    ws.message({ type: T.LobbyUpdate, seq: 4, payload: { players: [{ id: 'bob', connected: true }], hostId: 'bob' } })

    const playerExited = dispatched.find((a) => a.type === T.PlayerExited)
    const hostChanged = dispatched.find((a) => a.type === T.HostChanged)
    const lobbyUpdate = dispatched.find((a) => a.type === T.LobbyUpdate)
    expect(playerExited).toBeTruthy()
    expect(hostChanged).toBeTruthy() // was silently dropped as a duplicate seq before the fix
    expect(lobbyUpdate).toBeTruthy() // was silently dropped as a duplicate seq before the fix
    expect(hostChanged.payload).toEqual({ hostId: 'bob' })
    expect(lobbyUpdate.payload.hostId).toBe('bob')
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
