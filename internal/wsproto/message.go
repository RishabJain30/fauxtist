package wsproto

import "encoding/json"

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

	// Server -> client
	TypeRoomState       = "room_state"
	TypePlayerJoined    = "player_joined"
	TypePlayerLeft      = "player_left"
	TypeLobbyUpdate     = "lobby_update"
	TypeRoundStarted    = "round_started"
	TypeStrokeBroadcast = "stroke_broadcast"
	TypeTurnChanged     = "turn_changed"
	TypePhaseChanged    = "phase_changed"
	TypeVoteUpdate      = "vote_update"
	TypeRoundResult     = "round_result"
	TypeGameOver        = "game_over"
	TypeChatBroadcast   = "chat_broadcast"
	TypeError           = "error"
)

// Envelope is the outer wire frame for every message.
type Envelope struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Encode builds an Envelope from a typed payload.
func Encode(t string, payload any) (Envelope, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{Type: t, Payload: b}, nil
}

// ---- Client -> server payloads ----

type Point struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type JoinPayload struct {
	Name           string `json:"name"`
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
}
