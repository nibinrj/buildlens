// Command buildlens-server is the BuildLens HTTP service.
//
// Usage:
//
//	buildlens-server              start the server (configuration from BUILDLENS_* environment variables)
//	buildlens-server healthcheck  GET /healthz on the local server; exit 0 if healthy (for Docker healthchecks)
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nibinrj/buildlens/internal/api"
	"github.com/nibinrj/buildlens/internal/config"
	"github.com/nibinrj/buildlens/internal/db"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	// ctx is cancelled on SIGTERM (docker stop) or Ctrl+C; everything below watches it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("server stopped with an error", "err", err)
		stop()
		os.Exit(1)
	}
}

// run wires the server together and blocks until ctx is cancelled or the server fails.
func run(ctx context.Context, getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()

	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, pool, logger); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.HTTPAddr, err)
	}

	srv := &http.Server{
		Handler:           api.NewRouter(pool, logger),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	logger.InfoContext(ctx, "server listening", "addr", ln.Addr().String())
	return serve(ctx, srv, ln, cfg.ShutdownTimeout, logger)
}

// serve runs srv on ln until ctx is cancelled, then shuts down gracefully: it stops accepting
// connections and waits up to timeout for in-flight requests to finish.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration, logger *slog.Logger) error {
	// Serve blocks, so it runs in its own goroutine and reports how it ended on errCh.
	// The channel is buffered so the goroutine can always send and exit, even if nobody receives.
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		// Serve only returns early on a real failure; it never returns nil.
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	logger.InfoContext(ctx, "shutting down", "timeout", timeout.String())
	// ctx is already cancelled; WithoutCancel keeps its values but gives the shutdown its own deadline.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-errCh; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	logger.InfoContext(ctx, "server stopped")
	return nil
}

// healthcheck is the container's liveness probe. The distroless image has no shell or curl,
// so the binary checks itself. It returns the process exit code.
func healthcheck() int {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := probe(ctx, http.DefaultClient, healthURL(os.Getenv("BUILDLENS_HTTP_ADDR"))); err != nil {
		fmt.Fprintln(os.Stderr, "unhealthy:", err)
		return 1
	}
	return 0
}

// healthURL turns a listen address such as ":8080" into the URL of the local /healthz.
func healthURL(listenAddr string) string {
	if listenAddr == "" {
		listenAddr = ":8080"
	}
	host, port, err := net.SplitHostPort(listenAddr)
	if err != nil {
		return "http://" + listenAddr + "/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}

// probe GETs url and fails unless the answer is 200 OK.
func probe(ctx context.Context, client *http.Client, url string) error {
	// The URL comes from this process's own listen address, never from a request: gosec's SSRF check does not apply.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil) //nolint:gosec // G704, see above
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := client.Do(req) //nolint:gosec // G704, see above
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only body; nothing to do if close fails

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get %s: status %d", url, resp.StatusCode)
	}
	return nil
}
