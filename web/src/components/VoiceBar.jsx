export default function VoiceBar({ voice, state, meId }) {
  const { active, muted, speaking, error, enable, toggleMute } = voice

  return (
    <div className="card voicebar pop-in">
      <div className="row">
        {!active
          ? <button className="btn-mic" onClick={enable}>🎤 Enable voice</button>
          : <button className="btn-mic" onClick={toggleMute}>{muted ? '🔇 Unmute' : '🎙️ Mute'}</button>}
        {error && <span className="muted">{error}</span>}
      </div>
      <div className="row" style={{ gap: 8 }}>
        {state.players.map((p) => {
          const isMe = p.id === meId
          const present = isMe ? active : state.voicePeers.includes(p.id)
          if (!present) return null
          const v = isMe ? { muted, speaking } : (state.voiceStates[p.id] || { muted: true, speaking: false })
          return (
            <span key={p.id} className={`vpill ${v.speaking ? 'is-speaking' : ''} ${v.muted ? 'is-muted' : ''}`}>
              <span className="avatar">{p.emoji || '🎭'}</span>{p.name}{v.muted ? ' 🔇' : ''}
            </span>
          )
        })}
      </div>
    </div>
  )
}
