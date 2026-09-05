package server

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// localDevOrigins are always allowed in non-production so the Vite dev
// server (any localhost port) and a plain `go run` keep working with zero
// configuration.
var localDevOrigins = []string{"localhost", "127.0.0.1", "[::1]"}

// ResolveAllowedOrigins builds the WebSocket origin allowlist passed to
// websocket.Accept's OriginPatterns, from (in order) FAUXTIST_ALLOWED_ORIGINS
// (comma-separated), RENDER_EXTERNAL_HOSTNAME (set automatically by
// Render), and the legacy ALLOWED_ORIGIN. nhooyr's Accept always allows
// the request's own Host header regardless of this list — OriginPatterns
// only matters for genuinely cross-origin embeds — but IsProduction()
// still requires at least one explicitly configured entry: a bare
// wildcard fallback (this package's previous behavior) is exactly the
// cross-site WebSocket hijacking footgun nhooyr's own docs warn against,
// so production fails closed instead of silently defaulting to one.
func ResolveAllowedOrigins() ([]string, error) {
	var configured []string
	if raw := os.Getenv("FAUXTIST_ALLOWED_ORIGINS"); raw != "" {
		for _, o := range strings.Split(raw, ",") {
			o = strings.TrimSpace(o)
			if o == "" {
				continue
			}
			norm, err := normalizeOrigin(o)
			if err != nil {
				return nil, fmt.Errorf("invalid FAUXTIST_ALLOWED_ORIGINS entry %q: %w", o, err)
			}
			configured = append(configured, norm)
		}
	}
	if h := os.Getenv("RENDER_EXTERNAL_HOSTNAME"); h != "" {
		configured = append(configured, h)
	}
	if legacy := os.Getenv("ALLOWED_ORIGIN"); legacy != "" {
		norm, err := normalizeOrigin(legacy)
		if err != nil {
			return nil, fmt.Errorf("invalid ALLOWED_ORIGIN %q: %w", legacy, err)
		}
		configured = append(configured, norm)
	}

	if IsProduction() {
		if len(configured) == 0 {
			return nil, errors.New("no valid allowed origin configured in production (set FAUXTIST_ALLOWED_ORIGINS)")
		}
		return configured, nil
	}
	return append(configured, localDevOrigins...), nil
}

// IsProduction reports whether the process is running in a deployed
// environment, either because Render set RENDER_EXTERNAL_HOSTNAME
// automatically or because FAUXTIST_ENV=production was set explicitly for
// a non-Render deployment.
func IsProduction() bool {
	return os.Getenv("RENDER_EXTERNAL_HOSTNAME") != "" || strings.EqualFold(os.Getenv("FAUXTIST_ENV"), "production")
}

// normalizeOrigin validates a configured origin is a bare host[:port] —
// what OriginPatterns expects — rejecting a full URL (scheme/path) or a
// wildcard so a misconfigured value fails loudly at startup instead of
// silently matching nothing, or everything, at request time.
func normalizeOrigin(o string) (string, error) {
	if o == "*" {
		return "", errors.New("wildcard origins are not permitted")
	}
	if strings.Contains(o, "://") || strings.Contains(o, "/") {
		return "", errors.New("must be a bare host[:port], not a full URL")
	}
	return o, nil
}
