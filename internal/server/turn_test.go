package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// --- End-to-end: ice_config_request over an established session ---

func TestIceConfigRequestReturnsConfiguredServersOverTheSession(t *testing.T) {
	h := hub.New()
	defer h.Close()
	turnCfg := TURNConfig{
		StunURLs:      []string{"stun:stun.example.com:3478"},
		TurnURLs:      []string{"turn:turn.example.com:3478"},
		SharedSecret:  "s3cret",
		CredentialTTL: time.Hour,
	}
	srv := httptest.NewServer(New(h, WithTURNConfig(turnCfg)).Handler())
	defer srv.Close()

	cr := createTestRoom(t, srv, "Host")
	c := dialJoin(t, wsURLFor(srv, cr.Code), wsproto.JoinPayload{PlayerID: cr.PlayerID, ReconnectToken: cr.ReconnectToken})
	defer c.CloseNow()
	readUntil(t, c, wsproto.TypeStateSnapshot)

	writeMsg(t, c, wsproto.TypeIceConfigRequest, map[string]any{})
	env := readUntil(t, c, wsproto.TypeIceConfig)
	var p wsproto.IceConfigPayload
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		t.Fatalf("unmarshal ice_config payload: %v", err)
	}
	if len(p.IceServers) != 2 {
		t.Fatalf("iceServers = %+v, want one STUN + one TURN entry", p.IceServers)
	}
}

// --- Requirement #21: STUN fallback works when TURN is not configured ---

func TestBuildIceServersStunOnlyWithoutTurnConfig(t *testing.T) {
	cfg := TURNConfig{StunURLs: []string{"stun:stun.example.com:3478"}}
	servers := buildIceServers(cfg, time.Unix(0, 0))
	if len(servers) != 1 {
		t.Fatalf("servers = %+v, want exactly one STUN entry", servers)
	}
	if servers[0].Username != "" || servers[0].Credential != "" {
		t.Fatalf("a STUN-only entry must carry no credentials: %+v", servers[0])
	}
}

// --- Requirement #19: TURN credentials are time-limited and correctly HMAC-generated ---

func TestBuildIceServersGeneratesCorrectTimeLimitedHMACCredential(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	ttl := 3600 * time.Second
	cfg := TURNConfig{
		StunURLs:      []string{"stun:stun.example.com:3478"},
		TurnURLs:      []string{"turn:turn.example.com:3478"},
		SharedSecret:  "s3cret",
		CredentialTTL: ttl,
	}
	servers := buildIceServers(cfg, now)
	if len(servers) != 2 {
		t.Fatalf("servers = %+v, want one STUN + one TURN entry", servers)
	}
	turnEntry := servers[1]

	wantUsername := strconv.FormatInt(now.Add(ttl).Unix(), 10)
	if turnEntry.Username != wantUsername {
		t.Fatalf("username = %q, want expiry timestamp %q", turnEntry.Username, wantUsername)
	}

	mac := hmac.New(sha1.New, []byte(cfg.SharedSecret))
	mac.Write([]byte(wantUsername))
	wantCredential := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	if turnEntry.Credential != wantCredential {
		t.Fatalf("credential = %q, want %q", turnEntry.Credential, wantCredential)
	}
}

func TestBuildIceServersOneEntryPerConfiguredTurnURL(t *testing.T) {
	cfg := TURNConfig{
		TurnURLs:      []string{"turn:a.example.com:3478", "turn:b.example.com:3478"},
		SharedSecret:  "s3cret",
		CredentialTTL: time.Hour,
	}
	servers := buildIceServers(cfg, time.Unix(0, 0))
	if len(servers) != 2 {
		t.Fatalf("servers = %+v, want one entry per TURN URL (no STUN configured)", servers)
	}
}

// --- Requirement #20: the shared secret itself never appears in a response ---

func TestBuildIceServersNeverExposesTheSharedSecretItself(t *testing.T) {
	cfg := TURNConfig{
		TurnURLs:      []string{"turn:turn.example.com:3478"},
		SharedSecret:  "super-secret-value",
		CredentialTTL: time.Hour,
	}
	for _, s := range buildIceServers(cfg, time.Now()) {
		if strings.Contains(s.Username, cfg.SharedSecret) || strings.Contains(s.Credential, cfg.SharedSecret) {
			t.Fatalf("shared secret leaked into an ICE server entry: %+v", s)
		}
	}
}

func TestBuildIceServersDifferentCallsProduceDifferentCredentialsOverTime(t *testing.T) {
	cfg := TURNConfig{TurnURLs: []string{"turn:turn.example.com:3478"}, SharedSecret: "s3cret", CredentialTTL: time.Hour}
	a := buildIceServers(cfg, time.Unix(1000, 0))
	b := buildIceServers(cfg, time.Unix(2000, 0))
	if a[0].Credential == b[0].Credential {
		t.Fatal("credentials generated at different times must differ (time-limited)")
	}
}
