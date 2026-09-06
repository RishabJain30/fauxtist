import { useCallback, useEffect, useState } from 'react'

const KEY = 'fauxlands:prefs:v1'

// Non-sensitive local preferences only. Never store chat, voice, tokens, or
// any secret match data here.
export const DEFAULT_PREFS = {
  sound: true,
  sfxVolume: 0.6,
  reducedMotion: false,
  highContrast: false,
  pushToTalk: false,
  tutorialDismissed: false,
}

function load() {
  try {
    const raw = globalThis.localStorage?.getItem(KEY)
    if (!raw) return { ...DEFAULT_PREFS }
    return { ...DEFAULT_PREFS, ...JSON.parse(raw) }
  } catch {
    return { ...DEFAULT_PREFS }
  }
}

// useLocalPrefs persists a small preferences object and returns [prefs, setPref].
export function useLocalPrefs() {
  const [prefs, setPrefs] = useState(load)

  useEffect(() => {
    try {
      globalThis.localStorage?.setItem(KEY, JSON.stringify(prefs))
    } catch {
      // storage unavailable — non-fatal
    }
  }, [prefs])

  const setPref = useCallback((key, value) => {
    setPrefs((p) => ({ ...p, [key]: value }))
  }, [])

  return [prefs, setPref]
}

// prefersReducedMotion combines the OS setting with the in-game override.
export function prefersReducedMotion(prefs) {
  if (prefs?.reducedMotion) return true
  try {
    return globalThis.matchMedia?.('(prefers-reduced-motion: reduce)').matches ?? false
  } catch {
    return false
  }
}
