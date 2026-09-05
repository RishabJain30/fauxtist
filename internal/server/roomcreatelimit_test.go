package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RishabJain30/fauxtist/internal/hub"
)

// --- Requirement: POST /api/rooms cannot be used to exhaust FAUXTIST_MAX_ROOMS from one client ---

func TestCreateRoomIsRateLimitedPerIP(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	post := func() *http.Response {
		resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice"}`))
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		return resp
	}

	// httptest requests all arrive from the same loopback address, so
	// these share one per-IP bucket — exhaust its burst (see
	// newRoomCreateLimiter) with successful creations first. A
	// rate-limited request returning before ever calling s.hub.CreateRoom
	// (see createRoom) means this also proves no room is created once the
	// burst is exhausted — a 200 response is only possible via that call.
	limiter := newRoomCreateLimiter()
	for i := 0; i < limiter.perIPBurst; i++ {
		resp := post()
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 within the burst", i, resp.StatusCode)
		}
	}

	resp := post()
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 once the per-IP burst is exhausted", resp.StatusCode)
	}
	var body apiError
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Code != "rate_limited" {
		t.Fatalf("error code = %q, want rate_limited", body.Code)
	}
}
