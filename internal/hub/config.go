package hub

import (
	"time"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
)

// Config controls room lifecycle limits. Zero values are never used
// directly — New always starts from DefaultConfig and lets options
// override individual fields — so a caller that only wants to change one
// setting doesn't have to know sensible values for the rest.
type Config struct {
	// EmptyRoomTTL is how long a room may sit with zero connected players
	// before the sweeper reaps it.
	EmptyRoomTTL time.Duration
	// SweepInterval is how often the background sweeper checks every
	// registered room for expiry eligibility.
	SweepInterval time.Duration
	// MaxRooms bounds how many rooms may be registered at once. A
	// CreateRoom call past this limit fails with ErrHubAtCapacity rather
	// than growing the hub unbounded.
	MaxRooms int
}

// DefaultConfig returns production defaults, each overridable by an
// environment variable (in whole milliseconds, or a plain count for
// MaxRooms) for deployment-time tuning:
//
//	FAUXTIST_EMPTY_ROOM_TTL_MS
//	FAUXTIST_ROOM_SWEEP_INTERVAL_MS
//	FAUXTIST_MAX_ROOMS
//
// Values are read via envconfig.PositiveDurationMS/PositiveInt, which
// reject a non-positive or unreasonably large override (a zero
// SweepInterval, in particular, would otherwise reach time.NewTicker and
// panic) rather than silently applying it — main's startup validation
// (envconfig.Validate) already fails the process before this ever runs
// with a bad value, so the fallback to def here in that error case is
// defense in depth, not the primary guard.
func DefaultConfig() Config {
	emptyRoomTTL, _ := envconfig.PositiveDurationMS("FAUXTIST_EMPTY_ROOM_TTL_MS", 15*time.Minute)
	sweepInterval, _ := envconfig.PositiveDurationMS("FAUXTIST_ROOM_SWEEP_INTERVAL_MS", 1*time.Minute)
	// A measured hobby-host default: one free Render instance holds a modest
	// number of in-memory rooms comfortably. Raise via FAUXTIST_MAX_ROOMS
	// only after load testing proves headroom.
	maxRooms, _ := envconfig.PositiveInt("FAUXTIST_MAX_ROOMS", 100)
	return Config{
		EmptyRoomTTL:  emptyRoomTTL,
		SweepInterval: sweepInterval,
		MaxRooms:      maxRooms,
	}
}
