// An original, self-contained Web Audio sound layer. No external audio assets
// are bundled (their licensing could not be verified in this environment), so
// every cue is synthesized here — small, dependency-free, and unambiguously
// original. Sound is off until a user gesture initializes the AudioContext,
// never autoplays, and every cue has a visible equivalent elsewhere in the UI.

let ctx = null
let enabled = true
let volume = 0.6

// The cue vocabulary. Each entry is a tiny tone recipe (frequency sweep +
// short envelope), tuned to be distinct and unobtrusive.
const CUES = {
  order_placed: { freq: 520, to: 640, dur: 0.08, type: 'sine' },
  order_removed: { freq: 440, to: 300, dur: 0.08, type: 'sine' },
  declaration_revealed: { freq: 400, to: 700, dur: 0.18, type: 'triangle' },
  orders_locked: { freq: 300, to: 300, dur: 0.12, type: 'square', gain: 0.5 },
  faux_revealed: { freq: 700, to: 180, dur: 0.35, type: 'sawtooth' },
  army_move: { freq: 360, to: 420, dur: 0.06, type: 'sine', gain: 0.4 },
  battle: { freq: 160, to: 90, dur: 0.22, type: 'square', gain: 0.5 },
  captured: { freq: 520, to: 780, dur: 0.16, type: 'triangle' },
  mine_done: { freq: 660, to: 990, dur: 0.16, type: 'sine' },
  relic: { freq: 880, to: 1320, dur: 0.22, type: 'triangle' },
  timer_warning: { freq: 880, to: 880, dur: 0.1, type: 'sine' },
  victory: { freq: 520, to: 1040, dur: 0.5, type: 'triangle' },
  error: { freq: 200, to: 140, dur: 0.16, type: 'sawtooth' },
}

// initAudio must be called from a user gesture (a click/tap) before any sound
// can play, per browser autoplay policy.
export function initAudio() {
  if (ctx) return
  try {
    const AC = globalThis.AudioContext || globalThis.webkitAudioContext
    if (AC) ctx = new AC()
  } catch {
    ctx = null
  }
}

export function setSoundEnabled(on) {
  enabled = !!on
}

export function setSfxVolume(v) {
  volume = Math.max(0, Math.min(1, v))
}

// play triggers one named cue. A no-op if sound is disabled or audio has not
// been initialized by a gesture yet.
export function play(name) {
  if (!enabled || !ctx) return
  const cue = CUES[name]
  if (!cue) return
  try {
    if (ctx.state === 'suspended') ctx.resume()
    const now = ctx.currentTime
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()
    osc.type = cue.type || 'sine'
    osc.frequency.setValueAtTime(cue.freq, now)
    osc.frequency.linearRampToValueAtTime(cue.to ?? cue.freq, now + cue.dur)
    const peak = (cue.gain ?? 0.7) * volume
    gain.gain.setValueAtTime(0.0001, now)
    gain.gain.exponentialRampToValueAtTime(Math.max(0.0002, peak), now + 0.01)
    gain.gain.exponentialRampToValueAtTime(0.0001, now + cue.dur)
    osc.connect(gain).connect(ctx.destination)
    osc.start(now)
    osc.stop(now + cue.dur + 0.02)
  } catch {
    // Audio failure is never fatal to gameplay.
  }
}

export const CUE_NAMES = Object.keys(CUES)
