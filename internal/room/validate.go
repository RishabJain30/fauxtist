package room

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
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
