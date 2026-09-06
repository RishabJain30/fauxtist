import { useEffect, useState } from 'react'

// useCountdown returns whole seconds remaining until an absolute epoch-ms
// deadline, ticking a few times a second. Client clocks only ever DISPLAY
// remaining time — the server owns the authoritative deadline. Returns null
// when there is no deadline (untimed phase or paused).
export function useCountdown(deadlineMs) {
  const [remaining, setRemaining] = useState(() => computeRemaining(deadlineMs))

  useEffect(() => {
    if (deadlineMs == null) {
      setRemaining(null)
      return
    }
    setRemaining(computeRemaining(deadlineMs))
    const id = setInterval(() => setRemaining(computeRemaining(deadlineMs)), 250)
    return () => clearInterval(id)
  }, [deadlineMs])

  return remaining
}

function computeRemaining(deadlineMs) {
  if (deadlineMs == null) return null
  return Math.max(0, Math.ceil((deadlineMs - Date.now()) / 1000))
}
