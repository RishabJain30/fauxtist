package wsproto

import "errors"

// maxRequestIDBytes and maxPayloadBytes bound an inbound client envelope's
// own shape, ahead of any per-message-type semantic validation (stroke
// bounds, chat length, etc. — see internal/room/validate.go). Every real
// client command already carries a requestId (see web/src/protocol.js's
// encodeCommand), so requiring one here costs nothing for a legitimate
// client while giving a forged or garbled envelope one more reason to be
// rejected before it ever reaches a room actor.
const (
	maxRequestIDBytes = 128
	// maxPayloadBytes must comfortably fit a maximally-sized stroke's
	// JSON encoding (see internal/room/validate.go's maxStrokePoints) —
	// every other message type's payload is far smaller than this on its
	// own, tighter dedicated limits.
	maxPayloadBytes = 32 * 1024
)

// clientCommandTypes is every message type a client may legitimately send
// once a session is established. Join is deliberately excluded — it is
// only ever valid as the very first frame, handled by its own dedicated
// path (readJoinFrame), never through Submit.
var clientCommandTypes = map[string]bool{
	TypeStartGame:        true,
	TypeStroke:           true,
	TypeChatMessage:      true,
	TypeCastVote:         true,
	TypeImpostorGuess:    true,
	TypeEndDiscussion:    true,
	TypeVoiceJoin:        true,
	TypeVoiceLeave:       true,
	TypeVoiceSignal:      true,
	TypeVoiceState:       true,
	TypeNewGame:          true,
	TypeResync:           true,
	TypeIceConfigRequest: true,
}

var (
	ErrUnknownType     = errors.New("unknown message type")
	ErrRequestIDShape  = errors.New("requestId must be non-empty and bounded")
	ErrPayloadTooLarge = errors.New("payload too large")
)

// ValidateEnvelope checks a decoded, already version-matched envelope's own
// shape before it is ever handed to a room actor. Protocol version is
// checked separately by the caller (readJoinFrame/readLoop) since its
// rejection semantics differ — a dedicated close code, not a dropped
// frame.
func ValidateEnvelope(env Envelope) error {
	if !clientCommandTypes[env.Type] {
		return ErrUnknownType
	}
	if env.RequestID == "" || len(env.RequestID) > maxRequestIDBytes {
		return ErrRequestIDShape
	}
	if len(env.Payload) > maxPayloadBytes {
		return ErrPayloadTooLarge
	}
	return nil
}
