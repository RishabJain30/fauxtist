import { describe, it, expect } from 'vitest'
import { loadCredentials, saveCredentials, clearCredentials } from './credentials.js'

function fakeStorage() {
  const m = new Map()
  return {
    getItem: (k) => (m.has(k) ? m.get(k) : null),
    setItem: (k, v) => m.set(k, v),
    removeItem: (k) => m.delete(k),
  }
}

describe('credentials', () => {
  it('returns null when nothing is stored', () => {
    expect(loadCredentials('AB3D', fakeStorage())).toBeNull()
  })

  it('round-trips saved credentials', () => {
    const store = fakeStorage()
    saveCredentials('AB3D', 'player-1', 'token-1', store)
    expect(loadCredentials('AB3D', store)).toEqual({ playerId: 'player-1', reconnectToken: 'token-1' })
  })

  it('namespaces credentials by room code', () => {
    const store = fakeStorage()
    saveCredentials('AAAA', 'p-a', 't-a', store)
    saveCredentials('BBBB', 'p-b', 't-b', store)
    expect(loadCredentials('AAAA', store)).toEqual({ playerId: 'p-a', reconnectToken: 't-a' })
    expect(loadCredentials('BBBB', store)).toEqual({ playerId: 'p-b', reconnectToken: 't-b' })
  })

  it('clears credentials for a room without touching others', () => {
    const store = fakeStorage()
    saveCredentials('AAAA', 'p-a', 't-a', store)
    saveCredentials('BBBB', 'p-b', 't-b', store)
    clearCredentials('AAAA', store)
    expect(loadCredentials('AAAA', store)).toBeNull()
    expect(loadCredentials('BBBB', store)).toEqual({ playerId: 'p-b', reconnectToken: 't-b' })
  })

  it('treats corrupt stored JSON as missing', () => {
    const store = fakeStorage()
    store.setItem('fauxtist:credentials:AB3D', 'not-json')
    expect(loadCredentials('AB3D', store)).toBeNull()
  })

  it('treats incomplete stored data as missing', () => {
    const store = fakeStorage()
    store.setItem('fauxtist:credentials:AB3D', JSON.stringify({ playerId: 'only-this' }))
    expect(loadCredentials('AB3D', store)).toBeNull()
  })

  it('is a no-op without a store and without a room code', () => {
    expect(() => saveCredentials('AB3D', 'p', 't', null)).not.toThrow()
    expect(loadCredentials('', fakeStorage())).toBeNull()
  })
})
