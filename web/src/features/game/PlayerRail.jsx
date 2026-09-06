import { Crown, Mic, MicOff, WifiOff } from 'lucide-react'
import { factionForPlayer } from './factions.js'

// PlayerRail lists every player with their faction identity and public
// standing (energy, influence, relics, Faux availability) plus connection,
// host, and mic state.
export function PlayerRail({ players, hostId, meId, relicsByOwner, voiceStates, speakingIds }) {
  return (
    <ul className="player-rail" aria-label="Players">
      {players.map((p) => {
        const fac = factionForPlayer(p)
        const isHost = p.id === hostId
        const isMe = p.id === meId
        const voice = voiceStates?.[p.id]
        const speaking = speakingIds?.has(p.id)
        return (
          <li
            key={p.id}
            className={`player-card ${isMe ? 'player-me' : ''} ${p.forfeited ? 'player-out' : ''} ${!p.connected ? 'player-disconnected' : ''}`}
          >
            <span className="player-sigil" style={{ color: fac.color }} aria-hidden="true">
              {fac.sigil}
            </span>
            <span className="player-emoji" aria-hidden="true">{p.emoji}</span>
            <div className="player-body">
              <div className="player-name-row">
                <span className="player-name">{p.name}</span>
                {isHost && <Crown size={13} className="host-badge" aria-label="Host" />}
                {!p.connected && <WifiOff size={13} aria-label="Disconnected" />}
                {p.afk && <span className="afk-badge" title="Away">AFK</span>}
                {voice && (voice.muted ? <MicOff size={13} aria-label="Muted" /> : <Mic size={13} className={speaking ? 'mic-speaking' : ''} aria-label={speaking ? 'Speaking' : 'Mic on'} />)}
              </div>
              <div className="player-stats">
                <span className="stat" title="Faction">{fac.label}</span>
                <span className="stat" title="Energy">⚡{p.energy}</span>
                <span className="stat" title="Influence">✦{p.influence}</span>
                <span className="stat" title="Relics controlled">◆{relicsByOwner?.[p.id] || 0}</span>
                {p.fauxAvailable ? <span className="stat faux-ready" title="Faux Order available">Faux</span> : <span className="stat faux-spent" title="Faux Order spent">Faux✗</span>}
              </div>
            </div>
          </li>
        )
      })}
    </ul>
  )
}
