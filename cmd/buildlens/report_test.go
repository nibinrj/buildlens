package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/upload"
)

const testKey = "test-ingest-key-0123456789"

// fakeServer answers with the given statuses in order (the last one repeats) and records each request.
type fakeServer struct {
	mu       sync.Mutex
	statuses []int
	requests []recorded
}

type recorded struct {
	auth     string
	parts    []string // "field:filename" in order
	metadata upload.Metadata
	log      string
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := recorded{auth: r.Header.Get("Authorization")}
	_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	mr := multipart.NewReader(r.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		_, dp, _ := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
		rec.parts = append(rec.parts, p.FormName()+":"+dp["filename"])
		b, _ := io.ReadAll(p)
		switch p.FormName() {
		case upload.PartMetadata:
			_ = json.Unmarshal(b, &rec.metadata)
		case upload.PartLog:
			rec.log = string(b)
		}
	}

	f.mu.Lock()
	f.requests = append(f.requests, rec)
	i := min(len(f.requests)-1, len(f.statuses)-1)
	status := f.statuses[i]
	f.mu.Unlock()

	w.WriteHeader(status)
	if status == http.StatusCreated {
		_, _ = io.WriteString(w, `{"id":1,"created":true,"tests":8,"stages":2}`)
	} else {
		_, _ = io.WriteString(w, `{"error":"canned answer"}`)
	}
}

// workspace creates a fake Jenkins workspace with reports, a stages file and a log tail.
func workspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	report, err := os.ReadFile(filepath.Join("..", "..", "testdata", "surefire", "rerun", "TEST-lab.core.CalculatorTest.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	files := map[string]string{
		"core/target/surefire-reports/TEST-lab.core.CalculatorTest.xml": string(report),
		"core/target/surefire-reports/lab.core.CalculatorTest.txt":      "plain summary, not a report",
		"target/failsafe-reports/TEST-lab.core.CalculatorIT.xml":        string(report),
		".git/target/surefire-reports/TEST-ignored.xml":                 "must not be uploaded",
		".buildlens/stages.json": `[{"name":"Build","startedAt":1791115200000,"durationMs":1200,"result":"SUCCESS"},` +
			`{"name":"Test","startedAt":1791115201200,"durationMs":3400,"result":"UNSTABLE"}]`,
		".buildlens/log-tail.txt": "[INFO] BUILD SUCCESS\n",
	}
	for rel, content := range files {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// jenkinsEnv is what a Jenkins agent provides, plus the BuildLens variables the agent container sets.
func jenkinsEnv(server string) map[string]string {
	return map[string]string{
		"BUILDLENS_SERVER_URL": server, "BUILDLENS_KEY": testKey, "BUILDLENS_AGENT_LIFECYCLE": "local",
		"JOB_NAME": "buildlens-lab/PR-7", "BUILD_NUMBER": "12", "BRANCH_NAME": "PR-7", "CHANGE_ID": "7", "NODE_NAME": "agent-1",
	}
}

func reportArgs(dir string, extra ...string) []string {
	return append([]string{
		"--dir", dir, "--repo", "nibinrj/buildlens-lab",
		"--commit", "0123456789abcdef0123456789abcdef01234567", "--tree", "89abcdef0123456789abcdef0123456789abcdef",
		"--result", "unstable", "--started-at", "1791115200000", "--duration-ms", "5000",
		"--stages", filepath.Join(dir, ".buildlens", "stages.json"),
		"--log-tail", filepath.Join(dir, ".buildlens", "log-tail.txt"),
	}, extra...)
}

// runReport runs "buildlens report" with a fake sleep and returns the exit code, output and recorded waits.
func runReport(t *testing.T, env map[string]string, args []string) (int, string, []time.Duration) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	var waits []time.Duration
	r := reporter{
		client: &http.Client{},
		sleep: func(_ context.Context, d time.Duration) error {
			waits = append(waits, d)
			return nil
		},
		stdout: &stdout, stderr: &stderr,
	}
	code := r.run(t.Context(), args, func(k string) string { return env[k] })
	return code, stdout.String() + stderr.String(), waits
}

func TestReportSuccess(t *testing.T) {
	fs := &fakeServer{statuses: []int{http.StatusCreated}}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	dir := workspace(t)

	code, out, waits := runReport(t, jenkinsEnv(srv.URL), reportArgs(dir))

	if code != exitOK || len(fs.requests) != 1 || len(waits) != 0 {
		t.Fatalf("exit %d, %d requests, %d waits; output:\n%s", code, len(fs.requests), len(waits), out)
	}
	if !strings.Contains(out, "uploaded buildlens-lab/PR-7 #12 with 2 report file(s)") {
		t.Errorf("output does not confirm the upload:\n%s", out)
	}

	req := fs.requests[0]
	if req.auth != "Bearer "+testKey {
		t.Errorf("Authorization = %q", req.auth)
	}
	wantParts := []string{
		"metadata:",
		"report:core/target/surefire-reports/TEST-lab.core.CalculatorTest.xml",
		"report:target/failsafe-reports/TEST-lab.core.CalculatorIT.xml",
		"log:log-tail.txt",
	}
	if !reflect.DeepEqual(req.parts, wantParts) {
		t.Errorf("parts = %v, want %v (metadata first, reports with paths, .git skipped, .txt ignored)", req.parts, wantParts)
	}
	if req.log != "[INFO] BUILD SUCCESS\n" {
		t.Errorf("log part = %q", req.log)
	}

	m := req.metadata
	if m.Job != "buildlens-lab/PR-7" || m.BuildNumber != 12 || m.Branch != "PR-7" || m.PRNumber == nil || *m.PRNumber != 7 {
		t.Errorf("Jenkins env not used: %+v", m)
	}
	if m.Result != "UNSTABLE" || m.Agent.Lifecycle != "LOCAL" || m.Agent.Name != "agent-1" {
		t.Errorf("result/agent = %q / %+v, want UNSTABLE and LOCAL agent-1", m.Result, m.Agent)
	}
	if m.DurationMs == nil || *m.DurationMs != 5000 || m.FinishedAt == nil || m.FinishedAt.Sub(m.StartedAt) != 5*time.Second {
		t.Errorf("duration/finish = %v / %v", m.DurationMs, m.FinishedAt)
	}
	if len(m.Stages) != 2 || m.Stages[1].Name != "Test" || m.Stages[1].Result != "UNSTABLE" ||
		!m.Stages[0].StartedAt.Equal(time.UnixMilli(1791115200000)) {
		t.Errorf("stages = %+v", m.Stages)
	}
}

func TestReportFailures(t *testing.T) {
	tests := []struct {
		name         string
		statuses     []int // nil: the server is down
		strict       bool
		wantCode     int
		wantRequests int
		wantWaits    int
		wantOutput   string
	}{
		{name: "server down warns and exits 0", statuses: nil, wantCode: exitOK, wantWaits: 3, wantOutput: "WARNING: build not uploaded"},
		{name: "server down with --strict exits 1", statuses: nil, strict: true, wantCode: exitFailed, wantWaits: 3, wantOutput: "gave up after 4 attempts"},
		{name: "401 is not retried and exits 0", statuses: []int{401}, wantCode: exitOK, wantRequests: 1, wantOutput: "server answered 401"},
		{name: "401 with --strict exits 1", statuses: []int{401}, strict: true, wantCode: exitFailed, wantRequests: 1, wantOutput: "server answered 401"},
		{name: "400 is not retried", statuses: []int{400}, wantCode: exitOK, wantRequests: 1, wantOutput: "server answered 400"},
		{name: "retry then success", statuses: []int{503, 502, 201}, wantCode: exitOK, wantRequests: 3, wantWaits: 2, wantOutput: "uploaded"},
		{name: "429 is retried", statuses: []int{429, 201}, wantCode: exitOK, wantRequests: 2, wantWaits: 1, wantOutput: "uploaded"},
		{name: "5xx until retries run out", statuses: []int{500}, wantCode: exitOK, wantRequests: 4, wantWaits: 3, wantOutput: "gave up after 4 attempts"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := &fakeServer{statuses: tc.statuses}
			srv := httptest.NewServer(fs)
			url := srv.URL
			if tc.statuses == nil {
				srv.Close() // connection refused
			} else {
				defer srv.Close()
			}
			args := reportArgs(workspace(t))
			if tc.strict {
				args = append(args, "--strict")
			}

			code, out, waits := runReport(t, jenkinsEnv(url), args)

			if code != tc.wantCode {
				t.Errorf("exit = %d, want %d; output:\n%s", code, tc.wantCode, out)
			}
			if len(fs.requests) != tc.wantRequests {
				t.Errorf("requests = %d, want %d", len(fs.requests), tc.wantRequests)
			}
			if len(waits) != tc.wantWaits {
				t.Errorf("waits = %v, want %d of them", waits, tc.wantWaits)
			}
			for i, w := range waits {
				base := time.Second << i
				if w < base || w > base+base/4 {
					t.Errorf("wait %d = %s, want between %s and %s", i, w, base, base+base/4)
				}
			}
			if !strings.Contains(out, tc.wantOutput) {
				t.Errorf("output does not contain %q:\n%s", tc.wantOutput, out)
			}
			if strings.Contains(out, testKey) {
				t.Error("the API key appears in the output")
			}
		})
	}
}

func TestReportUsageErrors(t *testing.T) {
	fs := &fakeServer{statuses: []int{http.StatusCreated}}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	dir := workspace(t)

	tests := []struct {
		name    string
		env     func(map[string]string)
		args    []string
		wantOut string
	}{
		{"no server", func(e map[string]string) { delete(e, "BUILDLENS_SERVER_URL") }, nil, "--server (or BUILDLENS_SERVER_URL) is required"},
		{"server without scheme", func(e map[string]string) { e["BUILDLENS_SERVER_URL"] = "buildlens-server:8080" }, nil, "must be an http(s) URL"},
		{"no key", func(e map[string]string) { delete(e, "BUILDLENS_KEY") }, nil, "--key (or BUILDLENS_KEY) is required"},
		{"build number not a number", func(e map[string]string) { e["BUILD_NUMBER"] = "twelve" }, nil, "--build-number"},
		{"unknown result", nil, []string{"--result", "green"}, "result must be one of"},
		{"missing start time", nil, []string{"--started-at", ""}, "--started-at"},
		{"stages file missing", nil, []string{"--stages", filepath.Join(dir, "nope.json")}, "read --stages"},
		{"stray argument", nil, []string{"extra"}, "unexpected arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			env := jenkinsEnv(srv.URL)
			if tc.env != nil {
				tc.env(env)
			}
			code, out, _ := runReport(t, env, reportArgs(dir, tc.args...))
			if code != exitUsage {
				t.Errorf("exit = %d, want %d (usage); output:\n%s", code, exitUsage, out)
			}
			if !strings.Contains(out, tc.wantOut) {
				t.Errorf("output does not contain %q:\n%s", tc.wantOut, out)
			}
		})
	}
	if len(fs.requests) != 0 {
		t.Errorf("usage errors sent %d requests, want 0", len(fs.requests))
	}
}

func TestReportWithoutReportsStillUploads(t *testing.T) {
	fs := &fakeServer{statuses: []int{http.StatusCreated}}
	srv := httptest.NewServer(fs)
	defer srv.Close()
	dir := workspace(t)

	code, out, _ := runReport(t, jenkinsEnv(srv.URL), reportArgs(dir, "--reports", "**/no-such-dir/*.xml"))
	if code != exitOK || len(fs.requests) != 1 {
		t.Fatalf("exit %d, %d requests; output:\n%s", code, len(fs.requests), out)
	}
	if !strings.Contains(out, "no test reports matched") {
		t.Errorf("no warning about missing reports:\n%s", out)
	}
}

func TestGlobToRegexp(t *testing.T) {
	tests := []struct {
		glob  string
		path  string
		match bool
	}{
		{"**/target/surefire-reports/TEST-*.xml", "target/surefire-reports/TEST-a.xml", true},
		{"**/target/surefire-reports/TEST-*.xml", "core/target/surefire-reports/TEST-a.xml", true},
		{"**/target/surefire-reports/TEST-*.xml", "a/b/c/target/surefire-reports/TEST-a.xml", true},
		{"**/target/surefire-reports/TEST-*.xml", "core/target/surefire-reports/sub/TEST-a.xml", false},
		{"**/target/surefire-reports/TEST-*.xml", "core/target/surefire-reports/a.xml", false},
		{"**/target/surefire-reports/TEST-*.xml", "core/target/surefire-reports/TEST-a.txt", false},
		{"target/*.xml", "target/a.xml", true},
		{"target/*.xml", "core/target/a.xml", false},
		{"TEST-?.xml", "TEST-a.xml", true},
		{"TEST-?.xml", "TEST-ab.xml", false},
		{"reports/**", "reports/x/y.xml", true},
		{"a.b", "axb", false}, // dots are literal
	}
	for _, tc := range tests {
		t.Run(tc.glob+" "+tc.path, func(t *testing.T) {
			re, err := globToRegexp(tc.glob)
			if err != nil {
				t.Fatalf("globToRegexp: %v", err)
			}
			if got := re.MatchString(tc.path); got != tc.match {
				t.Errorf("match = %v, want %v (regexp %s)", got, tc.match, re)
			}
		})
	}
}

func TestRunCommands(t *testing.T) {
	tests := []struct {
		args     []string
		wantCode int
		wantOut  string
	}{
		{nil, exitUsage, "usage: buildlens"},
		{[]string{"version"}, exitOK, "buildlens dev"},
		{[]string{"help"}, exitOK, "commands:"},
		{[]string{"quarantine", "--repo", "x"}, exitOK, "not implemented yet"},
		{[]string{"deploy"}, exitUsage, `unknown command "deploy"`},
		{[]string{"report", "-h"}, exitOK, "-server"},
	}
	for _, tc := range tests {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			var out bytes.Buffer
			code := run(t.Context(), tc.args, &out, &out, func(string) string { return "" })
			if code != tc.wantCode || !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("exit %d (want %d), output:\n%s\nwant it to contain %q", code, tc.wantCode, out.String(), tc.wantOut)
			}
		})
	}
}

func TestSleepCtxStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	start := time.Now()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepCtx = %v, want context.Canceled", err)
	}
	if time.Since(start) > time.Second {
		t.Error("sleepCtx did not return promptly")
	}
}
