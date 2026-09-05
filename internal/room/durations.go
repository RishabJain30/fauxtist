package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
)

// Durations configures how long a room waits before treating something as
// consequential: how long a discussion runs, how long a round result holds
// on screen, and the presence-related grace periods added for reconnect
// handling. Tests should construct one explicitly (overriding only the
// fields they care about, typically to a few milliseconds) rather than
// mutating process environment variables, so timing stays local to the
// test instead of leaking into whatever else runs in the same process.
type Durations struct {
	// Discussion is how long the discussion phase runs before the host (or
	// the room, on their behalf) is prompted to end it.
	Discussion time.Duration
	// Reveal is how long a resolved round's result is shown before the room
	// advances to the next round or game over.
	Reveal time.Duration
	// Reconnect is how long a disconnected player's seat is preserved
	// before a lobby removal or host-migration eligibility check fires.
	Reconnect time.Duration
	// DisconnectedTurn is how long the room waits for a disconnected
	// current drawer to reconnect before skipping their turn.
	DisconnectedTurn time.Duration
	// ImpostorGuess is the deadline for a caught impostor to guess the
	// word before it's resolved as an incorrect guess.
	ImpostorGuess time.Duration
}

// DefaultDurations returns production defaults. Reveal, Reconnect,
// DisconnectedTurn, and ImpostorGuess can each be overridden by an
// environment variable (in whole milliseconds) for deployment-time tuning
// or fast integration tests that still want to go through the real clock:
//
//	FAUXTIST_REVEAL_MS
//	FAUXTIST_RECONNECT_GRACE_MS
//	FAUXTIST_DISCONNECTED_TURN_MS
//	FAUXTIST_IMPOSTOR_GUESS_MS
//
// Values are read via envconfig.PositiveDurationMS, which rejects a
// non-positive or unreasonably large override rather than silently
// applying it — main's startup validation (envconfig.Validate) already
// fails the process before this ever runs with a bad value, so the
// fallback to def here in that error case is defense in depth, not the
// primary guard.
func DefaultDurations() Durations {
	reveal, _ := envconfig.PositiveDurationMS("FAUXTIST_REVEAL_MS", 6*time.Second)
	reconnect, _ := envconfig.PositiveDurationMS("FAUXTIST_RECONNECT_GRACE_MS", 60*time.Second)
	disconnectedTurn, _ := envconfig.PositiveDurationMS("FAUXTIST_DISCONNECTED_TURN_MS", 10*time.Second)
	impostorGuess, _ := envconfig.PositiveDurationMS("FAUXTIST_IMPOSTOR_GUESS_MS", 30*time.Second)
	return Durations{
		Discussion:       45 * time.Second,
		Reveal:           reveal,
		Reconnect:        reconnect,
		DisconnectedTurn: disconnectedTurn,
		ImpostorGuess:    impostorGuess,
	}
}
