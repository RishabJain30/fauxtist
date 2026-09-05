package room

import "testing"

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
