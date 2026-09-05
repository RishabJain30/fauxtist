package hub

import (
	"os"
	"strconv"
	"time"
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
func DefaultConfig() Config {
	return Config{
		EmptyRoomTTL:  envDurationMS("FAUXTIST_EMPTY_ROOM_TTL_MS", 15*time.Minute),
		SweepInterval: envDurationMS("FAUXTIST_ROOM_SWEEP_INTERVAL_MS", 1*time.Minute),
		MaxRooms:      envInt("FAUXTIST_MAX_ROOMS", 500),
	}
}

func envDurationMS(key string, def time.Duration) time.Duration {
	if ms := os.Getenv(key); ms != "" {
		if n, err := strconv.Atoi(ms); err == nil && n >= 0 {
			return time.Duration(n) * time.Millisecond
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
