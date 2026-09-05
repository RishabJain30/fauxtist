import { T } from './protocol.js'

// Dispatched locally by useRoomSocket (never sent by the server) when a
// join/reconnect attempt is rejected and the socket closes before ever
// reaching a snapshot, so the UI can stop showing "Connecting…" forever.
export const LOCAL_JOIN_FAILED = 'local:join_failed'

// Dispatched locally by useRoomSocket once it has decided (via
// sequencing.js) that an incoming state_snapshot is safe to apply — the
// single atomic, full-state-replacing action. Everything the UI needs for
// every phase comes from this one action; no other action may leave the
// screen showing a mix of two different snapshots' worth of state.
export const STATE_SNAPSHOT_RECEIVED = 'local:state_snapshot_received'

// Dispatched locally by Voting.jsx the moment it sends cast_vote, so a
// refresh or resync a moment later doesn't show the vote buttons as if
// nothing had been sent yet. The next snapshot's authoritative hasVoted
// always overwrites this — it's purely to bridge the gap between sending
// and the server's own broadcast confirming it.
export const LOCAL_VOTE_CAST = 'local:vote_cast'

export function initialState() {
  return {
    phase: 'connecting',
    players: [],
    hostId: null,
    you: null,
    round: 0,
    totalRounds: 0,
    category: '',
    word: null,
    youAreImpostor: false,
    currentPlayer: null,
    lap: 0,
    totalLaps: 2,
    strokes: [],
    discussionDeadlineMs: null,
    hasVoted: false,
    votesCast: 0,
    votesTotal: 0,
    voteTargets: [],
    lastResult: null,
    guessDeadlineMs: null,
    finalScores: null,
    chat: [],
    error: null,
    errorCode: null,
    voicePeers: [],
    voiceStates: {},
  }
}

// snapshotToState maps a state_snapshot payload onto every "core" field
// initialState defines, explicitly defaulting whatever the payload didn't
// include for the current phase — so applying a snapshot is a genuine
// full replace, never a merge that could leave a stale field over from
// whatever phase the client was previously in.
function snapshotToState(p) {
  return {
    phase: p.phase,
    players: p.players || [],
    hostId: p.hostId ?? null,
    you: p.you ?? null,
    round: p.round ?? 0,
    totalRounds: p.totalRounds ?? 0,
    category: p.category || '',
    word: p.word ?? null,
    youAreImpostor: !!p.youAreImpostor,
    currentPlayer: p.currentPlayer ?? null,
    lap: p.lap ?? 0,
    totalLaps: p.totalLaps ?? 2,
    strokes: p.strokes || [],
    discussionDeadlineMs: p.discussionDeadlineMs ?? null,
    hasVoted: !!p.hasVoted,
    votesCast: p.votesCast ?? 0,
    votesTotal: p.votesTotal ?? 0,
    voteTargets: p.voteTargets || [],
    lastResult: p.lastResult ?? null,
    guessDeadlineMs: p.guessDeadlineMs ?? null,
    finalScores: p.finalScores ?? null,
    error: null,
    errorCode: null,
  }
}

export function reduce(state, msg) {
  const p = msg.payload || {}
  switch (msg.type) {
    case STATE_SNAPSHOT_RECEIVED:
      // Local-only UI state that a snapshot has no opinion on and would
      // otherwise be lost for no reason: chat history (not part of the
      // reconstructible game state) and voice presence (its own
      // independent join/leave protocol, out of scope for this
      // milestone).
      return { ...snapshotToState(p), chat: state.chat, voicePeers: state.voicePeers, voiceStates: state.voiceStates }
    case T.LobbyUpdate:
      return { ...state, players: p.players || [], hostId: p.hostId ?? state.hostId }
    case T.PlayerLeft:
      // The player was actually removed from the roster (lobby-only, after
      // their reconnect grace expired) — not just marked disconnected.
      return { ...state, players: state.players.filter((pl) => pl.id !== p.id) }
    case T.PlayerPresenceChanged:
      return { ...state, players: state.players.map((pl) => (pl.id === p.id ? { ...pl, connected: p.connected } : pl)) }
    case T.HostChanged:
      return { ...state, hostId: p.hostId }
    case T.RoundStarted:
      return {
        ...state,
        phase: 'drawing',
        round: p.round,
        category: p.category,
        word: p.word ?? null,
        youAreImpostor: !!p.youAreImpostor,
        strokes: [],
        lastResult: null,
        hasVoted: false,
        votesCast: 0,
        votesTotal: 0,
      }
    case T.StrokeBroadcast:
      return { ...state, strokes: [...state.strokes, p] }
    case T.TurnChanged:
      return { ...state, currentPlayer: p.currentPlayer, lap: p.lap, totalLaps: p.totalLaps }
    case T.PhaseChanged:
      return { ...state, phase: p.phase }
    case T.VoteUpdate:
      return { ...state, votesCast: p.votesCast, votesTotal: p.votesTotal }
    case LOCAL_VOTE_CAST:
      return { ...state, hasVoted: true }
    case T.RoundResult:
      return { ...state, lastResult: p, phase: 'reveal', guessDeadlineMs: p.guessDeadlineMs ?? null }
    case T.GameOver:
      return { ...state, phase: 'game_over', finalScores: p.finalScores || [] }
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
