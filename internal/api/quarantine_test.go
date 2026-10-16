package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nibinrj/buildlens/internal/quarantine"
)

type fakeQuarantine struct {
	err     error
	calls   int
	request quarantine.Request
	id      int64
	reason  string
}

func (f *fakeQuarantine) List(_ context.Context, repo string) ([]quarantine.Entry, error) {
	f.calls++
	return nil, f.err
}
func (f *fakeQuarantine) CreateManual(_ context.Context, r quarantine.Request) (int64, error) {
	f.calls++
	f.request = r
	return 7, f.err
}
func (f *fakeQuarantine) ReleaseManual(_ context.Context, id int64, reason string) error {
	f.calls++
	f.id = id
	f.reason = reason
	return f.err
}
func TestQuarantineEndpoints(t *testing.T) {
	create := `{"repo":"owner/repo","class_name":"lab.A","method_name":"a","reason":"investigate"}`
	for _, tt := range []struct {
		name, method, url, body string
		err                     error
		auth                    bool
		status, calls           int
	}{
		{"empty list", "GET", "/api/v1/quarantine?repo=owner/repo", "", nil, true, 200, 1},
		{"missing repo", "GET", "/api/v1/quarantine", "", nil, true, 400, 0},
		{"invalid repo", "GET", "/api/v1/quarantine?repo=x", "", nil, true, 400, 0},
		{"create", "POST", "/api/v1/quarantine", create, nil, true, 201, 1},
		{"reason required", "POST", "/api/v1/quarantine", `{"repo":"a/b","class_name":"A","method_name":"a"}`, nil, true, 400, 0},
		{"unknown field", "POST", "/api/v1/quarantine", `{"extra":1}`, nil, true, 400, 0},
		{"multiple JSON", "POST", "/api/v1/quarantine", create + create, nil, true, 400, 0},
		{"missing test", "POST", "/api/v1/quarantine", create, quarantine.ErrNotFound, true, 404, 1},
		{"active exists", "POST", "/api/v1/quarantine", create, quarantine.ErrConflict, true, 409, 1},
		{"cap full", "POST", "/api/v1/quarantine", create, quarantine.ErrCap, true, 409, 1},
		{"release", "POST", "/api/v1/quarantine/7/release", `{"reason":"fixed"}`, nil, true, 200, 1},
		{"invalid id", "POST", "/api/v1/quarantine/no/release", `{"reason":"fixed"}`, nil, true, 400, 0},
		{"release missing", "POST", "/api/v1/quarantine/7/release", `{"reason":"fixed"}`, quarantine.ErrNotFound, true, 404, 1},
		{"zero id", "POST", "/api/v1/quarantine/0/release", `{"reason":"fixed"}`, nil, true, 400, 0},
		{"release reason required", "POST", "/api/v1/quarantine/7/release", `{"reason":"  "}`, nil, true, 400, 0},
		{"malformed body", "POST", "/api/v1/quarantine", "{", nil, true, 400, 0},
		{"overlong reason", "POST", "/api/v1/quarantine/7/release", `{"reason":"` + strings.Repeat("a", 2001) + `"}`, nil, true, 400, 0},
		{"unauthenticated release", "POST", "/api/v1/quarantine/7/release", `{"reason":"fixed"}`, nil, false, 401, 0},
		{"unauthenticated read", "GET", "/api/v1/quarantine?repo=a/b", "", nil, false, 401, 0},
		{"unauthenticated write", "POST", "/api/v1/quarantine", create, nil, false, 401, 0},
		{"storage error", "GET", "/api/v1/quarantine?repo=a/b", "", errors.New("private DB detail"), true, 500, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := &fakeQuarantine{err: tt.err}
			router := NewRouter(Deps{Quarantine: f, IngestKey: testKey, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
			req := httptest.NewRequestWithContext(t.Context(), tt.method, tt.url, strings.NewReader(tt.body))
			if tt.auth {
				req.Header.Set("Authorization", "Bearer "+testKey)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.status || f.calls != tt.calls {
				t.Fatalf("status %d calls %d want %d %d: %s", rec.Code, f.calls, tt.status, tt.calls, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), "private DB") {
				t.Error("internal detail leaked")
			}
			if tt.name == "empty list" && rec.Body.String() != "[]\n" {
				t.Error("empty list must be JSON array")
			}
			if tt.name == "create" && f.request.Reason != "investigate" {
				t.Error("reason lost")
			}
			if tt.name == "release" && (f.id != 7 || f.reason != "fixed") {
				t.Error("release audit input lost")
			}
		})
	}
}
