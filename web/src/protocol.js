// PROTOCOL_VERSION is the wire protocol version this client speaks. The
// server rejects a join frame declaring any other version with a
// structured error and a dedicated close code — see docs/protocol.md.
export const PROTOCOL_VERSION = 1

export const T = {
  // client -> server
  Join: 'join',
  StartGame: 'start_game',
  Stroke: 'stroke',
  ChatMessage: 'chat_message',
  CastVote: 'cast_vote',
  ImpostorGuess: 'impostor_guess',
  EndDiscussion: 'end_discussion',
  Resync: 'resync',
  IceConfigRequest: 'ice_config_request',
  // server -> client
  StateSnapshot: 'state_snapshot',
  JoinAccepted: 'join_accepted',
  LobbyUpdate: 'lobby_update',
  PlayerLeft: 'player_left',
  PlayerPresenceChanged: 'player_presence_changed',
  HostChanged: 'host_changed',
  RoundStarted: 'round_started',
  StrokeBroadcast: 'stroke_broadcast',
  TurnChanged: 'turn_changed',
  PhaseChanged: 'phase_changed',
  VoteUpdate: 'vote_update',
  RoundResult: 'round_result',
  GameOver: 'game_over',
  ChatBroadcast: 'chat_broadcast',
  Error: 'error',
  // voice (client -> server)
  VoiceJoin: 'voice_join',
  VoiceLeave: 'voice_leave',
  VoiceSignal: 'voice_signal',
  VoiceState: 'voice_state',
  NewGame: 'new_game',
  // voice (server -> client)
  VoicePeers: 'voice_peers',
  VoicePeerJoined: 'voice_peer_joined',
  VoicePeerLeft: 'voice_peer_left',
  IceConfig: 'ice_config',
}

// SEQUENCED_TYPES are the server->client messages that carry the room's
// authoritative revision as `seq` and must be applied in order: every type
// the server bumps its revision for (see internal/room's apply/
// processJoin/processLeave/handleGraceExpired). Chat and voice messages are
// deliberately not sequenced — they're not part of the snapshot-
// reconstructible room/game state, so out-of-order or duplicate delivery
// of one of those has no correctness impact worth gating on.
export const SEQUENCED_TYPES = new Set([
  T.StateSnapshot,
  T.LobbyUpdate,
  T.PlayerLeft,
  T.PlayerPresenceChanged,
  T.HostChanged,
  T.RoundStarted,
  T.StrokeBroadcast,
  T.TurnChanged,
  T.PhaseChanged,
  T.VoteUpdate,
  T.RoundResult,
  T.GameOver,
])

function newRequestId() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `req_${Math.random().toString(36).slice(2)}_${Math.random().toString(36).slice(2)}`
}

// encodeCommand builds the wire string for one client->server command. This
// is the single place a command envelope is constructed — components and
// hooks call send(type, payload) and never build a `{type, payload}`
// literal themselves.
export function encodeCommand(type, payload = {}) {
  const requestId = newRequestId()
  return { requestId, raw: JSON.stringify({ version: PROTOCOL_VERSION, type, requestId, payload }) }
}

// parseServerMessage safely parses one incoming WebSocket frame into an
// envelope, or returns null (logging why, in development only, without
// the payload) rather than throwing — a malformed or unexpected frame from
// the server must never crash the client.
export function parseServerMessage(raw) {
  let msg
  try {
    msg = JSON.parse(raw)
  } catch {
    devLog('could not parse server message as JSON')
    return null
  }
  if (!msg || typeof msg !== 'object' || typeof msg.type !== 'string') {
    devLog('server message missing a valid type')
    return null
  }
  if (msg.version !== PROTOCOL_VERSION) {
    devLog(`server message version ${msg.version} != client version ${PROTOCOL_VERSION}`)
    return null
  }
  return {
    version: msg.version,
    type: msg.type,
    roomId: msg.roomId,
    seq: typeof msg.seq === 'number' ? msg.seq : undefined,
    requestId: msg.requestId,
    payload: msg.payload || {},
  }
}

function devLog(message) {
  if (typeof process !== 'undefined' && process.env?.NODE_ENV === 'production') return
  // eslint-disable-next-line no-console
  console.warn(`[fauxtist protocol] ${message}`)
}
