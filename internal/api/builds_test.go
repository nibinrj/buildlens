package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/ingest"
	"github.com/nibinrj/buildlens/internal/upload"
)

const testKey = "test-ingest-key-0123456789"

// fakeIngester records what it was given and returns a canned answer.
type fakeIngester struct {
	calls int
	got   ingest.Upload
	res   ingest.Result
	err   error
}

func (f *fakeIngester) Ingest(_ context.Context, u ingest.Upload) (ingest.Result, error) {
	f.calls++
	f.got = u
	return f.res, f.err
}

type fakeReader struct {
	view ingest.BuildView
	err  error
}

func (f fakeReader) GetBuild(context.Context, int64) (ingest.BuildView, error) { return f.view, f.err }

type part struct {
	field, filename, content string
}

// multipartBody builds an upload the way the CLI does.
func multipartBody(t *testing.T, parts ...part) (io.Reader, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p.filename != "" {
			w, err = mw.CreateFormFile(p.field, p.filename)
		} else {
			w, err = mw.CreateFormField(p.field)
		}
		if err != nil {
			t.Fatalf("create part: %v", err)
		}
		if _, err := io.WriteString(w, p.content); err != nil {
			t.Fatalf("write part: %v", err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func fixture(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "surefire", rel))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func metadataJSON(t *testing.T, mutate func(*upload.Metadata)) string {
	t.Helper()
	m := upload.Metadata{
		Repo: "nibinrj/buildlens-lab", Job: "buildlens-lab/main", BuildNumber: 5, Branch: "main",
		CommitSHA: "0123456789abcdef0123456789abcdef01234567", Result: "UNSTABLE",
		StartedAt: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC),
		Stages:    []upload.Stage{{Name: "Test", StartedAt: time.Date(2026, 10, 4, 12, 0, 1, 0, time.UTC), DurationMs: 4200, Result: "UNSTABLE"}},
	}
	if mutate != nil {
		mutate(&m)
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestCreateBuild(t *testing.T) {
	rerun := fixture(t, "rerun/TEST-lab.core.CalculatorTest.xml")
	failsafe := fixture(t, "failsafe/TEST-lab.core.CalculatorIT.xml")
	malformed := fixture(t, "malformed/TEST-lab.core.Broken.xml")
	validMeta := metadataJSON(t, nil)
	reportPath := "core/target/surefire-reports/TEST-lab.core.CalculatorTest.xml"

	tests := []struct {
		name        string
		auth        string // full Authorization header value
		contentType string // overrides the multipart content type when set
		body        func(t *testing.T) (io.Reader, string)
		maxBytes    int64
		ingester    fakeIngester
		wantStatus  int
		wantError   string // substring of the JSON error message
		wantCalls   int
		check       func(t *testing.T, f *fakeIngester, body []byte)
	}{
		{
			name: "new build is created", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t,
					part{field: "metadata", content: validMeta},
					part{field: "report", filename: reportPath, content: rerun},
					part{field: "report", filename: "api/target/failsafe-reports/TEST-lab.core.CalculatorIT.xml", content: failsafe},
					part{field: "log", filename: "log-tail.txt", content: "line 1\nline 2\n"})
			},
			ingester:   fakeIngester{res: ingest.Result{BuildID: 42, Created: true, Tests: 8, Stages: 1}},
			wantStatus: http.StatusCreated, wantCalls: 1,
			check: func(t *testing.T, f *fakeIngester, body []byte) {
				if len(f.got.Tests) != 8 {
					t.Errorf("ingester got %d tests, want 8 (6 surefire + 2 failsafe)", len(f.got.Tests))
				}
				if f.got.Tests[0].Module != "core" || f.got.Tests[7].Module != "api" {
					t.Errorf("modules = %q, %q; want core and api from the uploaded paths", f.got.Tests[0].Module, f.got.Tests[7].Module)
				}
				if f.got.Meta.Job != "buildlens-lab/main" || len(f.got.Meta.Stages) != 1 {
					t.Errorf("metadata not passed through: %+v", f.got.Meta)
				}
				var res ingest.Result
				if err := json.Unmarshal(body, &res); err != nil || res.BuildID != 42 || !res.Created {
					t.Errorf("response = %s (err %v), want id 42 created", body, err)
				}
			},
		},
		{
			name: "same build again answers 200", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta})
			},
			ingester:   fakeIngester{res: ingest.Result{BuildID: 42, Created: false}},
			wantStatus: http.StatusOK, wantCalls: 1,
		},
		{
			name: "no Authorization header", auth: "",
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta})
			},
			wantStatus: http.StatusUnauthorized, wantError: "missing or invalid API key",
		},
		{
			name: "wrong key", auth: "Bearer not-the-key-0123456789",
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta})
			},
			wantStatus: http.StatusUnauthorized, wantError: "missing or invalid API key",
		},
		{
			name: "key without the Bearer scheme", auth: testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta})
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "JSON body instead of multipart", auth: "Bearer " + testKey, contentType: "application/json",
			body:       func(*testing.T) (io.Reader, string) { return strings.NewReader(validMeta), "" },
			wantStatus: http.StatusUnsupportedMediaType,
		},
		{
			name: "missing metadata part", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "report", filename: reportPath, content: rerun})
			},
			wantStatus: http.StatusBadRequest, wantError: "missing the metadata part",
		},
		{
			name: "metadata is not JSON", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: "{not json"})
			},
			wantStatus: http.StatusBadRequest, wantError: "metadata is not valid JSON",
		},
		{
			name: "metadata fails validation", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: metadataJSON(t, func(m *upload.Metadata) { m.Result = "GREEN"; m.BuildNumber = 0 })})
			},
			wantStatus: http.StatusBadRequest, wantError: "build_number must be positive",
		},
		{
			name: "malformed report rejects the whole upload", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t,
					part{field: "metadata", content: validMeta},
					part{field: "report", filename: reportPath, content: rerun},
					part{field: "report", filename: "core/target/surefire-reports/TEST-lab.core.Broken.xml", content: malformed})
			},
			wantStatus: http.StatusBadRequest, wantError: "TEST-lab.core.Broken.xml",
		},
		{
			name: "two metadata parts", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta}, part{field: "metadata", content: validMeta})
			},
			wantStatus: http.StatusBadRequest, wantError: "more than one metadata part",
		},
		{
			name: "unknown part", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta}, part{field: "junk", content: "x"})
			},
			wantStatus: http.StatusBadRequest, wantError: `unknown part "junk"`,
		},
		{
			name: "log tail over 1 MiB", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta},
					part{field: "log", filename: "log-tail.txt", content: strings.Repeat("x", upload.MaxLogBytes+1)})
			},
			wantStatus: http.StatusRequestEntityTooLarge, wantError: "log part is larger",
		},
		{
			name: "body over the upload limit", auth: "Bearer " + testKey, maxBytes: 4096,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta},
					part{field: "report", filename: reportPath, content: rerun}) // ~12 KB
			},
			wantStatus: http.StatusRequestEntityTooLarge, wantError: "exceeds the size limit",
		},
		{
			name: "storage failure is a 500 without internals", auth: "Bearer " + testKey,
			body: func(t *testing.T) (io.Reader, string) {
				return multipartBody(t, part{field: "metadata", content: validMeta})
			},
			ingester:   fakeIngester{err: errors.New("pq: connection reset by peer at 10.0.0.5")},
			wantStatus: http.StatusInternalServerError, wantError: "could not store the build", wantCalls: 1,
			check: func(t *testing.T, _ *fakeIngester, body []byte) {
				if strings.Contains(string(body), "10.0.0.5") {
					t.Errorf("response leaks the internal error: %s", body)
				}
			},
		},
	}

	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ing := tc.ingester
			maxBytes := tc.maxBytes
			if maxBytes == 0 {
				maxBytes = 64 << 20
			}
			router := NewRouter(Deps{Ingester: &ing, IngestKey: testKey, MaxUploadBytes: maxBytes, Logger: logger})

			body, ct := tc.body(t)
			if tc.contentType != "" {
				ct = tc.contentType
			}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/builds", body)
			req.Header.Set("Content-Type", ct)
			if tc.auth != "" {
				req.Header.Set("Authorization", tc.auth)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if ing.calls != tc.wantCalls {
				t.Errorf("ingester called %d times, want %d", ing.calls, tc.wantCalls)
			}
			if tc.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("401 without a WWW-Authenticate header")
			}
			if tc.wantError != "" {
				var e map[string]string
				if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil || !strings.Contains(e["error"], tc.wantError) {
					t.Errorf("error body = %s, want an error containing %q", rec.Body.String(), tc.wantError)
				}
			}
			if tc.check != nil {
				tc.check(t, &ing, rec.Body.Bytes())
			}
		})
	}
}

func TestGetBuild(t *testing.T) {
	view := ingest.BuildView{ID: 7, Job: "buildlens-lab/main", BuildNumber: 3, Result: "SUCCESS",
		Stages: []ingest.StageView{{Name: "Build", DurationMs: 1500, Result: "SUCCESS"}}, TestSummary: map[string]int{"PASSED": 4}}

	tests := []struct {
		name       string
		path       string
		auth       bool
		reader     fakeReader
		wantStatus int
	}{
		{"found", "/api/v1/builds/7", true, fakeReader{view: view}, http.StatusOK},
		{"unknown id", "/api/v1/builds/99", true, fakeReader{err: ingest.ErrNotFound}, http.StatusNotFound},
		{"id not a number", "/api/v1/builds/abc", true, fakeReader{}, http.StatusBadRequest},
		{"id zero", "/api/v1/builds/0", true, fakeReader{}, http.StatusBadRequest},
		{"no key", "/api/v1/builds/7", false, fakeReader{view: view}, http.StatusUnauthorized},
		{"storage failure", "/api/v1/builds/7", true, fakeReader{err: errors.New("db down")}, http.StatusInternalServerError},
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			router := NewRouter(Deps{Builds: tc.reader, IngestKey: testKey, Logger: logger})
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			if tc.auth {
				req.Header.Set("Authorization", "Bearer "+testKey)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				var got ingest.BuildView
				if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
					t.Fatalf("body is not a BuildView: %v", err)
				}
				if got.ID != 7 || len(got.Stages) != 1 || got.TestSummary["PASSED"] != 4 {
					t.Errorf("got %+v", got)
				}
			}
		})
	}
}

func TestEmptyKeyRejectsEverything(t *testing.T) {
	// Defence in depth: config refuses an empty key, but the middleware must not accept "Bearer " either.
	router := NewRouter(Deps{Builds: fakeReader{}, IngestKey: "", Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/builds/1", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
