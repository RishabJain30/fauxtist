import { T } from './protocol.js'

// Local-only actions (never sent by the server), dispatched by the connection
// controller so the UI can react to connection-level events.
export const LOCAL_JOIN_FAILED = 'local:join_failed'
export const STATE_SNAPSHOT_RECEIVED = 'local:state_snapshot_received'

export function initialState() {
  return {
    phase: 'connecting',
    phaseDeadlineMs: null,
    earlyDeadlineMs: null,
    paused: false,
    round: 0,
    totalRounds: 0,
    preset: 'standard',
    mapId: null,
    hostId: null,
    role: 'player',
    me: null,
    you: null,
    players: [],
    spectators: [],
    board: [],
    chat: [],
    myDeclaration: null,
    myOrders: null,
    declarationsIn: 0,
    ordersSubmitted: 0,
    ordersLocked: 0,
    requiredCount: 0,
    revealedDeclarations: [],
    resolution: null,
    roundSummary: null,
    result: null,
    rematchReady: [],
    error: null,
    errorCode: null,
    voicePeers: [],
    voiceStates: {},
  }
}

// snapshotToState maps a state_snapshot payload onto every core field, so
// applying a snapshot is a genuine full replace, never a merge that could
// leave a stale field from a previous phase.
function snapshotToState(p) {
  return {
    phase: p.phase,
    phaseDeadlineMs: p.phaseDeadlineMs ?? null,
    earlyDeadlineMs: p.earlyDeadlineMs ?? null,
    paused: !!p.paused,
    round: p.round ?? 0,
    totalRounds: p.totalRounds ?? 0,
    preset: p.preset || 'standard',
    mapId: p.mapId ?? null,
    hostId: p.hostId ?? null,
    role: p.role || 'player',
    me: p.me ?? null,
    you: p.you ?? null,
    players: p.players || [],
    spectators: p.spectators || [],
    board: p.board || [],
    chat: p.chat || [],
    myDeclaration: p.myDeclaration ?? null,
    myOrders: p.myOrders ?? null,
    declarationsIn: p.declarationsIn ?? 0,
    ordersSubmitted: p.ordersSubmitted ?? 0,
    ordersLocked: p.ordersLocked ?? 0,
    requiredCount: p.requiredCount ?? 0,
    revealedDeclarations: p.revealedDeclarations || [],
    resolution: p.resolution ?? null,
    roundSummary: p.resolution?.summary ?? null,
    result: p.result ?? null,
    rematchReady: p.rematchReady || [],
    error: null,
    errorCode: null,
  }
}

export function reduce(state, msg) {
  const p = msg.payload || {}
  switch (msg.type) {
    case STATE_SNAPSHOT_RECEIVED:
      // Preserve voice presence (its own independent protocol) across the
      // full replace.
      return { ...snapshotToState(p), voicePeers: state.voicePeers, voiceStates: state.voiceStates }

    case T.PhaseChanged:
      return {
        ...state,
        phase: p.phase,
        round: p.round ?? state.round,
        totalRounds: p.totalRounds ?? state.totalRounds,
        paused: !!p.paused,
        phaseDeadlineMs: p.phaseDeadlineMs ?? null,
        earlyDeadlineMs: null,
        // A new phase clears the previous phase's transient reveal/summary.
        revealedDeclarations: p.phase === 'declaration' ? [] : state.revealedDeclarations,
      }

    case T.DeclarationStatus:
      return { ...state, declarationsIn: p.submitted ?? 0, requiredCount: p.required ?? state.requiredCount, earlyDeadlineMs: p.earlyDeadlineMs ?? state.earlyDeadlineMs }

    case T.DeclarationsRevealed:
      return { ...state, revealedDeclarations: p.declarations || [] }

    case T.PlanningStatus:
      return {
        ...state,
        ordersSubmitted: p.submitted ?? 0,
        ordersLocked: p.locked ?? 0,
        requiredCount: p.required ?? state.requiredCount,
        earlyDeadlineMs: p.earlyDeadlineMs ?? state.earlyDeadlineMs,
      }

    case T.OrdersSaved:
      return { ...state, myOrders: { faux: !!p.faux, commands: p.commands || [], locked: !!p.locked } }

    case T.RoundResolved:
      // round_resolved carries the final authoritative board for the round —
      // apply it so the map stays current (later phases only send
      // phase_changed, not a full board).
      return { ...state, resolution: p.resolution ?? null, board: p.resolution?.board ?? state.board }

    case T.RoundSummary:
      return { ...state, roundSummary: p.summary ?? null }

    case T.LobbyUpdate:
      return {
        ...state,
        players: p.players || [],
        spectators: p.spectators || state.spectators,
        hostId: p.hostId ?? state.hostId,
        preset: p.preset || state.preset,
        totalRounds: p.totalRounds ?? state.totalRounds,
      }

    case T.SettingsChanged:
      return { ...state, preset: p.preset || state.preset, totalRounds: p.totalRounds ?? state.totalRounds }

    case T.PlayerPresenceChanged:
      return { ...state, players: state.players.map((pl) => (pl.id === p.id ? { ...pl, connected: p.connected } : pl)) }

    case T.PlayerAFKChanged:
      return { ...state, players: state.players.map((pl) => (pl.id === p.id ? { ...pl, afk: p.afk } : pl)) }

    case T.PlayerExited:
      return { ...state, players: state.players.filter((pl) => pl.id !== p.id) }

    case T.HostChanged:
      return { ...state, hostId: p.hostId }

    case T.SpectatorUpdate:
      return { ...state, spectators: p.spectators || [] }

    case T.RematchStatus:
      return { ...state, rematchReady: p.ready || [] }

    case T.GameOver:
      return { ...state, phase: 'game_over', result: p.result ?? state.result }

    case T.ChatBroadcast:
      return { ...state, chat: [...state.chat, p] }

    case T.Error:
      return { ...state, error: p.message, errorCode: p.code }

    case LOCAL_JOIN_FAILED:
      return { ...state, phase: 'join_failed' }

    case T.VoicePeers:
      return { ...state, voicePeers: p.ids || [] }

    case T.VoicePeerJoined:
      return {
        ...state,
        voicePeers: state.voicePeers.includes(p.id) ? state.voicePeers : [...state.voicePeers, p.id],
      }

    case T.VoicePeerLeft: {
      const voiceStates = { ...state.voiceStates }
      delete voiceStates[p.id]
      return { ...state, voicePeers: state.voicePeers.filter((id) => id !== p.id), voiceStates }
    }

    case T.VoiceState:
      return { ...state, voiceStates: { ...state.voiceStates, [p.id]: { muted: p.muted, speaking: p.speaking } } }

    default:
      return state
  }
}
