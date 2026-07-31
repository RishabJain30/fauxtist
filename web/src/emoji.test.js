import { describe, it, expect } from 'vitest'
import { EMOJIS, defaultEmoji } from './emoji.js'

describe('emoji', () => {
  it('has 12 unique emojis', () => {
    expect(EMOJIS).toHaveLength(12)
    expect(new Set(EMOJIS).size).toBe(12)
  })
  it('default is the first', () => {
    expect(defaultEmoji()).toBe(EMOJIS[0])
  })
})
