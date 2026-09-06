// createRoom creates a room with the host's chosen name and avatar. The
// emoji is sent so the host's seat carries it (an earlier server version
// dropped it).
export async function createRoom(name, emoji) {
  const res = await fetch('/api/rooms', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ name, emoji }),
  })
  if (!res.ok) {
    let code = 'error'
    try {
      code = (await res.json()).code || code
    } catch {
      // non-JSON error body; keep the default
    }
    const err = new Error('could not create room')
    err.code = code
    throw err
  }
  return res.json() // { code, playerId, reconnectToken }
}
