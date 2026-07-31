export const T = {
  // client -> server
  Join: 'join',
  StartGame: 'start_game',
  Stroke: 'stroke',
  ChatMessage: 'chat_message',
  CastVote: 'cast_vote',
  ImpostorGuess: 'impostor_guess',
  EndDiscussion: 'end_discussion',
  // server -> client
  RoomState: 'room_state',
  LobbyUpdate: 'lobby_update',
  PlayerLeft: 'player_left',
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
  // voice (server -> client)
  VoicePeers: 'voice_peers',
  VoicePeerJoined: 'voice_peer_joined',
  VoicePeerLeft: 'voice_peer_left',
}
