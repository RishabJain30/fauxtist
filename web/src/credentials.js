const PREFIX = 'fauxlands:credentials:'

// SCHEMA_VERSION lets a future change invalidate stored credentials cleanly:
// an entry written by a different schema is ignored (and cleared) rather than
// misread.
const SCHEMA_VERSION = 1

// TTL_MS bounds how long a saved seat is offered for "resume". Two hours is
// comfortably longer than a match but short enough that a stale token on a
// shared device does not linger.
export const TTL_MS = 2 * 60 * 60 * 1000

function defaultStorage() {
  // localStorage survives a full browser close (unlike sessionStorage), which
  // is what makes "resume room" work after reopening the tab.
  return globalThis.localStorage
}

// loadCredentials returns a seat's {playerId, reconnectToken} for a room code
// if a valid, unexpired entry exists; otherwise null (clearing an expired or
// malformed one). `now` and `store` are injectable for testing.
export function loadCredentials(code, store = defaultStorage(), now = Date.now) {
  if (!store || !code) return null
  try {
    const raw = store.getItem(PREFIX + code)
    if (!raw) return null
    const data = JSON.parse(raw)
    if (
      !data ||
      data.schemaVersion !== SCHEMA_VERSION ||
      typeof data.playerId !== 'string' ||
      typeof data.reconnectToken !== 'string' ||
      typeof data.savedAt !== 'number' ||
      !data.playerId ||
      !data.reconnectToken
    ) {
      store.removeItem(PREFIX + code)
      return null
    }
    if (now() - data.savedAt > TTL_MS) {
      store.removeItem(PREFIX + code)
      return null
    }
    return { playerId: data.playerId, reconnectToken: data.reconnectToken }
  } catch {
    return null
  }
}

export function saveCredentials(code, playerId, reconnectToken, store = defaultStorage(), now = Date.now) {
  if (!store || !code) return
  try {
    store.setItem(
      PREFIX + code,
      JSON.stringify({ schemaVersion: SCHEMA_VERSION, playerId, reconnectToken, savedAt: now() }),
    )
  } catch {
    // Storage unavailable (private mode, quota) — non-fatal, just no resume.
  }
}

export function clearCredentials(code, store = defaultStorage()) {
  if (!store || !code) return
  try {
    store.removeItem(PREFIX + code)
  } catch {
    // ignore
  }
}

// listResumableRooms returns the room codes with a valid, unexpired saved
// seat, newest first — for the landing page's "Resume room ABCD" cards.
// Expired or malformed entries are cleared as a side effect.
export function listResumableRooms(store = defaultStorage(), now = Date.now) {
  if (!store) return []
  const out = []
  const stale = []
  try {
    for (let i = 0; i < store.length; i++) {
      const key = store.key(i)
      if (!key || !key.startsWith(PREFIX)) continue
      const code = key.slice(PREFIX.length)
      try {
        const data = JSON.parse(store.getItem(key))
        if (
          data &&
          data.schemaVersion === SCHEMA_VERSION &&
          typeof data.savedAt === 'number' &&
          now() - data.savedAt <= TTL_MS &&
          data.playerId &&
          data.reconnectToken
        ) {
          out.push({ code, savedAt: data.savedAt })
        } else {
          stale.push(key)
        }
      } catch {
        stale.push(key)
      }
    }
  } catch {
    return []
  }
  for (const key of stale) {
    try {
      store.removeItem(key)
    } catch {
      // ignore
    }
  }
  out.sort((a, b) => b.savedAt - a.savedAt)
  return out.map((e) => e.code)
}
