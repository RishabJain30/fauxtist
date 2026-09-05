package envconfig

import (
	"strings"
	"testing"
	"time"
)

// setEnv sets key for the duration of the test only, via t.Setenv — never
// leaks across tests even on failure, and (per testing.T's own docs) fails
// the test outright if run in parallel, which nothing here does.
func setEnv(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)
}

func TestPositiveDurationMSReturnsDefaultWhenUnset(t *testing.T) {
	got, err := PositiveDurationMS("FAUXTIST_TEST_UNSET_MS", 7*time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 7*time.Second {
		t.Fatalf("got %v, want the default 7s", got)
	}
}

func TestPositiveDurationMSAcceptsAValidOverride(t *testing.T) {
	setEnv(t, "FAUXTIST_TEST_MS", "250")
	got, err := PositiveDurationMS("FAUXTIST_TEST_MS", time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 250*time.Millisecond {
		t.Fatalf("got %v, want 250ms", got)
	}
}

// --- Requirement: a zero sweep/heartbeat interval must never panic time.NewTicker ---

func TestPositiveDurationMSRejectsZero(t *testing.T) {
	setEnv(t, "FAUXTIST_TEST_MS", "0")
	_, err := PositiveDurationMS("FAUXTIST_TEST_MS", time.Second)
	if err == nil {
		t.Fatal("want an error for a zero override — it would reach time.NewTicker(0), which panics")
	}
}

// --- Requirement: a negative game duration must never advance a phase immediately ---

func TestPositiveDurationMSRejectsNegative(t *testing.T) {
	setEnv(t, "FAUXTIST_TEST_MS", "-500")
	_, err := PositiveDurationMS("FAUXTIST_TEST_MS", time.Second)
	if err == nil {
		t.Fatal("want an error for a negative override")
	}
}

func TestPositiveDurationMSRejectsNonInteger(t *testing.T) {
	setEnv(t, "FAUXTIST_TEST_MS", "soon")
	_, err := PositiveDurationMS("FAUXTIST_TEST_MS", time.Second)
	if err == nil {
		t.Fatal("want an error for a non-integer override")
	}
}

func TestPositiveDurationMSRejectsUnreasonablyLarge(t *testing.T) {
	setEnv(t, "FAUXTIST_TEST_MS", "999999999999")
	_, err := PositiveDurationMS("FAUXTIST_TEST_MS", time.Second)
	if err == nil {
		t.Fatal("want an error for a value far past any plausible tuning (likely a fat-fingered config)")
	}
}

func TestPositiveIntRejectsZeroAndNegative(t *testing.T) {
	for _, v := range []string{"0", "-1"} {
		setEnv(t, "FAUXTIST_TEST_INT", v)
		if _, err := PositiveInt("FAUXTIST_TEST_INT", 500); err == nil {
			t.Fatalf("value %q: want an error", v)
		}
	}
}

func TestPositiveIntAcceptsAValidOverride(t *testing.T) {
	setEnv(t, "FAUXTIST_TEST_INT", "10")
	got, err := PositiveInt("FAUXTIST_TEST_INT", 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 10 {
		t.Fatalf("got %d, want 10", got)
	}
}

// --- Validate: the centralized startup preflight ---

func TestValidatePassesWithNoOverridesSet(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("unexpected error with a clean environment: %v", err)
	}
}

func TestValidateCatchesTheExactSweepIntervalZeroCrashScenario(t *testing.T) {
	setEnv(t, "FAUXTIST_ROOM_SWEEP_INTERVAL_MS", "0")
	err := Validate()
	if err == nil {
		t.Fatal("want an error: FAUXTIST_ROOM_SWEEP_INTERVAL_MS=0 would otherwise reach time.NewTicker(0) and panic")
	}
	if !strings.Contains(err.Error(), "FAUXTIST_ROOM_SWEEP_INTERVAL_MS") {
		t.Fatalf("error %q does not name the offending variable", err.Error())
	}
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	setEnv(t, "FAUXTIST_ROOM_SWEEP_INTERVAL_MS", "0")
	setEnv(t, "FAUXTIST_HEARTBEAT_INTERVAL_MS", "-1")
	setEnv(t, "FAUXTIST_MAX_ROOMS", "not-a-number")
	err := Validate()
	if err == nil {
		t.Fatal("want an error")
	}
	for _, want := range []string{"FAUXTIST_ROOM_SWEEP_INTERVAL_MS", "FAUXTIST_HEARTBEAT_INTERVAL_MS", "FAUXTIST_MAX_ROOMS"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %s alongside the others", err.Error(), want)
		}
	}
}

func TestValidatePassesWithEveryVariableSetToAReasonableValue(t *testing.T) {
	for _, kv := range [][2]string{
		{"FAUXTIST_REVEAL_MS", "5000"},
		{"FAUXTIST_RECONNECT_GRACE_MS", "60000"},
		{"FAUXTIST_DISCONNECTED_TURN_MS", "10000"},
		{"FAUXTIST_IMPOSTOR_GUESS_MS", "30000"},
		{"FAUXTIST_EMPTY_ROOM_TTL_MS", "900000"},
		{"FAUXTIST_ROOM_SWEEP_INTERVAL_MS", "60000"},
		{"FAUXTIST_HEARTBEAT_INTERVAL_MS", "25000"},
		{"FAUXTIST_HEARTBEAT_TIMEOUT_MS", "10000"},
		{"FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS", "3600"},
		{"FAUXTIST_MAX_ROOMS", "500"},
	} {
		setEnv(t, kv[0], kv[1])
	}
	if err := Validate(); err != nil {
		t.Fatalf("unexpected error with every variable set to a documented-reasonable value: %v", err)
	}
}
