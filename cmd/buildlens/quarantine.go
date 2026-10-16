package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var javaClass = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*$`)
var javaMethod = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

type quarantineFlags struct {
	server, key, repo, format, output string
	timeout                           time.Duration
}
type quarantineTest struct {
	ClassName  string `json:"class_name"`
	MethodName string `json:"method_name"`
}
type quarantineFetcher struct {
	client         *http.Client
	stdout, stderr io.Writer
}

func (q quarantineFetcher) run(ctx context.Context, args []string, getenv func(string) string) int {
	f, err := parseQuarantineFlags(args, getenv, q.stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(q.stderr, "buildlens quarantine:", err)
		return exitUsage
	}
	// Clear the previous snapshot before a fetch. A failed write must never report success.
	if err := writePatterns(f.output, nil); err != nil {
		fmt.Fprintln(q.stderr, "buildlens quarantine: cannot clear output:", err)
		return exitFailed
	}
	patterns, err := q.fetch(ctx, f)
	if err != nil {
		fmt.Fprintf(q.stderr, "buildlens quarantine: WARNING: could not fetch safe quarantine list (%v); empty file, all tests run\n", err)
		return exitOK
	}
	content := strings.Join(patterns, "\n")
	if content != "" {
		content += "\n"
	}
	if err := writePatterns(f.output, []byte(content)); err != nil {
		fmt.Fprintln(q.stderr, "buildlens quarantine: cannot write output:", err)
		return exitFailed
	}
	fmt.Fprintf(q.stdout, "buildlens quarantine: wrote %d test pattern(s) to %s (%s)\n", len(patterns), f.output, f.format)
	return exitOK
}
func parseQuarantineFlags(args []string, getenv func(string) string, out io.Writer) (quarantineFlags, error) {
	f := quarantineFlags{}
	fs := flag.NewFlagSet("quarantine", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&f.server, "server", getenv("BUILDLENS_SERVER_URL"), "BuildLens URL (or BUILDLENS_SERVER_URL)")
	fs.StringVar(&f.key, "key", getenv("BUILDLENS_KEY"), "API key (prefer BUILDLENS_KEY)")
	fs.StringVar(&f.repo, "repo", "", "owner/name")
	fs.StringVar(&f.format, "format", "surefire-excludes", "surefire-excludes or surefire-only")
	fs.StringVar(&f.output, "output", ".buildlens/quarantine-tests.txt", "pattern file")
	fs.DurationVar(&f.timeout, "timeout", 15*time.Second, "request deadline")
	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if fs.NArg() > 0 {
		return f, errors.New("unexpected arguments")
	}
	if f.server == "" {
		return f, errors.New("--server (or BUILDLENS_SERVER_URL) is required")
	}
	u, err := url.Parse(f.server)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return f, errors.New("--server must be an http(s) URL without credentials, query or fragment")
	}
	owner, name, ok := strings.Cut(f.repo, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return f, errors.New("--repo must be owner/name")
	}
	if f.key == "" {
		return f, errors.New("--key (or BUILDLENS_KEY) is required")
	}
	if f.format != "surefire-excludes" && f.format != "surefire-only" {
		return f, errors.New("--format must be surefire-excludes or surefire-only")
	}
	if f.timeout <= 0 || f.output == "" {
		return f, errors.New("positive --timeout and nonempty --output required")
	}
	return f, nil
}
func (q quarantineFetcher) fetch(ctx context.Context, f quarantineFlags) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, f.timeout)
	defer cancel()
	endpoint := strings.TrimRight(f.server, "/") + "/api/v1/quarantine?repo=" + url.QueryEscape(f.repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+f.key)
	resp, err := q.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch list: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only response
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server answered %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("read list: %w", err)
	}
	if len(data) > 1<<20 {
		return nil, errors.New("quarantine response exceeds 1 MiB")
	}
	var tests []quarantineTest
	if err := json.Unmarshal(data, &tests); err != nil {
		return nil, fmt.Errorf("decode list: %w", err)
	}
	seen := map[string]bool{}
	for _, test := range tests {
		if !javaClass.MatchString(test.ClassName) || !javaMethod.MatchString(test.MethodName) {
			return nil, errors.New("unsupported class/method identifier; refusing a broad or parameterized filter")
		}
		seen[test.ClassName+"#"+test.MethodName] = true
	}
	patterns := make([]string, 0, len(seen))
	for pattern := range seen {
		patterns = append(patterns, pattern)
	}
	sort.Strings(patterns)
	return patterns, nil
}
func writePatterns(file string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o750); err != nil {
		return fmt.Errorf("create pattern directory: %w", err)
	}
	if err := os.WriteFile(file, content, 0o600); err != nil {
		return fmt.Errorf("write pattern file: %w", err)
	}
	return nil
}
