// Package api holds the HTTP handlers of buildlens-server.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// Pinger is the one thing the readiness check needs from the database.
// *pgxpool.Pool satisfies it; tests pass a fake.
type Pinger interface {
	Ping(ctx context.Context) error
}

// readyTimeout bounds the database ping so a hung database makes /readyz fail fast instead of hanging.
const readyTimeout = 2 * time.Second

// Deps is everything the routes need. Interfaces let handler tests use fakes instead of a database.
type Deps struct {
	DB             Pinger
	Ingester       Ingester
	Builds         BuildReader
	IngestKey      string // required by every /api/v1 route
	MaxUploadBytes int64
	Logger         *slog.Logger
}

// NewRouter returns the server's routes.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	// Method patterns (Go 1.22+): other methods get 405 Method Not Allowed automatically.
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", handleReadyz(d.DB, d.Logger))

	auth := requireKey(d.IngestKey)
	mux.Handle("POST /api/v1/builds", auth(handleCreateBuild(d)))
	mux.Handle("GET /api/v1/builds/{id}", auth(handleGetBuild(d)))
	return mux
}

// handleHealthz answers liveness: the process is up and serving HTTP. It never touches the database,
// so a database outage does not get the container restarted.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleReadyz answers readiness: the server can do useful work, which needs the database.
func handleReadyz(db Pinger, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			logger.WarnContext(ctx, "readiness check failed", "err", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable", "database": "down"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "database": "up"})
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// The status line is already sent, so an encoding error cannot change the response; ignore it.
	_ = json.NewEncoder(w).Encode(body)
}
