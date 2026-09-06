package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RishabJain30/fauxtist/internal/envconfig"
	"github.com/RishabJain30/fauxtist/internal/game"
	"github.com/RishabJain30/fauxtist/internal/hub"
	"github.com/RishabJain30/fauxtist/internal/room"
	"github.com/RishabJain30/fauxtist/internal/server"
)

// version, commit, and buildDate are set at build time via -ldflags (see
// Dockerfile); "dev"/"unknown" are the values a plain `go build` or
// `go run` produces locally.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

// shutdownTimeout bounds how long graceful shutdown waits for in-flight
// HTTP requests and WebSocket connections to finish before forcing them
// closed.
const shutdownTimeout = 10 * time.Second

func main() {
	os.Exit(run())
}

// run does the actual work and returns a process exit code, so main
// itself is just os.Exit(run()) — no defers are ever silently skipped by
// an early return, since there are none to skip.
func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := envconfig.Validate(); err != nil {
		logger.Error("invalid timing configuration", "error", err)
		return 1
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	origins, err := server.ResolveAllowedOrigins()
	if err != nil {
		logger.Error("invalid origin configuration", "error", err)
		return 1
	}

	var hubOpts []hub.Option
	// E2E/dev-only: shorten every timed gameplay phase to a fixed duration so
	// an end-to-end test can drive a whole match in seconds. Unset in
	// production; validated by envconfig.Validate above.
	if fast, _ := envconfig.PositiveDurationMS("FAUXTIST_FAST_PHASES_MS", 0); fast > 0 {
		logger.Warn("FAUXTIST_FAST_PHASES_MS set — gameplay phases shortened (E2E/dev only)", "ms", fast.Milliseconds())
		hubOpts = append(hubOpts, hub.WithRoomOptions(room.WithPhaseDuration(func(game.Phase) time.Duration { return fast })))
	}

	h := hub.New(hubOpts...)
	defer h.Close()

	srv := server.New(h, server.WithAllowedOrigins(origins), server.WithLogger(logger))
	httpServer := &http.Server{
		Addr:              ":" + port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		// IdleTimeout only bounds a connection sitting between HTTP
		// requests; it does not apply once a connection has been upgraded
		// to a WebSocket (net/http stops tracking idleness after the
		// Hijack an upgrade performs), so a long-lived game session is
		// never at risk of being cut by this.
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 16 << 10,
	}

	logger.Info("fauxtist starting", "port", port, "version", version, "commit", commit, "build_date", buildDate, "production", server.IsProduction())

	serveErr := make(chan error, 1)
	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serveErr:
		if err != nil {
			logger.Error("server failed to start", "error", err)
			return 1
		}
		return 0
	case <-sigCtx.Done():
		logger.Info("shutdown signal received")
	}

	srv.SetNotReady()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	// http.Server.Shutdown stops accepting new connections and waits for
	// in-flight plain HTTP requests (like POST /api/rooms) to finish, but
	// explicitly never touches already-hijacked connections — which is
	// exactly what every joined WebSocket is (see nhooyr's Accept). Those
	// are closed deliberately afterward, by closing every room.
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown did not complete cleanly", "error", err)
		return 1
	}

	h.Close()
	logger.Info("shutdown complete")
	return 0
}
