package room

import (
	"math"
	"strings"
	"testing"

	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

func TestValidatePlayerNameTrimsAndAccepts(t *testing.T) {
	name, err := ValidatePlayerName("  Alice  ")
	if err != nil {
		t.Fatalf("ValidatePlayerName: %v", err)
	}
	if name != "Alice" {
		t.Fatalf("name = %q, want Alice", name)
	}
}

func TestValidatePlayerNameRejectsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t\n"} {
		if _, err := ValidatePlayerName(in); err == nil {
			t.Fatalf("ValidatePlayerName(%q) = nil error, want rejection", in)
		}
	}
}

func TestValidatePlayerNameRejectsTooLong(t *testing.T) {
	// 25 runes, one over the limit.
	long := "1234567890123456789012345"
	if _, err := ValidatePlayerName(long); err == nil {
		t.Fatal("expected rejection of a 25-rune name")
	}
	// Exactly 24 runes must be accepted.
	ok := "123456789012345678901234"
	if _, err := ValidatePlayerName(ok); err != nil {
		t.Fatalf("expected a 24-rune name to be accepted: %v", err)
	}
}

func TestValidatePlayerNameRejectsControlCharacters(t *testing.T) {
	if _, err := ValidatePlayerName("Ali\x00ce"); err == nil {
		t.Fatal("expected rejection of a name containing a control character")
	}
	if _, err := ValidatePlayerName("Ali\nce"); err == nil {
		t.Fatal("expected rejection of a name containing a newline")
	}
}

func validStroke() wsproto.StrokePayload {
	return wsproto.StrokePayload{Points: []wsproto.Point{{X: 0.1, Y: 0.1}, {X: 0.2, Y: 0.2}}, Color: "#111", Width: 3}
}

func TestValidateStrokeAcceptsTheOnlySupportedShape(t *testing.T) {
	if _, err := validateStroke(validStroke()); err != nil {
		t.Fatalf("expected a well-formed stroke to be accepted: %v", err)
	}
}

func TestValidateStrokeRejectsEmptyPoints(t *testing.T) {
	s := validStroke()
	s.Points = nil
	if _, err := validateStroke(s); err == nil {
		t.Fatal("expected rejection of a stroke with no points")
	}
}

func TestValidateStrokeRejectsTooManyPoints(t *testing.T) {
	s := validStroke()
	s.Points = make([]wsproto.Point, maxStrokePoints+1)
	if _, err := validateStroke(s); err == nil {
		t.Fatal("expected rejection of a stroke over the point-count cap")
	}
}

func TestValidateStrokeRejectsNonFiniteCoordinates(t *testing.T) {
	for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		s := validStroke()
		s.Points = []wsproto.Point{{X: bad, Y: 0}}
		if _, err := validateStroke(s); err == nil {
			t.Fatalf("expected rejection of a non-finite coordinate %v", bad)
		}
	}
}

func TestValidateStrokeRejectsOutOfBoundsCoordinates(t *testing.T) {
	s := validStroke()
	s.Points = []wsproto.Point{{X: 50, Y: 0.1}}
	if _, err := validateStroke(s); err == nil {
		t.Fatal("expected rejection of a wildly out-of-bounds coordinate")
	}
}

func TestValidateStrokeRejectsWidthOutOfRange(t *testing.T) {
	for _, w := range []float64{0, -1, maxStrokeWidth + 1} {
		s := validStroke()
		s.Width = w
		if _, err := validateStroke(s); err == nil {
			t.Fatalf("expected rejection of width %v", w)
		}
	}
}

func TestValidateStrokeRejectsUnsupportedColor(t *testing.T) {
	s := validStroke()
	s.Color = "javascript:alert(1)"
	if _, err := validateStroke(s); err == nil {
		t.Fatal("expected rejection of a color outside the supported palette")
	}
}

func TestValidateChatTextTrimsRejectsEmptyAndBoundsLength(t *testing.T) {
	if text, err := validateChatText("  hi  "); err != nil || text != "hi" {
		t.Fatalf("validateChatText(%q) = %q, %v", "  hi  ", text, err)
	}
	if _, err := validateChatText("   "); err == nil {
		t.Fatal("expected rejection of a whitespace-only message")
	}
	if _, err := validateChatText(strings.Repeat("a", maxChatRunes+1)); err == nil {
		t.Fatal("expected rejection of a message over the rune cap")
	}
}

func TestValidateGuessTrimsAndBoundsLength(t *testing.T) {
	if g, err := validateGuess("  banana  "); err != nil || g != "banana" {
		t.Fatalf("validateGuess = %q, %v", g, err)
	}
	if _, err := validateGuess(strings.Repeat("a", maxGuessRunes+1)); err == nil {
		t.Fatal("expected rejection of a guess over the rune cap")
	}
}

func TestValidateEmojiAcceptsPaletteAndDefaultsEmpty(t *testing.T) {
	if e, err := validateEmoji(""); err != nil || e != emojiPalette[0] {
		t.Fatalf("validateEmoji(\"\") = %q, %v, want default %q", e, err, emojiPalette[0])
	}
	if e, err := validateEmoji(emojiPalette[2]); err != nil || e != emojiPalette[2] {
		t.Fatalf("validateEmoji(palette entry) = %q, %v", e, err)
	}
	if _, err := validateEmoji("💣"); err == nil {
		t.Fatal("expected rejection of an emoji outside the supported palette")
	}
}

func TestValidateVoiceSignalRejectsUnknownOrSelfOrDisconnectedTarget(t *testing.T) {
	connected := map[game.PlayerID]bool{"a": true, "b": true}
	if err := validateVoiceSignal(wsproto.VoiceSignalIn{To: "b", Kind: "offer"}, "a", connected); err != nil {
		t.Fatalf("expected a signal to another connected player to be accepted: %v", err)
	}
	if err := validateVoiceSignal(wsproto.VoiceSignalIn{To: "a", Kind: "offer"}, "a", connected); err == nil {
		t.Fatal("expected rejection of a signal targeting the sender itself")
	}
	if err := validateVoiceSignal(wsproto.VoiceSignalIn{To: "ghost", Kind: "offer"}, "a", connected); err == nil {
		t.Fatal("expected rejection of a signal targeting an unknown/disconnected player")
	}
}

func TestValidateVoiceSignalRejectsUnsupportedKind(t *testing.T) {
	connected := map[game.PlayerID]bool{"a": true, "b": true}
	if err := validateVoiceSignal(wsproto.VoiceSignalIn{To: "b", Kind: "shout"}, "a", connected); err == nil {
		t.Fatal("expected rejection of an unsupported signaling kind")
	}
}

func TestValidateVoiceSignalRejectsOversizedPayload(t *testing.T) {
	connected := map[game.PlayerID]bool{"a": true, "b": true}
	big := wsproto.VoiceSignalIn{To: "b", Kind: "ice", Payload: []byte(strings.Repeat("x", maxVoiceSignalPayloadBytes+1))}
	if err := validateVoiceSignal(big, "a", connected); err == nil {
		t.Fatal("expected rejection of an oversized signaling payload")
	}
}
