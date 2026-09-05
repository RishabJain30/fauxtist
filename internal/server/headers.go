package server

import "net/http"

// contentSecurityPolicy is scoped to exactly what this single-page app
// needs: its own scripts/styles (plus 'unsafe-inline' for the styles Vite
// inlines at build time), same-origin WebSocket connections (both ws:
// and wss: since local dev is plain HTTP), and no framing by another
// site. connect-src intentionally omits the STUN/TURN URLs configured via
// FAUXTIST_STUN_URLS/FAUXTIST_TURN_URLS — WebRTC's ICE traffic is UDP,
// not fetch/WebSocket, and CSP's connect-src does not govern it.
const contentSecurityPolicy = "" +
	"default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self' ws: wss:; " +
	"media-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'"

// securityHeaders applies baseline hardening headers to every response.
// HSTS is only ever sent over a connection the client already reached via
// HTTPS (checked per-request, not just at startup) — sending it over
// plain HTTP would be a lie the header itself can't back up, and would
// wrongly force HTTPS on a local http://localhost dev server.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Permissions-Policy", "microphone=(self), camera=(), geolocation=(), payment=()")
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		if isRequestHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// isRequestHTTPS reports whether the client's original request reached
// this process over HTTPS. Render (like most PaaS platforms) terminates
// TLS at a proxy in front of the app and forwards plain HTTP, setting
// X-Forwarded-Proto to say so.
func isRequestHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
