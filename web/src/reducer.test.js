import { describe, it, expect } from 'vitest'
import { reduce, initialState, LOCAL_JOIN_FAILED, STATE_SNAPSHOT_RECEIVED, LOCAL_VOTE_CAST } from './reducer.js'
import { T } from './protocol.js'

describe('reduce', () => {
  it('initializes from a state_snapshot', () => {
    const s = reduce(initialState(), {
      type: STATE_SNAPSHOT_RECEIVED,
      payload: { phase: 'lobby', players: [{ id: 'a', name: 'A', score: 0 }], hostId: 'a' },
    })
    expect(s.phase).toBe('lobby')
    expect(s.players).toHaveLength(1)
    expect(s.hostId).toBe('a')
  })

  it('replaces state atomically from a snapshot, clearing obsolete phase-specific fields', () => {
    let s = reduce(initialState(), {
      type: STATE_SNAPSHOT_RECEIVED,
      payload: { phase: 'voting', players: [{ id: 'a' }], hostId: 'a', hasVoted: true, votesCast: 2, votesTotal: 4, voteTargets: ['b'] },
    })
    expect(s.phase).toBe('voting')
    expect(s.hasVoted).toBe(true)
    expect(s.votesCast).toBe(2)

    // Moving to reveal via a fresh snapshot must not leave any voting-era
    // field lingering — the whole point of a snapshot being a replace, not
    // a merge.
    s = reduce(s, {
      type: STATE_SNAPSHOT_RECEIVED,
      payload: { phase: 'reveal', players: [{ id: 'a' }], hostId: 'a', lastResult: { impostorId: 'a', caught: true } },
    })
    expect(s.phase).toBe('reveal')
    expect(s.hasVoted).toBe(false)
    expect(s.votesCast).toBe(0)
    expect(s.votesTotal).toBe(0)
    expect(s.voteTargets).toEqual([])
    expect(s.lastResult).toEqual({ impostorId: 'a', caught: true })
  })

  it('preserves chat and voice state across a snapshot, since neither is part of it', () => {
    let s = initialState()
    s = reduce(s, { type: T.ChatBroadcast, payload: { from: 'a', text: 'hi' } })
    s = reduce(s, { type: T.VoicePeers, payload: { ids: ['b'] } })
    s = reduce(s, { type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'lobby', players: [], hostId: 'a' } })
    expect(s.chat).toHaveLength(1)
    expect(s.voicePeers).toEqual(['b'])
  })

  it('clears a prior error when a snapshot is applied', () => {
    let s = reduce(initialState(), { type: T.Error, payload: { message: 'oops', code: 'bad' } })
    expect(s.error).toBe('oops')
    s = reduce(s, { type: STATE_SNAPSHOT_RECEIVED, payload: { phase: 'lobby', players: [], hostId: 'a' } })
    expect(s.error).toBeNull()
    expect(s.errorCode).toBeNull()
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

  it('sets phase, clears strokes, and resets voting fields on round_started', () => {
    let s = initialState()
    s = reduce(s, { type: LOCAL_VOTE_CAST })
    s = reduce(s, { type: T.StrokeBroadcast, payload: { by: 'a', points: [] } })
    s = reduce(s, { type: T.RoundStarted, payload: { round: 1, category: 'Animal', word: 'Giraffe', youAreImpostor: false } })
    expect(s.phase).toBe('drawing')
    expect(s.strokes).toHaveLength(0)
    expect(s.word).toBe('Giraffe')
    expect(s.round).toBe(1)
    expect(s.hasVoted).toBe(false)
  })

  it('tracks current drawer and phase changes', () => {
    let s = reduce(initialState(), { type: T.TurnChanged, payload: { currentPlayer: 'b', lap: 0, totalLaps: 2 } })
    expect(s.currentPlayer).toBe('b')
    s = reduce(s, { type: T.PhaseChanged, payload: { phase: 'voting' } })
    expect(s.phase).toBe('voting')
  })

  it('marks hasVoted locally the moment a vote is sent, ahead of server confirmation', () => {
    let s = reduce(initialState(), { type: LOCAL_VOTE_CAST })
    expect(s.hasVoted).toBe(true)
  })

  it('records round result, its guess deadline, and game over', () => {
    let s = reduce(initialState(), { type: T.RoundResult, payload: { impostorId: 'a', word: 'Giraffe', caught: true, guessDeadlineMs: 12345 } })
    expect(s.lastResult.caught).toBe(true)
    expect(s.guessDeadlineMs).toBe(12345)
    s = reduce(s, { type: T.GameOver, payload: { finalScores: [{ id: 'a', score: 2 }] } })
    expect(s.phase).toBe('game_over')
    expect(s.finalScores).toHaveLength(1)
  })

  it('records the error message and code from an error frame', () => {
    const s = reduce(initialState(), { type: T.Error, payload: { message: 'that name is already taken in this room', code: 'name_taken' } })
    expect(s.error).toBe('that name is already taken in this room')
    expect(s.errorCode).toBe('name_taken')
  })

  it('moves to join_failed on the local join-failure signal, without ever having reached a snapshot', () => {
    let s = reduce(initialState(), { type: T.Error, payload: { message: 'invalid or expired reconnect token', code: 'invalid_reconnect' } })
    s = reduce(s, { type: LOCAL_JOIN_FAILED })
    expect(s.phase).toBe('join_failed')
    expect(s.error).toBe('invalid or expired reconnect token')
  })

  it('removes a player from the roster on player_left', () => {
    let s = reduce(initialState(), {
      type: T.LobbyUpdate,
      payload: { players: [{ id: 'a', name: 'A' }, { id: 'b', name: 'B' }], hostId: 'a' },
    })
    s = reduce(s, { type: T.PlayerLeft, payload: { id: 'b' } })
    expect(s.players).toHaveLength(1)
    expect(s.players[0].id).toBe('a')
  })

  it('updates connected status on player_presence_changed without removing the player', () => {
    let s = reduce(initialState(), {
      type: T.LobbyUpdate,
      payload: { players: [{ id: 'a', name: 'A', connected: true }], hostId: 'a' },
    })
    s = reduce(s, { type: T.PlayerPresenceChanged, payload: { id: 'a', connected: false } })
    expect(s.players).toHaveLength(1)
    expect(s.players[0].connected).toBe(false)
    s = reduce(s, { type: T.PlayerPresenceChanged, payload: { id: 'a', connected: true } })
    expect(s.players[0].connected).toBe(true)
  })

  it('updates hostId on host_changed', () => {
    let s = reduce(initialState(), { type: T.LobbyUpdate, payload: { players: [{ id: 'a' }, { id: 'b' }], hostId: 'a' } })
    s = reduce(s, { type: T.HostChanged, payload: { hostId: 'b' } })
    expect(s.hostId).toBe('b')
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
