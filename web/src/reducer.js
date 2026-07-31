import { T } from './protocol.js'

export function initialState() {
  return {
    phase: 'connecting',
    players: [],
    hostId: null,
    round: 0,
    totalRounds: 0,
    category: '',
    word: null,
    youAreImpostor: false,
    currentPlayer: null,
    lap: 0,
    totalLaps: 2,
    strokes: [],
    votesCast: 0,
    votesTotal: 0,
    lastResult: null,
    finalScores: null,
    chat: [],
    error: null,
    voicePeers: [],
    voiceStates: {},
  }
}

export function reduce(state, msg) {
  const p = msg.payload || {}
  switch (msg.type) {
    case T.RoomState:
      return {
        ...state,
        phase: p.phase,
        players: p.players || [],
        hostId: p.hostId ?? state.hostId,
        round: p.round ?? 0,
        totalRounds: p.totalRounds ?? 0,
        category: p.category || '',
        word: p.word ?? null,
        youAreImpostor: !!p.youAreImpostor,
        strokes: p.strokes || [],
        lap: p.lap ?? 0,
        totalLaps: p.totalLaps ?? 2,
        lastResult: p.lastResult ?? null,
      }
    case T.LobbyUpdate:
      return { ...state, players: p.players || [], hostId: p.hostId ?? state.hostId }
    case T.PlayerLeft:
      return { ...state, players: state.players.map((pl) => (pl.id === p.id ? { ...pl, gone: true } : pl)) }
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
        votesCast: 0,
      }
    case T.StrokeBroadcast:
      return { ...state, strokes: [...state.strokes, p] }
    case T.TurnChanged:
      return { ...state, currentPlayer: p.currentPlayer, lap: p.lap, totalLaps: p.totalLaps }
    case T.PhaseChanged:
      return { ...state, phase: p.phase }
    case T.VoteUpdate:
      return { ...state, votesCast: p.votesCast, votesTotal: p.votesTotal }
    case T.RoundResult:
      return { ...state, lastResult: p, phase: 'reveal' }
    case T.GameOver:
      return { ...state, phase: 'game_over', finalScores: p.finalScores || [] }
    case T.ChatBroadcast:
      return { ...state, chat: [...state.chat, p] }
    case T.Error:
      return { ...state, error: p.message }
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
