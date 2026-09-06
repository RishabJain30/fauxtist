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

// clientIP must trust exactly trustedProxyHops X-Forwarded-For entries from
// the right, never a client-supplied leftmost entry. trustedProxyHops is a
// package var defaulting to 0 off-Render; these XFF tests set it to 1 (Render's
// single trusted edge) and restore it.

func TestClientIPTrustsOnlyTheRightmostForwardedForEntry(t *testing.T) {
	prev := trustedProxyHops
	trustedProxyHops = 1
	defer func() { trustedProxyHops = prev }()

	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{"no header at all falls back to RemoteAddr", "", "203.0.113.9:54321", "203.0.113.9"},
		{"a single entry (the one real hop) is used directly", "203.0.113.5", "10.0.0.1:12345", "203.0.113.5"},
		{"an attacker-prepended fake entry is ignored in favor of the rightmost", "1.2.3.4, 203.0.113.5", "10.0.0.1:12345", "203.0.113.5"},
		{"many attacker-prepended fake entries are still ignored", "9.9.9.9, 8.8.8.8, 1.2.3.4, 203.0.113.5", "10.0.0.1:12345", "203.0.113.5"},
		{"extra whitespace around the trusted entry is trimmed", "1.2.3.4,   203.0.113.5  ", "10.0.0.1:12345", "203.0.113.5"},
		{"a trailing comma leaving an empty rightmost entry falls back to RemoteAddr", "203.0.113.5,", "10.0.0.1:12345", "10.0.0.1"},
		{"a RemoteAddr with no port is returned as-is", "", "malformed-no-port", "malformed-no-port"},
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

func TestCreateRoomRateLimitCannotBeBypassedBySpoofingForwardedFor(t *testing.T) {
	prev := trustedProxyHops
	trustedProxyHops = 1
	defer func() { trustedProxyHops = prev }()

	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

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
		// A fresh, distinct fake leftmost on every request — if clientIP
		// trusted it, each would land in its own empty bucket.
		resp := postAs(fmt.Sprintf("1.2.3.%d", i))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("request %d: status = %d, want 200 within the burst", i, resp.StatusCode)
		}
	}

	resp := postAs("9.9.9.9")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429 — the real (rightmost) address's bucket must be exhausted regardless of the spoofed leftmost", resp.StatusCode)
	}

	// A genuinely different real (rightmost) address gets its own, unexhausted
	// bucket.
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/rooms", strings.NewReader(`{"name":"Bob"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 198.51.100.7")
	otherResp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer otherResp.Body.Close()
	if otherResp.StatusCode != http.StatusOK {
		t.Fatalf("a different real address's status = %d, want 200 (its own bucket)", otherResp.StatusCode)
	}
}

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

	// httptest requests all arrive from the same loopback address, sharing one
	// per-IP bucket — exhaust its burst with successful creations first.
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
