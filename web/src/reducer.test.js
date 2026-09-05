import { describe, it, expect } from 'vitest'
import { reduce, initialState, LOCAL_JOIN_FAILED } from './reducer.js'
import { T } from './protocol.js'

describe('reduce', () => {
  it('initializes from room_state', () => {
    const s = reduce(initialState(), {
      type: T.RoomState,
      payload: { phase: 'lobby', players: [{ id: 'a', name: 'A', score: 0 }], hostId: 'a' },
    })
    expect(s.phase).toBe('lobby')
    expect(s.players).toHaveLength(1)
    expect(s.hostId).toBe('a')
  })

  it('replaces players on lobby_update', () => {
    let s = reduce(initialState(), { type: T.LobbyUpdate, payload: { players: [{ id: 'a' }, { id: 'b' }], hostId: 'a' } })
    expect(s.players).toHaveLength(2)
  })

  it('appends strokes on stroke_broadcast', () => {
    let s = initialState()
    s = reduce(s, { type: T.StrokeBroadcast, payload: { by: 'a', points: [{ x: 0.1, y: 0.1 }] } })
    expect(s.strokes).toHaveLength(1)
  })

  it('sets phase and clears strokes on round_started', () => {
    let s = initialState()
    s = reduce(s, { type: T.StrokeBroadcast, payload: { by: 'a', points: [] } })
    s = reduce(s, { type: T.RoundStarted, payload: { round: 1, category: 'Animal', word: 'Giraffe', youAreImpostor: false } })
    expect(s.phase).toBe('drawing')
    expect(s.strokes).toHaveLength(0)
    expect(s.word).toBe('Giraffe')
    expect(s.round).toBe(1)
  })

  it('tracks current drawer and phase changes', () => {
    let s = reduce(initialState(), { type: T.TurnChanged, payload: { currentPlayer: 'b', lap: 0, totalLaps: 2 } })
    expect(s.currentPlayer).toBe('b')
    s = reduce(s, { type: T.PhaseChanged, payload: { phase: 'voting' } })
    expect(s.phase).toBe('voting')
  })

  it('records round result and game over', () => {
    let s = reduce(initialState(), { type: T.RoundResult, payload: { impostorId: 'a', word: 'Giraffe', caught: true } })
    expect(s.lastResult.caught).toBe(true)
    s = reduce(s, { type: T.GameOver, payload: { finalScores: [{ id: 'a', score: 2 }] } })
    expect(s.phase).toBe('game_over')
    expect(s.finalScores).toHaveLength(1)
  })

  it('records the error message and code from an error frame', () => {
    const s = reduce(initialState(), { type: T.Error, payload: { message: 'that name is already taken in this room', code: 'name_taken' } })
    expect(s.error).toBe('that name is already taken in this room')
    expect(s.errorCode).toBe('name_taken')
  })

  it('moves to join_failed on the local join-failure signal, without ever having reached room_state', () => {
    let s = reduce(initialState(), { type: T.Error, payload: { message: 'invalid or expired reconnect token', code: 'invalid_reconnect' } })
    s = reduce(s, { type: LOCAL_JOIN_FAILED })
    expect(s.phase).toBe('join_failed')
    expect(s.error).toBe('invalid or expired reconnect token')
  })

  it('accumulates chat', () => {
    let s = reduce(initialState(), { type: T.ChatBroadcast, payload: { from: 'a', text: 'hi' } })
    expect(s.chat).toHaveLength(1)
    expect(s.chat[0].text).toBe('hi')
  })

  it('tracks voice peers and state', () => {
    let s = reduce(initialState(), { type: T.VoicePeers, payload: { ids: ['a', 'b'] } })
    expect(s.voicePeers).toEqual(['a', 'b'])
    s = reduce(s, { type: T.VoicePeerJoined, payload: { id: 'c' } })
    expect(s.voicePeers).toContain('c')
    s = reduce(s, { type: T.VoiceState, payload: { id: 'c', muted: false, speaking: true } })
    expect(s.voiceStates.c).toEqual({ muted: false, speaking: true })
    s = reduce(s, { type: T.VoicePeerLeft, payload: { id: 'c' } })
    expect(s.voicePeers).not.toContain('c')
    expect(s.voiceStates.c).toBeUndefined()
  })
})
