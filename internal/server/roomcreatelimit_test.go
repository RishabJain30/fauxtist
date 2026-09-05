package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RishabJain30/fauxtist/internal/hub"
)

// --- Requirement: the per-IP limiter must trust exactly one proxy hop's
// X-Forwarded-For contribution (Render's edge), never a client-supplied
// leftmost entry, or a script could rotate through fake addresses and
// bypass the per-IP bucket entirely ---

func TestClientIPTrustsOnlyTheRightmostForwardedForEntry(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{
			name:       "no header at all falls back to RemoteAddr",
			xff:        "",
			remoteAddr: "203.0.113.9:54321",
			want:       "203.0.113.9",
		},
		{
			name:       "a single entry (the one real hop) is used directly",
			xff:        "203.0.113.5",
			remoteAddr: "10.0.0.1:12345", // Render's internal address, irrelevant once XFF is present
			want:       "203.0.113.5",
		},
		{
			name:       "an attacker-prepended fake entry is ignored in favor of the rightmost",
			xff:        "1.2.3.4, 203.0.113.5",
			remoteAddr: "10.0.0.1:12345",
			want:       "203.0.113.5",
		},
		{
			name:       "many attacker-prepended fake entries are still ignored",
			xff:        "9.9.9.9, 8.8.8.8, 1.2.3.4, 203.0.113.5",
			remoteAddr: "10.0.0.1:12345",
			want:       "203.0.113.5",
		},
		{
			name:       "extra whitespace around the trusted entry is trimmed",
			xff:        "1.2.3.4,   203.0.113.5  ",
			remoteAddr: "10.0.0.1:12345",
			want:       "203.0.113.5",
		},
		{
			name:       "a trailing comma leaving an empty rightmost entry falls back to RemoteAddr",
			xff:        "203.0.113.5,",
			remoteAddr: "10.0.0.1:12345",
			want:       "10.0.0.1",
		},
		{
			name:       "a RemoteAddr with no port is returned as-is",
			xff:        "",
			remoteAddr: "malformed-no-port",
			want:       "malformed-no-port",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/rooms", nil)
			req.RemoteAddr = tc.remoteAddr
			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}
			if got := clientIP(req); got != tc.want {
				t.Fatalf("clientIP() = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Requirement: spoofing X-Forwarded-For must not bypass the per-IP limiter ---

func TestCreateRoomRateLimitCannotBeBypassedBySpoofingForwardedFor(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	// Render's edge is the trusted hop that appends this real address;
	// everything to its left is exactly what an attacker fully controls.
	const realAddr = "203.0.113.5"

	postAs := func(fakeLeftmost string) *http.Response {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/rooms", strings.NewReader(`{"name":"Alice"}`))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", fakeLeftmost+", "+realAddr)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("post: %v", err)
		}
		return resp
	}

	limiter := newRoomCreateLimiter()
	for i := 0; i < limiter.perIPBurst; i++ {
		// A fresh, distinct fake leftmost address on every request — if
		// clientIP trusted it, each of these would land in its own empty
		// bucket and never trip the limit.
		resp := postAs(fmt.Sprintf("1.2.3.%d", i))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 within the burst", i, resp.StatusCode)
		}
	}

	resp := postAs("9.9.9.9") // yet another fresh fake leftmost address
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 — the real (rightmost) address's bucket must still be exhausted regardless of the spoofed leftmost entry", resp.StatusCode)
	}

	// A genuinely different real (rightmost) address gets its own,
	// unexhausted bucket — proving the limiter still distinguishes
	// legitimately different clients, not just refusing everyone.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/rooms", strings.NewReader(`{"name":"Bob"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.7")
	otherResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer otherResp.Body.Close()
	if otherResp.StatusCode != http.StatusOK {
		t.Fatalf("a different real address's status = %d, want 200 (its own bucket, unaffected by the exhausted one)", otherResp.StatusCode)
	}
}

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
