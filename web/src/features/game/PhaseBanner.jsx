import { useCountdown } from '../../shared/hooks/useCountdown.js'

export const PHASE_LABELS = {
  lobby: 'Lobby',
  income: 'Income',
  negotiation: 'Negotiation',
  declaration: 'Declaration',
  declaration_reveal: 'Declarations Revealed',
  secret_planning: 'Secret Planning',
  resolution: 'Resolution',
  round_summary: 'Round Summary',
  game_over: 'Game Over',
}

export const PHASE_HINTS = {
  income: 'Collecting income for the round.',
  negotiation: 'Talk it out. Propose moves — nothing here is binding.',
  declaration: 'Commit one order that everyone will see.',
  declaration_reveal: 'Every declaration is now public.',
  secret_planning: 'Submit your hidden orders. Turn your declaration into a Faux Order if you dare.',
  resolution: 'All orders resolve at once.',
  round_summary: 'How the round shook out.',
}

// PhaseBanner shows the current phase, round, and a display-only countdown to
// the server's absolute deadline. Announces phase changes politely for screen
// readers.
export function PhaseBanner({ phase, round, totalRounds, deadlineMs, earlyDeadlineMs, paused }) {
  const remaining = useCountdown(paused ? null : deadlineMs)
  const earlyRemaining = useCountdown(earlyDeadlineMs)
  const label = PHASE_LABELS[phase] || phase
  const warning = remaining != null && remaining <= 5

  return (
    <div className="phase-banner" role="status" aria-live="polite">
      <div className="phase-banner-main">
        <span className="phase-name">{label}</span>
        {round > 0 && <span className="phase-round">Round {round}/{totalRounds}</span>}
      </div>
      <div className="phase-banner-right">
        {paused && <span className="phase-paused">Paused — waiting for players</span>}
        {!paused && earlyRemaining != null && (
          <span className="phase-early">All locked · {earlyRemaining}s</span>
        )}
        {!paused && remaining != null && (
          <span className={`phase-timer ${warning ? 'phase-timer-warn' : ''}`} aria-label={`${remaining} seconds remaining`}>
            {remaining}s
          </span>
        )}
      </div>
      <div className="phase-hint">{PHASE_HINTS[phase] || ''}</div>
    </div>
  )
}
