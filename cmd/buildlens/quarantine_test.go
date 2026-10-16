package main

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQuarantineFiles(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		down       bool
		want, warn string
	}{
		{"list", `[{"class_name":"lab.BTest","method_name":"b"},{"class_name":"lab.ATest","method_name":"a"}]`, 200, false, "lab.ATest#a\nlab.BTest#b\n", ""},
		{"empty", "[]", 200, false, "", ""},
		{"unreachable", "", 0, true, "", "WARNING"},
		{"unauthorized", `{"error":"no"}`, 401, false, "", "WARNING"},
		{"server error", "", 503, false, "", "WARNING"},
		{"invalid JSON", "{", 200, false, "", "WARNING"},
		{"oversized response", strings.Repeat(" ", (1<<20)+1), 200, false, "", "exceeds"},
		{"trailing JSON", "[][]", 200, false, "", "WARNING"},
		{"malicious wildcard", `[{"class_name":"*","method_name":"*"}]`, 200, false, "", "unsupported"},
		{"newline injection", `[{"class_name":"lab.A","method_name":"a\n*"}]`, 200, false, "", "unsupported"},
		{"parameterized display", `[{"class_name":"lab.A","method_name":"value(int)[1]"}]`, 200, false, "", "unsupported"},
		{"duplicates", `[{"class_name":"lab.A","method_name":"a"},{"class_name":"lab.A","method_name":"a"}]`, 200, false, "lab.A#a\n", ""},
	}
	for _, format := range []string{"surefire-excludes", "surefire-only"} {
		for _, tt := range cases {
			t.Run(format+"/"+tt.name, func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Query().Get("repo") != "owner/repo" || r.Header.Get("Authorization") != "Bearer test-key" {
						t.Errorf("wrong request: %s", r.URL)
					}
					w.WriteHeader(tt.status)
					_, _ = w.Write([]byte(tt.body))
				}))
				defer s.Close()
				if tt.down {
					s.Close()
				}
				out := filepath.Join(t.TempDir(), "nested", "list")
				if err := writePatterns(out, []byte("stale#exclusion\n")); err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				q := quarantineFetcher{s.Client(), &stdout, &stderr}
				code := q.run(t.Context(), []string{"--server", s.URL, "--key", "test-key", "--repo", "owner/repo", "--format", format, "--output", out}, func(string) string { return "" })
				got, err := os.ReadFile(out)
				if err != nil {
					t.Fatal(err)
				}
				if code != 0 || string(got) != tt.want {
					t.Fatalf("code %d file %q want %q; %s", code, got, tt.want, stderr.String())
				}
				if tt.warn != "" && !strings.Contains(stderr.String(), tt.warn) {
					t.Errorf("warning=%s", stderr.String())
				}
				if strings.Contains(stderr.String(), "test-key") {
					t.Error("key leaked")
				}
			})
		}
	}
}
func TestQuarantineWriteFailure(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	q := quarantineFetcher{&http.Client{}, &out, &out}
	code := q.run(t.Context(), []string{"--server", "http://localhost:1", "--key", "key", "--repo", "a/b", "--output", dir}, func(string) string { return "" })
	if code != exitFailed {
		t.Fatalf("code %d: %s", code, out.String())
	}
}
func TestQuarantineUsage(t *testing.T) {
	for _, args := range [][]string{
		{"--server", "bad"}, {"--server", "http://u:p@localhost"}, {"--server", "http://localhost", "--repo", "bad"},
		{"--server", "http://localhost", "--repo", "a/b", "--key", "key", "--format", "invalid"},
		{"--server", "http://localhost", "--repo", "a/b", "--key", "key", "--timeout", "0s"},
	} {
		var out bytes.Buffer
		q := quarantineFetcher{&http.Client{}, &out, &out}
		if code := q.run(t.Context(), args, func(string) string { return "" }); code != exitUsage {
			t.Errorf("args %v code %d", args, code)
		}
	}
}

func TestQuarantineDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	for _, cancelled := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelled), func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if cancelled {
				cancel()
			}
			path := filepath.Join(t.TempDir(), "patterns")
			if err := writePatterns(path, []byte("old#test")); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			q := quarantineFetcher{srv.Client(), &out, &out}
			start := time.Now()
			code := q.run(ctx, []string{"--server", srv.URL, "--key", "key", "--repo", "a/b", "--timeout", "20ms", "--output", path}, func(string) string { return "" })
			data, err := os.ReadFile(path)
			if code != exitOK || err != nil || len(data) != 0 || !strings.Contains(out.String(), "WARNING") {
				t.Fatalf("unsafe deadline failure: code%d data%q err%v %s", code, data, err, out.String())
			}
			if time.Since(start) > time.Second {
				t.Fatal("deadline did not bound request")
			}
		})
	}
}
