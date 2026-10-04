package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// fakePinger returns err, or blocks until the context ends when block is true.
type fakePinger struct {
	err   error
	block bool
}

func (f fakePinger) Ping(ctx context.Context) error {
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

func TestRouter(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		path       string
		db         fakePinger
		wantStatus int
		wantBody   map[string]string // nil: body not checked
	}{
		{
			name: "healthz ok", method: http.MethodGet, path: "/healthz",
			wantStatus: http.StatusOK, wantBody: map[string]string{"status": "ok"},
		},
		{
			name: "healthz ignores a database outage", method: http.MethodGet, path: "/healthz",
			db:         fakePinger{err: errors.New("connection refused")},
			wantStatus: http.StatusOK, wantBody: map[string]string{"status": "ok"},
		},
		{
			name: "readyz ok when the database answers", method: http.MethodGet, path: "/readyz",
			wantStatus: http.StatusOK, wantBody: map[string]string{"status": "ok", "database": "up"},
		},
		{
			name: "readyz 503 when the database fails", method: http.MethodGet, path: "/readyz",
			db:         fakePinger{err: errors.New("connection refused")},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   map[string]string{"status": "unavailable", "database": "down"},
		},
		{
			name: "healthz rejects POST", method: http.MethodPost, path: "/healthz",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "unknown path", method: http.MethodGet, path: "/nope",
			wantStatus: http.StatusNotFound,
		},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(Deps{DB: tc.db, Logger: logger, IngestKey: testKey})
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantBody == nil {
				return
			}
			if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var got map[string]string
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("body is not JSON: %v (%q)", err, rec.Body.String())
			}
			if len(got) != len(tc.wantBody) {
				t.Fatalf("body = %v, want %v", got, tc.wantBody)
			}
			for k, v := range tc.wantBody {
				if got[k] != v {
					t.Errorf("body[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

// TestReadyzTimesOut proves a hung database makes /readyz fail after readyTimeout instead of hanging.
func TestReadyzTimesOut(t *testing.T) {
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	router := NewRouter(Deps{DB: fakePinger{block: true}, Logger: logger, IngestKey: testKey})

	start := time.Now()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil))
	elapsed := time.Since(start)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if elapsed < readyTimeout || elapsed > readyTimeout+2*time.Second {
		t.Errorf("readyz took %s, want about %s", elapsed, readyTimeout)
	}
}
