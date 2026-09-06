// Package envconfig validates the process's own timing-related environment
// variables before anything that depends on them is constructed.
//
// Several packages (room, hub, server) each read their own duration/count
// env vars lazily, at the point something needs a default value — by
// design, so a caller only has to know sensible values for the one
// setting it wants to override (see hub.Config's doc comment). That
// laziness has a sharp edge: a malformed or non-positive value (e.g.
// FAUXTIST_ROOM_SWEEP_INTERVAL_MS=0) doesn't surface until whatever uses
// it first runs — often time.NewTicker, which panics outright on a
// non-positive duration, well after the process has already reported
// itself started. Validate re-checks every one of those same env vars, by
// name, against the same rule (unset is fine; set means a valid positive,
// reasonably bounded integer) so main can fail fast with one clear error
// instead of panicking later on whatever timer happens to fire first.
package envconfig

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// maxReasonableMS bounds every millisecond-denominated duration var: a
// generous 24 hours, far past any plausible tuning value, that exists
// only to catch an obvious fat-fingered config (e.g. six extra zeros)
// rather than to constrain legitimate deployments.
const maxReasonableMS = 24 * 60 * 60 * 1000

// maxReasonableSeconds is the same bound for the one second-denominated
// var (FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS).
const maxReasonableSeconds = 24 * 60 * 60

// msVars lists every environment variable read as a positive count of
// milliseconds across room.DefaultDurations, hub.DefaultConfig, and
// server.DefaultHeartbeatConfig. Kept as one list, here, so a new duration
// var only needs adding in one place to be covered by startup validation.
var msVars = []string{
	"FAUXTIST_RECONNECT_GRACE_MS",
	"FAUXTIST_EARLY_COUNTDOWN_MS",
	"FAUXTIST_SOLO_WAIT_MS",
	// E2E/dev-only: when set, every timed gameplay phase is shortened to this
	// many milliseconds (see cmd/fauxtist/main.go). Unset in production.
	"FAUXTIST_FAST_PHASES_MS",
	"FAUXTIST_EMPTY_ROOM_TTL_MS",
	"FAUXTIST_ROOM_SWEEP_INTERVAL_MS",
	"FAUXTIST_HEARTBEAT_INTERVAL_MS",
	"FAUXTIST_HEARTBEAT_TIMEOUT_MS",
}

// secondsVars lists every environment variable read as a positive count
// of seconds.
var secondsVars = []string{
	"FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS",
}

// intVars lists every environment variable read as a plain positive
// integer (not a duration).
var intVars = []string{
	"FAUXTIST_MAX_ROOMS",
}

// maxProxyHops bounds FAUXTIST_TRUSTED_PROXY_HOPS: nobody legitimately runs
// this process behind more than a handful of trusted reverse proxies.
const maxProxyHops = 10

// nonNegIntVars lists every environment variable read as a plain non-negative
// integer (zero is meaningful). FAUXTIST_TRUSTED_PROXY_HOPS defaults to 1 on
// Render (one trusted edge) and 0 elsewhere (trust only the direct peer).
var nonNegIntVars = []string{
	"FAUXTIST_TRUSTED_PROXY_HOPS",
}

// Validate checks every timing-related environment variable the process
// recognizes and returns a single error listing every problem found (not
// just the first), or nil if every set variable — unset ones are left for
// their package's own documented default — parses as a valid positive,
// reasonably bounded value. It constructs nothing and has no side effects
// beyond reading os.Getenv; callers run it once at startup, before
// building anything that would otherwise read the same variables lazily
// and panic or misbehave on a bad one.
func Validate() error {
	var problems []string
	for _, key := range msVars {
		if _, err := PositiveDurationMS(key, 0); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, key := range secondsVars {
		if _, err := positiveDuration(key, 0, time.Second, maxReasonableSeconds); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, key := range intVars {
		if _, err := PositiveInt(key, 0); err != nil {
			problems = append(problems, err.Error())
		}
	}
	for _, key := range nonNegIntVars {
		if _, err := NonNegativeInt(key, 0, maxProxyHops); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	msg := "invalid environment configuration:"
	for _, p := range problems {
		msg += "\n  - " + p
	}
	return errors.New(msg)
}

// PositiveDurationMS reads key as a whole, positive, reasonably bounded
// number of milliseconds, or returns def if key is unset. def is never
// itself validated — callers pass an already-known-good constant — so
// this can be called with def=0 purely to validate a var without needing
// its real default in hand (see Validate).
func PositiveDurationMS(key string, def time.Duration) (time.Duration, error) {
	return positiveDuration(key, def, time.Millisecond, maxReasonableMS)
}

// PositiveDurationSeconds reads key as a whole, positive, reasonably
// bounded number of seconds, or returns def if key is unset.
func PositiveDurationSeconds(key string, def time.Duration) (time.Duration, error) {
	return positiveDuration(key, def, time.Second, maxReasonableSeconds)
}

func positiveDuration(key string, def time.Duration, unit time.Duration, max int) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a valid integer", key, raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s=%d must be positive", key, n)
	}
	if n > max {
		return 0, fmt.Errorf("%s=%d exceeds the maximum reasonable value of %d", key, n, max)
	}
	return time.Duration(n) * unit, nil
}

// PositiveInt reads key as a plain positive integer, or returns def if key
// is unset.
func PositiveInt(key string, def int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a valid integer", key, raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%s=%d must be positive", key, n)
	}
	return n, nil
}

// NonNegativeInt reads key as a plain non-negative integer (zero allowed),
// bounded by max, or returns def if key is unset.
func NonNegativeInt(key string, def, max int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s=%q is not a valid integer", key, raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("%s=%d must not be negative", key, n)
	}
	if n > max {
		return 0, fmt.Errorf("%s=%d exceeds the maximum reasonable value of %d", key, n, max)
	}
	return n, nil
}
