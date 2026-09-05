const PREFIX = 'fauxtist:credentials:'

function defaultStorage() {
  return globalThis.sessionStorage
}

// loadCredentials, saveCredentials, and clearCredentials persist a seat's
// {playerId, reconnectToken} under a key namespaced by room code, so a page
// refresh on the same room can reconnect instead of joining fresh. `store`
// is injectable for testing; it defaults to the browser's sessionStorage.
export function loadCredentials(code, store = defaultStorage()) {
  if (!store || !code) return null
  try {
    const raw = store.getItem(PREFIX + code)
    if (!raw) return null
    const data = JSON.parse(raw)
    if (!data || typeof data.playerId !== 'string' || typeof data.reconnectToken !== 'string') return null
    if (!data.playerId || !data.reconnectToken) return null
    return { playerId: data.playerId, reconnectToken: data.reconnectToken }
  } catch {
    return null
  }
}

export function saveCredentials(code, playerId, reconnectToken, store = defaultStorage()) {
  if (!store || !code) return
  try {
    store.setItem(PREFIX + code, JSON.stringify({ playerId, reconnectToken }))
  } catch {
    // Storage unavailable (private mode, quota) — non-fatal, just no restore.
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
