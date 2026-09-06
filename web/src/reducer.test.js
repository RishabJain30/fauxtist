import { describe, it, expect } from 'vitest'
import { reduce, initialState, LOCAL_JOIN_FAILED, STATE_SNAPSHOT_RECEIVED } from './reducer.js'
import { T } from './protocol.js'

describe('reduce', () => {
  it('initializes core fields from a state_snapshot', () => {
    const s = reduce(initialState(), {
      type: STATE_SNAPSHOT_RECEIVED,
      payload: {
        phase: 'lobby',
        round: 0,
        totalRounds: 6,
        hostId: 'a',
        players: [{ id: 'a', name: 'A' }],
        board: [{ id: 't1' }],
      },
    })
    expect(s.phase).toBe('lobby')
    expect(s.players).toHaveLength(1)
    expect(s.hostId).toBe('a')
    expect(s.totalRounds).toBe(6)
    expect(s.board).toEqual([{ id: 't1' }])
  })

  it('fully replaces state from a snapshot, clearing obsolete phase-specific fields', () => {
    let s = reduce(initialState(), {
      type: STATE_SNAPSHOT_RECEIVED,
      payload: {
        phase: 'planning',
        players: [{ id: 'a' }],
        hostId: 'a',
        resolution: { board: [], summary: { winner: 'a' } },
        myOrders: { faux: false, commands: [], locked: true },
      },
    })
    expect(s.phase).toBe('planning')
    expect(s.resolution).toEqual({ board: [], summary: { winner: 'a' } })
    expect(s.myOrders).toEqual({ faux: false, commands: [], locked: true })

    // A fresh snapshot without those fields must not leave them lingering —
    // a snapshot is a replace, not a merge.
    s = reduce(s, {
      type: STATE_SNAPSHOT_RECEIVED,
      payload: { phase: 'lobby', players: [{ id: 'a' }], hostId: 'a' },
    })
    expect(s.phase).toBe('lobby')
    expect(s.resolution).toBeNull()
    expect(s.myOrders).toBeNull()
  })

  it('preserves voicePeers and voiceStates across a snapshot, since neither is part of it', () => {
    let s = initialState()
    s = reduce(s, { type: T.VoicePeers, payload: { ids: ['b'] } })
    s = reduce(s, { type: T.VoiceState, payload: { id: 'b', muted: true, speaking: false } })
    s = reduce(s, { type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'lobby', players: [], hostId: 'a' } })
    expect(s.voicePeers).toEqual(['b'])
    expect(s.voiceStates.b).toEqual({ muted: true, speaking: false })
  })

  it('clears a prior error when a snapshot is applied', () => {
    let s = reduce(initialState(), { type: T.Error, payload: { message: 'oops', code: 'bad' } })
    expect(s.error).toBe('oops')
    s = reduce(s, { type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'lobby', players: [], hostId: 'a' } })
    expect(s.error).toBeNull()
    expect(s.errorCode).toBeNull()
  })

  it('updates phase, round, deadline and paused on phase_changed, clearing earlyDeadline', () => {
    // Seed an early deadline via declaration_status so we can prove it clears.
    let s = reduce(initialState(), { type: T.DeclarationStatus, payload: { submitted: 1, required: 3, earlyDeadlineMs: 5555 } })
    expect(s.earlyDeadlineMs).toBe(5555)

    s = reduce(s, {
      type: T.PhaseChanged,
      payload: { phase: 'planning', round: 2, totalRounds: 6, paused: true, phaseDeadlineMs: 9999 },
    })
    expect(s.phase).toBe('planning')
    expect(s.round).toBe(2)
    expect(s.totalRounds).toBe(6)
    expect(s.paused).toBe(true)
    expect(s.phaseDeadlineMs).toBe(9999)
    expect(s.earlyDeadlineMs).toBeNull()
  })

  it('updates aggregate declaration counts on declaration_status', () => {
    const s = reduce(initialState(), { type: T.DeclarationStatus, payload: { submitted: 2, required: 4, earlyDeadlineMs: 1234 } })
    expect(s.declarationsIn).toBe(2)
    expect(s.requiredCount).toBe(4)
    expect(s.earlyDeadlineMs).toBe(1234)
  })

  it('updates aggregate planning counts on planning_status', () => {
    const s = reduce(initialState(), { type: T.PlanningStatus, payload: { submitted: 3, locked: 1, required: 4 } })
    expect(s.ordersSubmitted).toBe(3)
    expect(s.ordersLocked).toBe(1)
    expect(s.requiredCount).toBe(4)
  })

  it('sets myOrders on orders_saved', () => {
    const s = reduce(initialState(), {
      type: T.OrdersSaved,
      payload: { faux: true, commands: [{ type: 'march', from: 't1', to: 't2' }], locked: false },
    })
    expect(s.myOrders).toEqual({ faux: true, commands: [{ type: 'march', from: 't1', to: 't2' }], locked: false })
  })

  it('sets resolution and applies its board on round_resolved', () => {
    const board = [{ id: 't1', owner: 'a', armies: 3 }]
    const s = reduce(initialState(), {
      type: T.RoundResolved,
      payload: { resolution: { board, summary: { winner: 'a' } } },
    })
    expect(s.resolution).toEqual({ board, summary: { winner: 'a' } })
    expect(s.board).toEqual(board)
  })

  it('updates connected status on player_presence_changed for the right player', () => {
    let s = reduce(initialState(), {
      type: T.LobbyUpdate,
      payload: { players: [{ id: 'a', connected: true }, { id: 'b', connected: true }], hostId: 'a' },
    })
    s = reduce(s, { type: T.PlayerPresenceChanged, payload: { id: 'b', connected: false } })
    expect(s.players.find((p) => p.id === 'b').connected).toBe(false)
    expect(s.players.find((p) => p.id === 'a').connected).toBe(true)
  })

  it('updates afk status on player_afk_changed for the right player', () => {
    let s = reduce(initialState(), {
      type: T.LobbyUpdate,
      payload: { players: [{ id: 'a', afk: false }, { id: 'b', afk: false }], hostId: 'a' },
    })
    s = reduce(s, { type: T.PlayerAFKChanged, payload: { id: 'a', afk: true } })
    expect(s.players.find((p) => p.id === 'a').afk).toBe(true)
    expect(s.players.find((p) => p.id === 'b').afk).toBe(false)
  })

  it('removes a player on player_exited', () => {
    let s = reduce(initialState(), {
      type: T.LobbyUpdate,
      payload: { players: [{ id: 'a' }, { id: 'b' }], hostId: 'a' },
    })
    s = reduce(s, { type: T.PlayerExited, payload: { id: 'b' } })
    expect(s.players).toHaveLength(1)
    expect(s.players[0].id).toBe('a')
  })

  it('updates hostId on host_changed', () => {
    let s = reduce(initialState(), { type: T.LobbyUpdate, payload: { players: [{ id: 'a' }, { id: 'b' }], hostId: 'a' } })
    s = reduce(s, { type: T.HostChanged, payload: { hostId: 'b' } })
    expect(s.hostId).toBe('b')
  })

  it('sets phase and result on game_over', () => {
    const s = reduce(initialState(), { type: T.GameOver, payload: { result: { winner: 'a', standings: [{ id: 'a' }] } } })
    expect(s.phase).toBe('game_over')
    expect(s.result).toEqual({ winner: 'a', standings: [{ id: 'a' }] })
  })

  it('appends chat on chat_broadcast', () => {
    let s = reduce(initialState(), { type: T.ChatBroadcast, payload: { from: 'a', text: 'hi' } })
    expect(s.chat).toHaveLength(1)
    expect(s.chat[0].text).toBe('hi')
    s = reduce(s, { type: T.ChatBroadcast, payload: { from: 'b', text: 'yo' } })
    expect(s.chat).toHaveLength(2)
  })

  it('records the error message and code from an error frame', () => {
    const s = reduce(initialState(), {
      type: T.Error,
      payload: { message: 'that name is already taken in this room', code: 'name_taken' },
    })
    expect(s.error).toBe('that name is already taken in this room')
    expect(s.errorCode).toBe('name_taken')
  })

  it('moves to join_failed on the local join-failure signal, keeping the prior error', () => {
    let s = reduce(initialState(), { type: T.Error, payload: { message: 'invalid or expired reconnect token', code: 'invalid_reconnect' } })
    s = reduce(s, { type: LOCAL_JOIN_FAILED })
    expect(s.phase).toBe('join_failed')
    expect(s.error).toBe('invalid or expired reconnect token')
  })

  it('returns the same state reference for an unknown message type', () => {
    const s = initialState()
    expect(reduce(s, { type: 'totally_unknown', payload: {} })).toBe(s)
  })
})
