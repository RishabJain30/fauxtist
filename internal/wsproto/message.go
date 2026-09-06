package wsproto

import "encoding/json"

// ProtocolVersion is the current wire protocol version. Every envelope in
// both directions carries it; the server rejects any other version at the
// join frame with CloseUnsupportedVersion. Fauxlands' strategy protocol is
// intentionally incompatible with the previous drawing-game protocol
// (version 1), so an old client fails cleanly rather than half-working.
const ProtocolVersion = 2

// Close codes are WebSocket close codes in the private-use range
// (4000-4999, RFC 6455 §7.4.2) for protocol-level rejections outside the
// structured-error-frame + 1008 path used for join business-rule failures.
const (
	CloseUnsupportedVersion = 4001
	CloseInvalidEnvelope    = 4002
	// CloseRoomClosed is sent when a room is torn down under a still-
	// connected client (idle expiry or process shutdown), distinct from a
	// normal 1000 so the client's reconnect logic knows the room is gone.
	CloseRoomClosed = 4003
)

// Message type constants. Client->server and server->client share one
// namespace.
const (
	// ---- Client -> server ----
	TypeJoin             = "join"
	TypeSetReady         = "set_ready"
	TypeUpdateSettings   = "update_settings"
	TypeStartMatch       = "start_match"
	TypeSubmitDecl       = "submit_declaration"
	TypeSetOrders        = "set_orders"
	TypeLockOrders       = "lock_orders"
	TypeUnlockOrders     = "unlock_orders"
	TypeMapPing          = "map_ping"
	TypeProposalArrow    = "proposal_arrow"
	TypeChatMessage      = "chat_message"
	TypeLeaveForNow      = "leave_for_now"
	TypeResignMatch      = "resign_match"
	TypeEndNoContest     = "end_no_contest"
	TypeKeepWaiting      = "keep_waiting"
	TypeRematchReady     = "rematch_ready"
	TypeStartRematch     = "start_rematch"
	TypeReturnToLobby    = "return_to_lobby"
	TypeClaimSeat        = "claim_seat"
	TypeRemovePlayer     = "remove_player" // host-only lobby moderation
	TypeResync           = "resync"
	TypeVoiceJoin        = "voice_join"
	TypeVoiceLeave       = "voice_leave"
	TypeVoiceSignal      = "voice_signal"
	TypeVoiceState       = "voice_state"
	TypeIceConfigRequest = "ice_config_request"

	// ---- Server -> client ----
	TypeStateSnapshot         = "state_snapshot"
	TypeJoinAccepted          = "join_accepted"
	TypeLobbyUpdate           = "lobby_update"
	TypeSettingsChanged       = "settings_changed"
	TypeMatchStarted          = "match_started"
	TypePhaseChanged          = "phase_changed"
	TypeDeclarationStatus     = "declaration_status"
	TypeDeclarationsRevealed  = "declarations_revealed"
	TypeOrdersSaved           = "orders_saved"
	TypePlanningStatus        = "planning_status"
	TypeRoundResolved         = "round_resolved"
	TypeRoundSummary          = "round_summary"
	TypePlayerPresenceChanged = "player_presence_changed"
	TypePlayerAFKChanged      = "player_afk_changed"
	TypePlayerExited          = "player_exited"
	TypeHostChanged           = "host_changed"
	TypeSpectatorUpdate       = "spectator_update"
	TypeRematchStatus         = "rematch_status"
	TypeGameOver              = "game_over"
	TypeLeaveAccepted         = "leave_accepted"
	TypeChatBroadcast         = "chat_broadcast"
	TypeError                 = "error"
	TypeVoicePeers            = "voice_peers"
	TypeVoicePeerJoined       = "voice_peer_joined"
	TypeVoicePeerLeft         = "voice_peer_left"
	TypeIceConfig             = "ice_config"
)

// Envelope is the outer wire frame for every message in both directions.
// RoomID and Seq are stamped by the room on every outbound message (Seq is
// the room's authoritative revision at send time); RequestID is set by the
// client on outbound commands and echoed on any error for that command.
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
// server) are added afterward by the caller.
func Encode(t string, payload any) (Envelope, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Version: ProtocolVersion, Type: t, Payload: b}, nil
}

// ---- Client -> server payloads ----

// JoinPayload is a client's join, reconnect, or spectate attempt: a new join
// carries Name/Emoji; a reconnect carries PlayerID/ReconnectToken; AsSpectator
// requests a read-only seat when the match has already started.
type JoinPayload struct {
	Name           string `json:"name,omitempty"`
	Emoji          string `json:"emoji,omitempty"`
	PlayerID       string `json:"playerId,omitempty"`
	ReconnectToken string `json:"reconnectToken,omitempty"`
	AsSpectator    bool   `json:"asSpectator,omitempty"`
}

// CommandWire is the wire form of one game command. The room converts it to a
// game.Command after validating the type string.
type CommandWire struct {
	Type   string `json:"type"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Armies int    `json:"armies,omitempty"`
}

type SetReadyPayload struct {
	Ready bool `json:"ready"`
}

type UpdateSettingsPayload struct {
	Preset string `json:"preset"`
}

type SubmitDeclPayload struct {
	Command CommandWire `json:"command"`
}

type SetOrdersPayload struct {
	Commands []CommandWire `json:"commands"`
	Faux     bool          `json:"faux"`
}

type ChatPayload struct {
	Text string `json:"text"`
}

type MapPingPayload struct {
	Tile string `json:"tile"`
}

type ProposalArrowPayload struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type ClaimSeatPayload struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

type RemovePlayerPayload struct {
	PlayerID string `json:"playerId"`
}

// ---- Server -> client shared payloads ----

// ErrorPayload is a typed, machine-readable error.
type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// JoinAcceptedPayload hands a newly joined player (or seat-claiming
// spectator) their server-minted seat credentials. Sent privately.
type JoinAcceptedPayload struct {
	PlayerID       string `json:"playerId"`
	ReconnectToken string `json:"reconnectToken"`
	Spectator      bool   `json:"spectator"`
}

// PlayerView is a player as seen by clients: engine-authoritative identity
// plus room-tracked connection/readiness/AFK. Never carries a token, hash, or
// connection id.
type PlayerView struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Emoji            string `json:"emoji"`
	Faction          string `json:"faction,omitempty"`
	SpawnSlot        int    `json:"spawnSlot"`
	Energy           int    `json:"energy"`
	Influence        int    `json:"influence"`
	FauxAvailable    bool   `json:"fauxAvailable"`
	FauxUsedRound    int    `json:"fauxUsedRound"`
	DominationStreak int    `json:"dominationStreak"`
	Forfeited        bool   `json:"forfeited"`
	Connected        bool   `json:"connected"`
	Ready            bool   `json:"ready"`
	AFK              bool   `json:"afk"`
}

// SpectatorView is a read-only watcher.
type SpectatorView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Emoji     string `json:"emoji"`
	Connected bool   `json:"connected"`
}

// PlayerPresenceChangedPayload announces a connection-status flip.
type PlayerPresenceChangedPayload struct {
	ID        string `json:"id"`
	Connected bool   `json:"connected"`
}

// PlayerAFKChangedPayload announces an AFK-status flip.
type PlayerAFKChangedPayload struct {
	ID  string `json:"id"`
	AFK bool   `json:"afk"`
}

// PlayerExitedPayload announces a permanent departure (resign or lobby leave).
type PlayerExitedPayload struct {
	ID        string `json:"id"`
	Forfeited bool   `json:"forfeited"`
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

// IceServer is one entry of an RTCPeerConnection's iceServers config.
type IceServer struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username,omitempty"`
	Credential string   `json:"credential,omitempty"`
}

// IceConfigPayload answers a TypeIceConfigRequest.
type IceConfigPayload struct {
	IceServers []IceServer `json:"iceServers"`
}
