// PROTOCOL_VERSION is the wire protocol version this client speaks. The
// server rejects a join frame declaring any other version with a dedicated
// close code — see docs/protocol.md. Fauxlands' strategy protocol is v2 and
// is intentionally incompatible with the old drawing-game protocol (v1).
export const PROTOCOL_VERSION = 2

export const T = {
  // client -> server
  Join: 'join',
  SetReady: 'set_ready',
  UpdateSettings: 'update_settings',
  StartMatch: 'start_match',
  SubmitDeclaration: 'submit_declaration',
  SetOrders: 'set_orders',
  LockOrders: 'lock_orders',
  UnlockOrders: 'unlock_orders',
  MapPing: 'map_ping',
  ProposalArrow: 'proposal_arrow',
  ChatMessage: 'chat_message',
  LeaveForNow: 'leave_for_now',
  ResignMatch: 'resign_match',
  EndNoContest: 'end_no_contest',
  KeepWaiting: 'keep_waiting',
  RematchReady: 'rematch_ready',
  StartRematch: 'start_rematch',
  ReturnToLobby: 'return_to_lobby',
  ClaimSeat: 'claim_seat',
  RemovePlayer: 'remove_player',
  Resync: 'resync',
  VoiceJoin: 'voice_join',
  VoiceLeave: 'voice_leave',
  VoiceSignal: 'voice_signal',
  VoiceState: 'voice_state',
  IceConfigRequest: 'ice_config_request',

  // server -> client
  StateSnapshot: 'state_snapshot',
  JoinAccepted: 'join_accepted',
  LobbyUpdate: 'lobby_update',
  SettingsChanged: 'settings_changed',
  PhaseChanged: 'phase_changed',
  DeclarationStatus: 'declaration_status',
  DeclarationsRevealed: 'declarations_revealed',
  OrdersSaved: 'orders_saved',
  PlanningStatus: 'planning_status',
  RoundResolved: 'round_resolved',
  RoundSummary: 'round_summary',
  PlayerPresenceChanged: 'player_presence_changed',
  PlayerAFKChanged: 'player_afk_changed',
  PlayerExited: 'player_exited',
  HostChanged: 'host_changed',
  SpectatorUpdate: 'spectator_update',
  RematchStatus: 'rematch_status',
  GameOver: 'game_over',
  LeaveAccepted: 'leave_accepted',
  ChatBroadcast: 'chat_broadcast',
  Error: 'error',
  VoicePeers: 'voice_peers',
  VoicePeerJoined: 'voice_peer_joined',
  VoicePeerLeft: 'voice_peer_left',
  IceConfig: 'ice_config',
}

// SEQUENCED_TYPES are the public server->client messages that carry the
// room's authoritative revision as `seq` and must be applied in order. It
// mirrors exactly the set the server bumps its revision for. Private
// (orders_saved), transient (chat, voice, pings, proposals), and
// handshake (join_accepted, leave_accepted, error, ice_config) messages are
// deliberately unsequenced.
export const SEQUENCED_TYPES = new Set([
  T.StateSnapshot,
  T.LobbyUpdate,
  T.SettingsChanged,
  T.PhaseChanged,
  T.DeclarationStatus,
  T.DeclarationsRevealed,
  T.PlanningStatus,
  T.RoundResolved,
  T.RoundSummary,
  T.PlayerPresenceChanged,
  T.PlayerAFKChanged,
  T.PlayerExited,
  T.HostChanged,
  T.SpectatorUpdate,
  T.RematchStatus,
  T.GameOver,
])

function newRequestId() {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `req_${Math.random().toString(36).slice(2)}_${Math.random().toString(36).slice(2)}`
}

// encodeCommand builds the wire string for one client->server command. The
// single place a command envelope is constructed.
export function encodeCommand(type, payload = {}) {
  const requestId = newRequestId()
  return { requestId, raw: JSON.stringify({ version: PROTOCOL_VERSION, type, requestId, payload }) }
}

// parseServerMessage safely parses one incoming frame into an envelope, or
// returns null (a malformed or wrong-version frame must never crash the
// client).
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
  console.warn(`[fauxlands protocol] ${message}`)
}
