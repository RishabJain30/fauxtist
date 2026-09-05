package room

import (
	"errors"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// maxNameRunes bounds display-name length.
const maxNameRunes = 24

var (
	errNameEmpty   = errors.New("name must not be empty")
	errNameTooLong = errors.New("name must be at most 24 characters")
	errNameControl = errors.New("name must not contain control characters")
)

// ValidatePlayerName trims and validates a player-chosen display name.
func ValidatePlayerName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", errNameEmpty
	}
	if utf8.RuneCountInString(name) > maxNameRunes {
		return "", errNameTooLong
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", errNameControl
		}
	}
	return name, nil
}

// emojiPalette mirrors web/src/emoji.js's EMOJIS list exactly; the first
// entry is the client's own default. Kept as the single source of truth
// server-side so a forged join can never set an avatar outside the set the
// UI actually offers.
var emojiPalette = []string{"🦊", "🐙", "🐸", "🦉", "🐨", "🦁", "🐵", "🦄", "🐼", "🐧", "🦔", "🐝"}

var errEmojiUnsupported = errors.New("unsupported emoji")

// validateEmoji accepts the empty string (mapped to the default emoji, for
// a client that omits the field) or exactly one palette entry.
func validateEmoji(raw string) (string, error) {
	if raw == "" {
		return emojiPalette[0], nil
	}
	for _, e := range emojiPalette {
		if e == raw {
			return raw, nil
		}
	}
	return "", errEmojiUnsupported
}

// Stroke bounds. Point coordinates are normalized to [0,1] by the canvas
// model (see game.Point), but a fast drag can carry pointermove events
// slightly outside the element's bounding box before pointerup fires, so a
// margin is tolerated rather than rejecting an otherwise-legitimate
// stroke. strokePalette lists every color the frontend can currently
// produce (Canvas.jsx hardcodes "#111"); it exists to reject a forged
// value, not to offer a color picker.
const (
	maxStrokePoints = 2000
	minStrokeWidth  = 0.5
	maxStrokeWidth  = 20
	coordMargin     = 0.5
)

var strokePalette = map[string]bool{"#111": true}

var (
	errStrokeEmpty   = errors.New("stroke must have at least one point")
	errStrokeTooLong = errors.New("stroke has too many points")
	errStrokeCoord   = errors.New("stroke contains an invalid coordinate")
	errStrokeWidth   = errors.New("stroke width out of range")
	errStrokeColor   = errors.New("unsupported stroke color")
)

func validateStroke(p wsproto.StrokePayload) (wsproto.StrokePayload, error) {
	if len(p.Points) == 0 {
		return p, errStrokeEmpty
	}
	if len(p.Points) > maxStrokePoints {
		return p, errStrokeTooLong
	}
	for _, pt := range p.Points {
		if math.IsNaN(pt.X) || math.IsInf(pt.X, 0) || math.IsNaN(pt.Y) || math.IsInf(pt.Y, 0) {
			return p, errStrokeCoord
		}
		if pt.X < -coordMargin || pt.X > 1+coordMargin || pt.Y < -coordMargin || pt.Y > 1+coordMargin {
			return p, errStrokeCoord
		}
	}
	if p.Width < minStrokeWidth || p.Width > maxStrokeWidth {
		return p, errStrokeWidth
	}
	if !strokePalette[p.Color] {
		return p, errStrokeColor
	}
	return p, nil
}

// maxChatRunes bounds one chat message.
const maxChatRunes = 300

var (
	errChatEmpty   = errors.New("message must not be empty")
	errChatTooLong = errors.New("message is too long")
)

// validateChatText trims and bounds a chat message. React escapes it
// normally on render (it's just text content, never innerHTML) — no HTML
// sanitization happens or needs to happen here.
func validateChatText(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", errChatEmpty
	}
	if utf8.RuneCountInString(text) > maxChatRunes {
		return "", errChatTooLong
	}
	return text, nil
}

// maxGuessRunes bounds an impostor's word guess. Unlike chat, an empty
// guess is left to the engine to score as simply wrong rather than
// rejected outright — trimming and bounding length is all that's needed
// here.
const maxGuessRunes = 100

var errGuessTooLong = errors.New("guess is too long")

func validateGuess(raw string) (string, error) {
	guess := strings.TrimSpace(raw)
	if utf8.RuneCountInString(guess) > maxGuessRunes {
		return "", errGuessTooLong
	}
	return guess, nil
}

// maxVoiceSignalPayloadBytes bounds one signaling message's SDP/ICE
// payload — generous for a real offer/answer/candidate, still far short
// of anything that could meaningfully burden the room actor relaying it.
const maxVoiceSignalPayloadBytes = 8 * 1024

var (
	errVoiceSignalTarget = errors.New("signaling target must be another connected player")
	errVoiceSignalKind   = errors.New("unsupported signaling kind")
	errVoiceSignalSize   = errors.New("signaling payload too large")
)

func validateVoiceSignal(p wsproto.VoiceSignalIn, from game.PlayerID, connected map[game.PlayerID]bool) error {
	target := game.PlayerID(p.To)
	if target == "" || target == from || !connected[target] {
		return errVoiceSignalTarget
	}
	if p.Kind != "offer" && p.Kind != "answer" && p.Kind != "ice" {
		return errVoiceSignalKind
	}
	if len(p.Payload) > maxVoiceSignalPayloadBytes {
		return errVoiceSignalSize
	}
	return nil
}
