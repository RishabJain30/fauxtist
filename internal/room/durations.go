package room

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
)

// Durations configures the room's infrastructure timings — the ones that are
// NOT per-phase gameplay deadlines (those come from the selected preset, see
// game.PresetConfigFor). Tests construct one explicitly with tiny values
// rather than mutating process environment variables.
type Durations struct {
	// Reconnect is how long a disconnected seat is preserved before a lobby
	// removal or host-migration eligibility check fires.
	Reconnect time.Duration
	// EarlyCountdown is the visible "all players locked" countdown that runs
	// once every required player has completed a phase early, after which the
	// phase advances and submissions are irreversible.
	EarlyCountdown time.Duration
	// SoloWait is how long the room waits, offering a choice, when only one
	// active player remains connected.
	SoloWait time.Duration
	// ResumeMinRemaining floors the remaining time restored to a phase that
	// was paused because everyone disconnected, so a reconnecting player is
	// never handed a phase that expires the instant they arrive.
	ResumeMinRemaining time.Duration
}

// DefaultDurations returns production defaults. Reconnect, the early-lock
// countdown, and the solo-wait window can each be overridden (in whole
// milliseconds) for deployment tuning or fast integration tests.
func DefaultDurations() Durations {
	reconnect, _ := envconfig.PositiveDurationMS("FAUXTIST_RECONNECT_GRACE_MS", 60*time.Second)
	early, _ := envconfig.PositiveDurationMS("FAUXTIST_EARLY_COUNTDOWN_MS", 3*time.Second)
	solo, _ := envconfig.PositiveDurationMS("FAUXTIST_SOLO_WAIT_MS", 60*time.Second)
	return Durations{
		Reconnect:          reconnect,
		EarlyCountdown:     early,
		SoloWait:           solo,
		ResumeMinRemaining: 3 * time.Second,
	}
}
