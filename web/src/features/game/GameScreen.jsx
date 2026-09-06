import { useEffect, useMemo, useRef, useState } from 'react'
import * as AlertDialog from '@radix-ui/react-alert-dialog'
import { LogOut } from 'lucide-react'
import { layoutBoard } from './mapGeometry.js'
import { factionForPlayer } from './factions.js'
import { legalMarchTargets } from './orderDraft.js'
import { HexMap } from './HexMap.jsx'
import { BoardViewport } from './BoardViewport.jsx'
import { PhaseBanner } from './PhaseBanner.jsx'
import { PlayerRail } from './PlayerRail.jsx'
import { OrderPanel, describeCommand } from './OrderPanel.jsx'
import { NegotiationPanel } from './NegotiationPanel.jsx'
import { ResolutionOverlay } from './ResolutionOverlay.jsx'
import { RoundSummary } from './RoundSummary.jsx'
import { GameResults } from './GameResults.jsx'
import { TerritoryList } from './TerritoryList.jsx'
import { Chat } from '../chat/Chat.jsx'
import { VoiceBar } from '../voice/VoiceBar.jsx'
import { Settings } from '../settings/Settings.jsx'

// GameScreen orchestrates the board, HUD, and the phase-appropriate panel.
export function GameScreen({ state, meId, send, subscribe, voice, prefs, setPref, sfx, onLeaveForNow, onResign, disabled }) {
  const board = useMemo(() => state.board || [], [state.board])
  const layout = useMemo(() => layoutBoard(board), [board])
  const [selectedTile, setSelectedTile] = useState(null)
  const isSpectator = state.role === 'spectator'

  const nameOf = useMemo(() => {
    const m = {}
    for (const p of state.players || []) m[p.id] = p.name
    return (id) => m[id] || 'Player'
  }, [state.players])

  const factionByOwner = useMemo(() => {
    const m = {}
    for (const p of state.players || []) m[p.id] = factionForPlayer(p)
    return m
  }, [state.players])

  const relicsByOwner = useMemo(() => {
    const m = {}
    for (const t of board) if (t.type === 'relic' && t.owner) m[t.owner] = (m[t.owner] || 0) + 1
    return m
  }, [board])

  const { pings, proposals, speakingIds } = useMapEffects(subscribe)

  // Play a short cue when a new phase begins.
  const prevPhase = useRef(state.phase)
  useEffect(() => {
    if (state.phase !== prevPhase.current) {
      if (state.phase === 'declaration_reveal') sfx('declaration_revealed')
      prevPhase.current = state.phase
    }
  }, [state.phase, sfx])

  // Arrows: revealed declarations (public) + live proposals.
  const arrows = useMemo(() => {
    const out = []
    if (['declaration_reveal', 'secret_planning', 'resolution', 'round_summary'].includes(state.phase)) {
      for (const d of state.revealedDeclarations || []) {
        if (d.command?.type === 'march') {
          out.push({ from: d.command.from, to: d.command.to, kind: 'order', color: factionByOwner[d.player]?.color })
        }
      }
    }
    for (const pr of proposals) out.push({ from: pr.from, to: pr.to, kind: 'proposal' })
    return out
  }, [state.phase, state.revealedDeclarations, proposals, factionByOwner])

  const highlightedIds = useMemo(() => {
    if (!selectedTile || isSpectator) return new Set()
    if (state.phase !== 'declaration' && state.phase !== 'secret_planning') return new Set()
    const t = board.find((x) => x.id === selectedTile)
    if (!t || t.owner !== meId) return new Set()
    return new Set(legalMarchTargets(board, selectedTile, meId))
  }, [selectedTile, board, meId, state.phase, isSpectator])

  const pingSet = useMemo(() => new Set(pings.map((p) => p.tile)), [pings])

  if (state.phase === 'game_over') {
    return (
      <div className="game-over-screen">
        <GameResults
          result={state.result}
          isHost={state.hostId === meId}
          meId={meId}
          rematchReady={state.rematchReady}
          nameOf={nameOf}
          send={send}
          onLeave={onResign}
          disabled={disabled}
          reducedMotion={prefs.reducedMotion}
          sfx={sfx}
        />
      </div>
    )
  }

  return (
    <div className="game-screen">
      <header className="game-header">
        <PhaseBanner
          phase={state.phase}
          round={state.round}
          totalRounds={state.totalRounds}
          deadlineMs={state.phaseDeadlineMs}
          earlyDeadlineMs={state.earlyDeadlineMs}
          paused={state.paused}
        />
        <div className="game-header-actions">
          <Settings prefs={prefs} setPref={setPref} />
          <ExitDialog onLeaveForNow={onLeaveForNow} onResign={onResign} isSpectator={isSpectator} />
        </div>
      </header>

      <div className="game-body">
        <main className="game-board-col">
          <BoardViewport reducedMotion={prefs.reducedMotion}>
            <HexMap
              board={board}
              layout={layout}
              factionByOwner={factionByOwner}
              selectedId={selectedTile}
              highlightedIds={highlightedIds}
              dimmedIds={pingSet}
              arrows={arrows}
              onSelect={setSelectedTile}
            />
          </BoardViewport>
          <TerritoryList board={board} meId={meId} factionByOwner={factionByOwner} selectedId={selectedTile} onSelect={setSelectedTile} />
        </main>

        <aside className="game-rail">
          <PlayerRail
            players={state.players}
            hostId={state.hostId}
            meId={meId}
            relicsByOwner={relicsByOwner}
            voiceStates={state.voiceStates}
            speakingIds={speakingIds}
          />
          {!isSpectator && <VoiceBar voice={voice} pushToTalk={prefs.pushToTalk} />}

          <div className="phase-panel">
            {state.error && <div className="inline-error" role="alert">{state.error}</div>}
            {isSpectator ? (
              <SpectatorPanel state={state} />
            ) : state.phase === 'negotiation' ? (
              <NegotiationPanel board={board} meId={meId} selectedTile={selectedTile} send={send} disabled={disabled} />
            ) : state.phase === 'declaration' || state.phase === 'secret_planning' ? (
              <OrderPanel
                phase={state.phase}
                board={board}
                meId={meId}
                you={state.you}
                myDeclaration={state.myDeclaration}
                myOrders={state.myOrders}
                selectedTile={selectedTile}
                onSelectTile={setSelectedTile}
                send={send}
                disabled={disabled}
                sfx={sfx}
              />
            ) : state.phase === 'declaration_reveal' ? (
              <RevealPanel state={state} nameOf={nameOf} />
            ) : state.phase === 'resolution' ? (
              <ResolutionOverlay resolution={state.resolution} nameOf={nameOf} reducedMotion={prefs.reducedMotion} sfx={sfx} />
            ) : state.phase === 'round_summary' ? (
              <RoundSummary summary={state.roundSummary} nameOf={nameOf} />
            ) : (
              <p className="muted">Waiting…</p>
            )}
          </div>

          <Chat messages={state.chat} nameOf={nameOf} send={send} canPost={!isSpectator} disabled={disabled} />
        </aside>
      </div>
    </div>
  )
}

function RevealPanel({ state, nameOf }) {
  return (
    <div className="reveal-panel">
      <h3>Declarations revealed</h3>
      <ul className="reveal-list">
        {(state.revealedDeclarations || []).map((d) => (
          <li key={d.player}>
            <strong>{nameOf(d.player)}</strong>: {describeCommand(d.command)}
          </li>
        ))}
      </ul>
      <p className="muted small">Remember — any one of these could be a Faux Order.</p>
    </div>
  )
}

function SpectatorPanel({ state }) {
  return (
    <div className="spectator-panel">
      <p className="muted">You are watching. Public information only.</p>
      <p className="small">Declarations in: {state.declarationsIn}/{state.requiredCount} · Orders locked: {state.ordersLocked}/{state.requiredCount}</p>
    </div>
  )
}

function ExitDialog({ onLeaveForNow, onResign, isSpectator }) {
  return (
    <AlertDialog.Root>
      <AlertDialog.Trigger asChild>
        <button className="icon-btn" aria-label="Leave"><LogOut size={18} aria-hidden="true" /></button>
      </AlertDialog.Trigger>
      <AlertDialog.Portal>
        <AlertDialog.Overlay className="dialog-overlay" />
        <AlertDialog.Content className="dialog-content">
          <AlertDialog.Title>Leave this match?</AlertDialog.Title>
          <AlertDialog.Description className="muted">
            {isSpectator ? 'Stop watching and return home.' : 'Leave for now keeps your seat — you can resume. Resigning is permanent and gives up your territory.'}
          </AlertDialog.Description>
          <div className="dialog-actions">
            <AlertDialog.Cancel asChild><button className="btn-ghost">Stay</button></AlertDialog.Cancel>
            {!isSpectator && <button className="btn-secondary" onClick={onLeaveForNow}>Leave for now</button>}
            <AlertDialog.Action asChild>
              <button className="btn-danger" onClick={onResign}>{isSpectator ? 'Leave' : 'Resign permanently'}</button>
            </AlertDialog.Action>
          </div>
        </AlertDialog.Content>
      </AlertDialog.Portal>
    </AlertDialog.Root>
  )
}

// useMapEffects tracks the transient, unsequenced map pings, proposal arrows,
// and voice speaking state, expiring them after a short window.
function useMapEffects(subscribe) {
  const [pings, setPings] = useState([])
  const [proposals, setProposals] = useState([])
  const [speakingIds, setSpeakingIds] = useState(new Set())

  useEffect(() => {
    return subscribe((msg) => {
      const p = msg.payload || {}
      if (msg.type === 'map_ping') {
        const id = `${p.tile}-${Math.random()}`
        setPings((prev) => [...prev, { id, tile: p.tile }])
        setTimeout(() => setPings((prev) => prev.filter((x) => x.id !== id)), 1600)
      } else if (msg.type === 'proposal_arrow') {
        const id = `${p.fromTile}-${p.toTile}-${Math.random()}`
        setProposals((prev) => [...prev, { id, from: p.fromTile, to: p.toTile }])
        setTimeout(() => setProposals((prev) => prev.filter((x) => x.id !== id)), 4000)
      } else if (msg.type === 'voice_state') {
        setSpeakingIds((prev) => {
          const next = new Set(prev)
          if (p.speaking) next.add(p.id)
          else next.delete(p.id)
          return next
        })
      }
    })
  }, [subscribe])

  return { pings, proposals, speakingIds }
}
