import { describe, it, expect } from 'vitest'
import { decideSequence } from './sequencing.js'
import { T } from './protocol.js'

const snapshot = (seq) => ({ type: T.StateSnapshot, seq })
const turnChanged = (seq) => ({ type: T.TurnChanged, seq })
const chat = () => ({ type: T.ChatBroadcast })

describe('decideSequence', () => {
  it('always applies unsequenced message types regardless of seq', () => {
    expect(decideSequence(5, chat())).toBe('apply')
    expect(decideSequence(null, chat())).toBe('apply')
  })

  it('applies the very first sequenced message seen, with no baseline yet', () => {
    expect(decideSequence(null, turnChanged(7))).toBe('apply')
  })

  it('applies exactly the next expected revision', () => {
    expect(decideSequence(5, turnChanged(6))).toBe('apply')
  })

  it('ignores an exact duplicate', () => {
    expect(decideSequence(5, turnChanged(5))).toBe('duplicate-or-old')
  })

  it('ignores an event older than what is already applied', () => {
    expect(decideSequence(10, turnChanged(3))).toBe('duplicate-or-old')
  })

  it('requests a resync on a sequence gap, without applying it', () => {
    expect(decideSequence(5, turnChanged(8))).toBe('gap')
  })

  it('always applies a snapshot that is not older than the current baseline', () => {
    expect(decideSequence(5, snapshot(5))).toBe('apply-snapshot')
    expect(decideSequence(5, snapshot(9))).toBe('apply-snapshot')
    expect(decideSequence(null, snapshot(1))).toBe('apply-snapshot')
  })

  it('rejects a snapshot older than the current baseline', () => {
    expect(decideSequence(9, snapshot(5))).toBe('stale-snapshot')
  })
})
