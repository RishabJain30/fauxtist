package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"

	"github.com/RishabJain30/fauxtist/internal/hub"
)

// --- Requirement #8: oversized HTTP bodies are rejected ---

func TestCreateRoomRejectsOversizedBody(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	huge := `{"name":"` + strings.Repeat("a", maxCreateRoomBodyBytes*2) + `"}`
	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(huge))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("status = %d, want a 4xx rejection", resp.StatusCode)
	}
}

func TestCreateRoomRequiresJSONContentType(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/rooms", "text/plain", strings.NewReader(`{"name":"Alice"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	assertAPIError(t, resp, http.StatusUnsupportedMediaType, "invalid_content_type")
}

func TestCreateRoomRejectsMalformedJSON(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	assertAPIError(t, resp, http.StatusBadRequest, "invalid_body")
}

func TestCreateRoomRejectsTrailingData(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice"}{"extra":true}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	assertAPIError(t, resp, http.StatusBadRequest, "invalid_body")
}

func TestCreateRoomRejectsUnknownFields(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice","isAdmin":true}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	assertAPIError(t, resp, http.StatusBadRequest, "invalid_body")
}

func TestCreateRoomReturnsCapacityReachedWhenHubIsFull(t *testing.T) {
	h := hub.New(hub.WithConfig(hub.Config{EmptyRoomTTL: time.Hour, SweepInterval: time.Hour, MaxRooms: 1}))
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	first, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Alice"}`))
	if err != nil {
		t.Fatalf("post 1: %v", err)
	}
	first.Body.Close()
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first room status = %d, want 200", first.StatusCode)
	}

	second, err := http.Post(srv.URL+"/api/rooms", "application/json", strings.NewReader(`{"name":"Bob"}`))
	if err != nil {
		t.Fatalf("post 2: %v", err)
	}
	defer second.Body.Close()
	assertAPIError(t, second, http.StatusServiceUnavailable, "capacity_reached")
}

func assertAPIError(t *testing.T, resp *http.Response, wantStatus int, wantCode string) {
	t.Helper()
	if resp.StatusCode != wantStatus {
		t.Fatalf("status = %d, want %d", resp.StatusCode, wantStatus)
	}
	var body apiError
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if body.Code != wantCode {
		t.Fatalf("code = %q, want %q", body.Code, wantCode)
	}
}

// --- Requirement #9: oversized WebSocket frames are rejected ---

func TestOversizedWSFrameIsRejected(t *testing.T) {
	h := hub.New()
	defer h.Close()
	srv := httptest.NewServer(New(h).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	wsURL := wsURLFor(srv, cr.Code)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.CloseNow()

	huge := make([]byte, maxWSMessageBytes*2)
	if err := c.Write(ctx, websocket.MessageText, huge); err != nil {
		t.Fatalf("write: %v", err)
	}

	readCtx, readCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readCancel()
	if _, _, err := c.Read(readCtx); err == nil {
		t.Fatal("expected the oversized frame to close the connection")
	}
}
