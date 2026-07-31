export default function VoiceBar({ voice, state, meId }) {
  const { active, muted, speaking, error, enable, toggleMute } = voice

  const icon = (v) => (v.speaking ? '🔊' : v.muted ? '🔇' : '🎙️')

  return (
    <div className="card row" style={{ justifyContent: 'space-between' }}>
      <div className="row">
        {!active
          ? <button onClick={enable}>🎤 Enable voice</button>
          : <button onClick={toggleMute}>{muted ? '🔇 Unmute' : '🎙️ Mute'}</button>}
        {error && <span className="muted">{error}</span>}
      </div>
      <div className="row" style={{ flexWrap: 'wrap' }}>
        {state.players.map((p) => {
          const isMe = p.id === meId
          const present = isMe ? active : state.voicePeers.includes(p.id)
          if (!present) return null
          const v = isMe ? { muted, speaking } : (state.voiceStates[p.id] || { muted: true, speaking: false })
          return <span key={p.id} className="badge">{icon(v)} {p.name}</span>
        })}
      </div>
    </div>
  )
}
