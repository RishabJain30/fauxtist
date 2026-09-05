package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
	"github.com/RishabJain30/fauxtist/internal/wsproto"
)

// TURNConfig controls the WebRTC ICE configuration handed to a client on
// request (see handleIceConfigRequest). Voice remains best-effort without
// it: StunURLs alone still lets peers on favorable networks connect, TURN
// only helps peers behind restrictive NATs relay through a separate
// Coturn deployment (see docs/turn.md) — this process never runs a TURN
// server itself.
type TURNConfig struct {
	StunURLs      []string
	TurnURLs      []string
	SharedSecret  string
	CredentialTTL time.Duration
}

// DefaultTURNConfig reads its settings from environment variables:
//
//	FAUXTIST_STUN_URLS                  comma-separated, default a public Google STUN server
//	FAUXTIST_TURN_URLS                  comma-separated, default empty (no TURN)
//	FAUXTIST_TURN_SHARED_SECRET         Coturn's static-auth-secret; default empty (no TURN)
//	FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS default 3600
//
// TURN entries are only ever included if both a shared secret and at
// least one TURN URL are configured.
func DefaultTURNConfig() TURNConfig {
	stun := []string{"stun:stun.l.google.com:19302"}
	if raw := os.Getenv("FAUXTIST_STUN_URLS"); raw != "" {
		stun = splitCommaList(raw)
	}
	ttl, _ := envconfig.PositiveDurationSeconds("FAUXTIST_TURN_CREDENTIAL_TTL_SECONDS", 3600*time.Second)
	return TURNConfig{
		StunURLs:      stun,
		TurnURLs:      splitCommaList(os.Getenv("FAUXTIST_TURN_URLS")),
		SharedSecret:  os.Getenv("FAUXTIST_TURN_SHARED_SECRET"),
		CredentialTTL: ttl,
	}
}

func splitCommaList(raw string) []string {
	var out []string
	for _, v := range strings.Split(raw, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// buildIceServers returns the current best-effort ICE server list: STUN
// always, plus a fresh time-limited TURN credential (Coturn's REST API
// convention: username is the credential's Unix expiry timestamp,
// credential is base64(HMAC-SHA1(sharedSecret, username))) for every
// configured TURN URL, if TURN is configured at all. Pure given now, so
// it's testable without waiting out a real TTL.
func buildIceServers(cfg TURNConfig, now time.Time) []wsproto.IceServer {
	servers := make([]wsproto.IceServer, 0, 1+len(cfg.TurnURLs))
	if len(cfg.StunURLs) > 0 {
		servers = append(servers, wsproto.IceServer{URLs: cfg.StunURLs})
	}
	if cfg.SharedSecret == "" || len(cfg.TurnURLs) == 0 {
		return servers
	}
	username := strconv.FormatInt(now.Add(cfg.CredentialTTL).Unix(), 10)
	mac := hmac.New(sha1.New, []byte(cfg.SharedSecret))
	mac.Write([]byte(username))
	credential := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	for _, url := range cfg.TurnURLs {
		servers = append(servers, wsproto.IceServer{URLs: []string{url}, Username: username, Credential: credential})
	}
	return servers
}
