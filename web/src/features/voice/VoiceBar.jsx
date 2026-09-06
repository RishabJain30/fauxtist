import { useEffect } from 'react'
import { Mic, MicOff, PhoneCall } from 'lucide-react'

// VoiceBar exposes the microphone controls: enable (explicit gesture),
// mute/unmute, and optional push-to-talk (hold Space). It always degrades to
// text chat — a denied mic never blocks play, and shows a clear message.
export function VoiceBar({ voice, pushToTalk }) {
  const { active, muted, speaking, error, enable, toggleMute, setMicMuted } = voice

  // Push-to-talk: hold Space to unmute, release to mute. Ignored while typing
  // in an input.
  useEffect(() => {
    if (!pushToTalk || !active) return
    const isTyping = (t) => t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)
    const down = (e) => {
      if (e.code === 'Space' && !e.repeat && !isTyping(e.target)) {
        e.preventDefault()
        setMicMuted(false)
      }
    }
    const up = (e) => {
      if (e.code === 'Space' && !isTyping(e.target)) {
        e.preventDefault()
        setMicMuted(true)
      }
    }
    window.addEventListener('keydown', down)
    window.addEventListener('keyup', up)
    return () => {
      window.removeEventListener('keydown', down)
      window.removeEventListener('keyup', up)
    }
  }, [pushToTalk, active, setMicMuted])

  if (!active) {
    return (
      <div className="voice-bar">
        <button className="btn-secondary" onClick={enable}>
          <PhoneCall size={16} aria-hidden="true" /> Enable mic
        </button>
        {error && <span className="voice-error" role="status">{error}</span>}
        {!error && <span className="muted small">Voice is optional — chat always works.</span>}
      </div>
    )
  }

  return (
    <div className="voice-bar">
      <button className={`btn-secondary ${speaking ? 'is-speaking' : ''}`} onClick={toggleMute} aria-pressed={!muted}>
        {muted ? <MicOff size={16} aria-hidden="true" /> : <Mic size={16} aria-hidden="true" />}
        {muted ? ' Muted' : speaking ? ' Speaking' : ' Live'}
      </button>
      {pushToTalk && <span className="muted small">Hold Space to talk</span>}
    </div>
  )
}
