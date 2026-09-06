package server

import "testing"

// The WebSocket origin allowlist must default to local-dev origins with no
// configuration, accept multiple explicitly configured bare hosts, reject
// malformed or wildcard entries, and fail closed in production.

func clearOriginEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"FAUXTIST_ALLOWED_ORIGINS", "RENDER_EXTERNAL_HOSTNAME", "ALLOWED_ORIGIN", "FAUXTIST_ENV"} {
		t.Setenv(k, "")
	}
}

func TestResolveAllowedOriginsDefaultsToLocalDevWithNoConfig(t *testing.T) {
	clearOriginEnv(t)
	origins, err := ResolveAllowedOrigins()
	if err != nil {
		t.Fatalf("ResolveAllowedOrigins: %v", err)
	}
	for _, want := range localDevOrigins {
		if !contains(origins, want) {
			t.Fatalf("origins %v missing local-dev entry %q", origins, want)
		}
	}
}

func TestResolveAllowedOriginsAcceptsMultipleConfiguredOrigins(t *testing.T) {
	clearOriginEnv(t)
	t.Setenv("FAUXTIST_ALLOWED_ORIGINS", "example.com, sub.example.com ,another.example.com")
	origins, err := ResolveAllowedOrigins()
	if err != nil {
		t.Fatalf("ResolveAllowedOrigins: %v", err)
	}
	for _, want := range []string{"example.com", "sub.example.com", "another.example.com"} {
		if !contains(origins, want) {
			t.Fatalf("origins %v missing %q", origins, want)
		}
	}
}

func TestResolveAllowedOriginsRejectsMalformedEntry(t *testing.T) {
	clearOriginEnv(t)
	t.Setenv("FAUXTIST_ALLOWED_ORIGINS", "https://example.com")
	if _, err := ResolveAllowedOrigins(); err == nil {
		t.Fatal("expected rejection of a full URL instead of a bare host")
	}
}

func TestResolveAllowedOriginsRejectsWildcard(t *testing.T) {
	clearOriginEnv(t)
	t.Setenv("FAUXTIST_ALLOWED_ORIGINS", "*")
	if _, err := ResolveAllowedOrigins(); err == nil {
		t.Fatal("expected rejection of a wildcard origin")
	}
}

func TestResolveAllowedOriginsFailsClosedInProductionWithNoOrigin(t *testing.T) {
	clearOriginEnv(t)
	t.Setenv("FAUXTIST_ENV", "production")
	if _, err := ResolveAllowedOrigins(); err == nil {
		t.Fatal("expected production with no configured origin to fail closed")
	}
}

func TestResolveAllowedOriginsRenderHostnameSatisfiesProduction(t *testing.T) {
	clearOriginEnv(t)
	t.Setenv("RENDER_EXTERNAL_HOSTNAME", "myapp.onrender.com")
	origins, err := ResolveAllowedOrigins()
	if err != nil {
		t.Fatalf("ResolveAllowedOrigins: %v", err)
	}
	if !IsProduction() {
		t.Fatal("expected RENDER_EXTERNAL_HOSTNAME to be detected as production")
	}
	if !contains(origins, "myapp.onrender.com") || contains(origins, "localhost") {
		t.Fatalf("origins = %v, want exactly the render hostname, no local-dev entries in production", origins)
	}
}
