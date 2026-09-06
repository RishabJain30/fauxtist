import { useEffect, useRef, useState } from 'react'
import { FastForward } from 'lucide-react'

const SFX_FOR = {
  faux_revealed: 'faux_revealed',
  recruit: 'army_move',
  fortify: 'army_move',
  march: 'army_move',
  battle: 'battle',
  capture: 'captured',
  build: 'mine_done',
  build_failed: 'error',
  relic_influence: 'relic',
}

// ResolutionOverlay narrates a round's resolution timeline frame by frame,
// with sound cues and a visible caption (every audio cue has this visible
// equivalent). Fast-forward reveals the rest instantly — it affects visuals
// only, never the authoritative outcome. Reduced motion shows everything at
// once.
export function ResolutionOverlay({ resolution, nameOf, reducedMotion, sfx }) {
  const frames = resolution?.frames || []
  const [shown, setShown] = useState(reducedMotion ? frames.length : 0)
  const timer = useRef(null)

  useEffect(() => {
    if (reducedMotion) {
      setShown(frames.length)
      return
    }
    setShown(0)
    let i = 0
    timer.current = setInterval(() => {
      i += 1
      setShown(i)
      const f = frames[i - 1]
      if (f && sfx) sfx(SFX_FOR[f.kind])
      if (i >= frames.length && timer.current) {
        clearInterval(timer.current)
        timer.current = null
      }
    }, 550)
    return () => {
      if (timer.current) clearInterval(timer.current)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [resolution])

  function skip() {
    if (timer.current) clearInterval(timer.current)
    timer.current = null
    setShown(frames.length)
  }

  const visible = frames.slice(0, shown)

  return (
    <div className="resolution-overlay">
      <div className="resolution-head">
        <h3>Resolution</h3>
        {shown < frames.length && (
          <button className="btn-secondary" onClick={skip}>
            <FastForward size={14} aria-hidden="true" /> Skip
          </button>
        )}
      </div>
      <ol className="resolution-log" aria-live="polite">
        {visible.map((f, i) => (
          <li key={i} className={`res-frame res-${f.kind}`}>{caption(f, nameOf)}</li>
        ))}
      </ol>
      {frames.length === 0 && <p className="muted">Everyone held. A quiet round.</p>}
    </div>
  )
}

function caption(f, nameOf) {
  const who = f.player ? nameOf(f.player) : ''
  const to = f.to ? f.to.replace('t_', '') : ''
  switch (f.kind) {
    case 'faux_revealed':
      return `🎭 ${who} revealed a Faux Order — that was a lie!`
    case 'recruit':
      return `➕ ${who} recruited ${f.armies}⚔ at ${to}`
    case 'fortify':
      return `🛡 ${who} fortified ${to}`
    case 'march':
      return `➡ ${who} marched ${f.armies}⚔ ${f.from.replace('t_', '')} → ${to}`
    case 'battle':
      return f.winner ? `⚔ Battle at ${to} — ${nameOf(f.winner)} broke through` : `⚔ Battle at ${to} — the defender held`
    case 'capture':
      return `🚩 ${who} captured ${to} (${f.armies}⚔ remain)`
    case 'build':
      return `🏗 ${who} completed a ${f.structure} at ${to}`
    case 'build_failed':
      return `✖ ${who}'s build at ${to} failed — energy refunded`
    case 'relic_influence':
      return `✦ ${who} holds a relic (+1 influence)`
    default:
      return f.kind
  }
}
