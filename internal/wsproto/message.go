package wsproto

import "encoding/json"

// ProtocolVersion is the current wire protocol version. Every envelope in
// both directions carries it; the server rejects anything else at the
// join frame with CloseUnsupportedVersion (see docs/protocol.md).
const ProtocolVersion = 1

// Close codes are WebSocket close codes in the private-use range
// (4000-4999, RFC 6455 §7.4.2) for protocol-level rejections that happen
// before or outside the existing structured-error-frame + 1008 path used
// for join/reconnect business-rule failures (name_taken, room_full, etc).
const (
	CloseUnsupportedVersion = 4001
	CloseInvalidEnvelope    = 4002
)

// Message type constants. Client->server and server->client share one namespace.
const (
	// Client -> server
	TypeJoin          = "join"
	TypeStartGame     = "start_game"
	TypeStroke        = "stroke"
	TypeChatMessage   = "chat_message"
	TypeCastVote      = "cast_vote"
	TypeImpostorGuess = "impostor_guess"
	TypeEndDiscussion = "end_discussion"
	TypeVoiceJoin     = "voice_join"
	TypeVoiceLeave    = "voice_leave"
	TypeVoiceSignal   = "voice_signal"
	TypeVoiceState    = "voice_state"
	TypeNewGame       = "new_game"
	TypeResync        = "resync"

	// Server -> client
	TypeStateSnapshot         = "state_snapshot"
	TypeJoinAccepted          = "join_accepted"
	TypePlayerJoined          = "player_joined"
	TypePlayerLeft            = "player_left"
	TypePlayerPresenceChanged = "player_presence_changed"
	TypeHostChanged           = "host_changed"
	TypeLobbyUpdate           = "lobby_update"
	TypeRoundStarted          = "round_started"
	TypeStrokeBroadcast       = "stroke_broadcast"
	TypeTurnChanged           = "turn_changed"
	TypePhaseChanged          = "phase_changed"
	TypeVoteUpdate            = "vote_update"
	TypeRoundResult           = "round_result"
	TypeGameOver              = "game_over"
	TypeChatBroadcast         = "chat_broadcast"
	TypeError                 = "error"
	TypeVoicePeers            = "voice_peers"
	TypeVoicePeerJoined       = "voice_peer_joined"
	TypeVoicePeerLeft         = "voice_peer_left"
)

// Envelope is the outer wire frame for every message in both directions.
// RoomID and Seq are stamped by the room on every outbound message (Seq is
// the room's authoritative revision at send time — see Room.stamp);
// RequestID is set by the client on outbound commands and echoed back on
// any error produced by that specific command, for client-side
// correlation. Fields unused in a given direction are omitted rather than
// sent as zero values.
type Envelope struct {
	Version   int             `json:"version"`
	Type      string          `json:"type"`
	RoomID    string          `json:"roomId,omitempty"`
	Seq       int64           `json:"seq,omitempty"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

// Encode builds an Envelope from a typed payload, stamped with the current
// protocol version. RoomID/Seq (server->client) or RequestID (client->
// server) are added afterward by the caller, once known.
func Encode(t string, payload any) (Envelope, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Version: ProtocolVersion, Type: t, Payload: b}, nil
}

// ---- Client -> server payloads ----

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// JoinPayload is a client's join or reconnect attempt: a new join carries
// Name/Emoji; a reconnect carries PlayerID/ReconnectToken instead.
type JoinPayload struct {
	Name           string `json:"name,omitempty"`
	Emoji          string `json:"emoji,omitempty"`
	PlayerID       string `json:"playerId,omitempty"`
	ReconnectToken string `json:"reconnectToken,omitempty"`
}

type StrokePayload struct {
	Points []Point `json:"points"`
	Color  string  `json:"color"`
	Width  float64 `json:"width"`
}

type ChatPayload struct {
	Text string `json:"text"`
}

type VotePayload struct {
	Target string `json:"target"`
}

type ImpostorGuessPayload struct {
	Guess string `json:"guess"`
}

// ---- Server -> client payloads ----

type PhaseChangedPayload struct {
	Phase string `json:"phase"`
}

type TurnChangedPayload struct {
	CurrentPlayer string `json:"currentPlayer"`
	Lap           int    `json:"lap"`
	TotalLaps     int    `json:"totalLaps"`
}

type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// JoinAcceptedPayload hands a newly joined (non-reconnecting) player their
// server-minted seat credentials. Sent privately, only to that player.
type JoinAcceptedPayload struct {
	PlayerID       string `json:"playerId"`
	ReconnectToken string `json:"reconnectToken"`
}

// PlayerView is a player as seen by clients: the engine's authoritative
// fields plus room-tracked connection presence, which the engine itself has
// no notion of. Never carries a reconnect token, token hash, or connection
// id — those never leave the room's own memory.
type PlayerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	Score     int    `json:"score"`
	Connected bool   `json:"connected"`
}

// PlayerPresenceChangedPayload announces that one player's connection
// status flipped, without implying they were removed from the roster.
type PlayerPresenceChangedPayload struct {
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
}

// HostChangedPayload announces deterministic host migration.
type HostChangedPayload struct {
	HostID string `json:"hostId"`
}

// VoiceSignalIn is a client's signaling message addressed to another peer.
type VoiceSignalIn struct {
	To      string          `json:"to"`
	Kind    string          `json:"kind"` // offer | answer | ice
	Payload json.RawMessage `json:"payload"`
}

// VoiceStateIn is a client's current mic state.
type VoiceStateIn struct {
	Muted    bool `json:"muted"`
	Speaking bool `json:"speaking"`
}
