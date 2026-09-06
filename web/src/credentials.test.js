import { describe, it, expect } from 'vitest'
import {
  loadCredentials,
  saveCredentials,
  clearCredentials,
  listResumableRooms,
  TTL_MS,
} from './credentials.js'

const PREFIX = 'fauxlands:credentials:'

// A Map-backed fake Storage implementing the full Web Storage surface the
// module uses (getItem/setItem/removeItem plus key/length for
// listResumableRooms). Tests inject this so the real localStorage is never
// touched.
function fakeStorage() {
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

describe('credentials', () => {
  it('round-trips saved credentials', () => {
    const store = fakeStorage()
    saveCredentials('AB3D', 'player-1', 'token-1', store, () => 1000)
    expect(loadCredentials('AB3D', store, () => 1000)).toEqual({
      playerId: 'player-1',
      reconnectToken: 'token-1',
    })
  })

  it('namespaces credentials by room code under the fauxlands prefix', () => {
    const store = fakeStorage()
    saveCredentials('AAAA', 'p-a', 't-a', store, () => 0)
    saveCredentials('BBBB', 'p-b', 't-b', store, () => 0)
    expect(store.getItem(PREFIX + 'AAAA')).toBeTruthy()
    expect(loadCredentials('AAAA', store, () => 0)).toEqual({ playerId: 'p-a', reconnectToken: 't-a' })
    expect(loadCredentials('BBBB', store, () => 0)).toEqual({ playerId: 'p-b', reconnectToken: 't-b' })
  })

  it('returns null when nothing is stored', () => {
    expect(loadCredentials('AB3D', fakeStorage(), () => 0)).toBeNull()
  })

  it('clears one room without touching others', () => {
    const store = fakeStorage()
    saveCredentials('AAAA', 'p-a', 't-a', store, () => 0)
    saveCredentials('BBBB', 'p-b', 't-b', store, () => 0)
    clearCredentials('AAAA', store)
    expect(loadCredentials('AAAA', store, () => 0)).toBeNull()
    expect(loadCredentials('BBBB', store, () => 0)).toEqual({ playerId: 'p-b', reconnectToken: 't-b' })
  })

  it('returns null for a malformed (non-JSON) entry', () => {
    const store = fakeStorage()
    store.setItem(PREFIX + 'AB3D', 'not-json')
    expect(loadCredentials('AB3D', store, () => 0)).toBeNull()
  })

  it('returns null for an entry with the wrong schemaVersion, clearing it', () => {
    const store = fakeStorage()
    store.setItem(
      PREFIX + 'AB3D',
      JSON.stringify({ schemaVersion: 99, playerId: 'p', reconnectToken: 't', savedAt: 0 }),
    )
    expect(loadCredentials('AB3D', store, () => 0)).toBeNull()
    expect(store.getItem(PREFIX + 'AB3D')).toBeNull()
  })

  it('returns null for an incomplete entry, clearing it', () => {
    const store = fakeStorage()
    store.setItem(PREFIX + 'AB3D', JSON.stringify({ schemaVersion: 1, playerId: 'only-this' }))
    expect(loadCredentials('AB3D', store, () => 0)).toBeNull()
    expect(store.getItem(PREFIX + 'AB3D')).toBeNull()
  })

  it('honours the TTL boundary exactly', () => {
    const store = fakeStorage()
    saveCredentials('AB3D', 'p', 't', store, () => 1000)
    // now - savedAt === TTL_MS is still within the window (> TTL_MS expires).
    expect(loadCredentials('AB3D', store, () => 1000 + TTL_MS)).toEqual({ playerId: 'p', reconnectToken: 't' })
    // One millisecond past the TTL is expired.
    expect(loadCredentials('AB3D', store, () => 1000 + TTL_MS + 1)).toBeNull()
  })

  it('clears an expired entry on load', () => {
    const store = fakeStorage()
    saveCredentials('AB3D', 'p', 't', store, () => 0)
    expect(loadCredentials('AB3D', store, () => TTL_MS + 1)).toBeNull()
    expect(store.getItem(PREFIX + 'AB3D')).toBeNull()
  })

  it('lists only unexpired resumable rooms, newest first, clearing stale ones', () => {
    const store = fakeStorage()
    const NOW = 10_000_000
    saveCredentials('AAAA', 'p-a', 't-a', store, () => NOW - 1000) // older
    saveCredentials('BBBB', 'p-b', 't-b', store, () => NOW - 500) // newer
    saveCredentials('CCCC', 'p-c', 't-c', store, () => NOW - TTL_MS - 1) // expired

    expect(listResumableRooms(store, () => NOW)).toEqual(['BBBB', 'AAAA'])
    // The expired one is cleared as a side effect.
    expect(store.getItem(PREFIX + 'CCCC')).toBeNull()
  })

  it('is a no-op without a store and returns null without a room code', () => {
    expect(() => saveCredentials('AB3D', 'p', 't', null)).not.toThrow()
    expect(loadCredentials('', fakeStorage(), () => 0)).toBeNull()
    expect(listResumableRooms(null, () => 0)).toEqual([])
  })
})
