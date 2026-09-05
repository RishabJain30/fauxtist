package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RishabJain30/fauxtist/internal/hub"
)

// --- Requirement #18: security headers are present without breaking required resources ---

func TestSecurityHeadersPresentOnEveryResponse(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "same-origin",
	} {
		if got := resp.Header.Get(header); got != want {
			t.Fatalf("%s = %q, want %q", header, got, want)
		}
	}
	if resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("expected a Content-Security-Policy header")
	}
	if resp.Header.Get("Permissions-Policy") == "" {
		t.Fatal("expected a Permissions-Policy header")
	}
}

func TestNoHSTSOverPlainHTTP(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Strict-Transport-Security") != "" {
		t.Fatal("must not send HSTS over a connection that did not arrive via HTTPS")
	}
}

func TestHSTSSentWhenForwardedAsHTTPS(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/healthz", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("expected HSTS once the request is known to have arrived via HTTPS")
	}
}

// --- Requirement #17: graceful shutdown changes readiness ---

func TestReadyzReflectsReadiness(t *testing.T) {
	h := hub.New()
	defer h.Close()
	s := New(h)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 while ready", resp.StatusCode)
	}

	s.SetNotReady()
	resp2, err := http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 once not ready", resp2.StatusCode)
	}
}
