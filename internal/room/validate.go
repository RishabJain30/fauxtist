package room

import (
	"errors"
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

// emojiPalette mirrors web/src/emoji.js's EMOJIS list exactly; the first entry
// is the client's default. Kept as the single source of truth server-side so a
// forged join can never set an avatar outside the set the UI offers.
var emojiPalette = []string{"🦊", "🐙", "🐸", "🦉", "🐨", "🦁", "🐵", "🦄", "🐼", "🐧", "🦔", "🐝"}

var errEmojiUnsupported = errors.New("unsupported emoji")

// validateEmoji accepts the empty string (mapped to the default emoji) or
// exactly one palette entry.
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

// ValidateEmoji is the exported form used by the room-creation HTTP handler,
// which validates the host's chosen avatar before minting their seat.
func ValidateEmoji(raw string) (string, error) { return validateEmoji(raw) }

// maxChatRunes bounds one chat message.
const maxChatRunes = 300

var (
	errChatEmpty   = errors.New("message must not be empty")
	errChatTooLong = errors.New("message is too long")
)

// validateChatText trims and bounds a chat message. React escapes it on render
// (it's text content, never innerHTML) — no HTML sanitization is needed here.
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

// maxVoiceSignalPayloadBytes bounds one signaling message's SDP/ICE payload —
// generous for a real offer/answer/candidate, far short of anything that could
// burden the room actor relaying it.
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
