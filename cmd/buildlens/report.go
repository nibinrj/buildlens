package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nibinrj/buildlens/internal/upload"
)

const defaultReports = "**/target/surefire-reports/TEST-*.xml,**/target/failsafe-reports/TEST-*.xml"

// reporter uploads one build. Its fields are what tests replace: the HTTP client and the wait between retries.
type reporter struct {
	client *http.Client
	sleep  func(ctx context.Context, d time.Duration) error
	stdout io.Writer
	stderr io.Writer
}

// reportFlags holds the parsed command line.
type reportFlags struct {
	server, key, repo, defaultBranch, job, branch, commit, tree, result string
	buildNumber, pr                                                     string
	startedAtMs, durationMs                                             string
	agent, agentInstanceType, agentLifecycle                            string
	stagesFile, logTailFile, reports, dir                               string
	strict                                                              bool
	timeout                                                             time.Duration
	retries                                                             int
}

// parseReportFlags reads the flags. Every flag defaults to an environment variable, so a Jenkins pipeline only
// passes what Jenkins does not already set (JOB_NAME, BUILD_NUMBER, BRANCH_NAME, CHANGE_ID and NODE_NAME are
// standard Jenkins variables).
func parseReportFlags(args []string, getenv func(string) string, out io.Writer) (reportFlags, error) {
	var f reportFlags
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(out)

	fs.StringVar(&f.server, "server", getenv("BUILDLENS_SERVER_URL"), "BuildLens base URL (env BUILDLENS_SERVER_URL)")
	fs.StringVar(&f.key, "key", getenv("BUILDLENS_KEY"), "ingest API key (env BUILDLENS_KEY; prefer the env var: flags show up in ps)")
	fs.StringVar(&f.repo, "repo", getenv("BUILDLENS_REPO"), "repository as owner/name (env BUILDLENS_REPO)")
	fs.StringVar(&f.defaultBranch, "default-branch", getenv("BUILDLENS_DEFAULT_BRANCH"), "repository default branch, used when it is first seen (default main)")
	fs.StringVar(&f.job, "job", getenv("JOB_NAME"), "Jenkins job name (env JOB_NAME)")
	fs.StringVar(&f.buildNumber, "build-number", getenv("BUILD_NUMBER"), "build number (env BUILD_NUMBER)")
	fs.StringVar(&f.branch, "branch", getenv("BRANCH_NAME"), "branch (env BRANCH_NAME)")
	fs.StringVar(&f.pr, "pr", getenv("CHANGE_ID"), "pull request number, empty for branch builds (env CHANGE_ID)")
	fs.StringVar(&f.commit, "commit", getenv("GIT_COMMIT"), "commit SHA (env GIT_COMMIT)")
	fs.StringVar(&f.tree, "tree", "", "git tree hash of the tested checkout (git rev-parse HEAD^{tree})")
	fs.StringVar(&f.result, "result", "", "build result: SUCCESS, UNSTABLE, FAILURE, ABORTED, NOT_BUILT")
	fs.StringVar(&f.startedAtMs, "started-at", "", "build start, epoch milliseconds")
	fs.StringVar(&f.durationMs, "duration-ms", "", "build duration in milliseconds (default: now minus --started-at)")
	fs.StringVar(&f.agent, "agent", getenv("NODE_NAME"), "agent name (env NODE_NAME)")
	fs.StringVar(&f.agentInstanceType, "agent-instance-type", getenv("BUILDLENS_AGENT_INSTANCE_TYPE"), "agent instance type (env BUILDLENS_AGENT_INSTANCE_TYPE)")
	fs.StringVar(&f.agentLifecycle, "agent-lifecycle", getenv("BUILDLENS_AGENT_LIFECYCLE"), "LOCAL, SPOT or ON_DEMAND (env BUILDLENS_AGENT_LIFECYCLE)")
	fs.StringVar(&f.stagesFile, "stages", "", "stages.json written by the shared library")
	fs.StringVar(&f.logTailFile, "log-tail", "", "file with the last lines of the build log")
	fs.StringVar(&f.reports, "reports", defaultReports, "comma-separated report globs, relative to --dir")
	fs.StringVar(&f.dir, "dir", ".", "directory to search for reports (the workspace)")
	fs.BoolVar(&f.strict, "strict", false, "exit 1 when the upload fails (default: warn and exit 0)")
	fs.DurationVar(&f.timeout, "timeout", 30*time.Second, "timeout per upload attempt")
	fs.IntVar(&f.retries, "retries", 3, "retries after the first attempt, for network errors, 429 and 5xx")

	if err := fs.Parse(args); err != nil {
		return f, err
	}
	if fs.NArg() > 0 {
		return f, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if f.server == "" {
		return f, errors.New("--server (or BUILDLENS_SERVER_URL) is required")
	}
	if u, err := url.Parse(f.server); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return f, fmt.Errorf("--server %q must be an http(s) URL such as http://buildlens-server:8080", f.server)
	}
	if f.key == "" {
		return f, errors.New("--key (or BUILDLENS_KEY) is required")
	}
	if f.retries < 0 || f.timeout <= 0 {
		return f, errors.New("--retries must be >= 0 and --timeout > 0")
	}
	return f, nil
}

// stageRecord is one line the shared library's timedStage records (times in epoch milliseconds).
type stageRecord struct {
	Name       string `json:"name"`
	StartedAt  int64  `json:"startedAt"`
	DurationMs int64  `json:"durationMs"`
	Result     string `json:"result"`
}

// buildMetadata turns the flags into the upload's metadata and validates it.
func buildMetadata(f reportFlags, now time.Time) (upload.Metadata, error) {
	m := upload.Metadata{
		Repo: f.repo, DefaultBranch: f.defaultBranch, Job: f.job, Branch: f.branch,
		CommitSHA: f.commit, TestedTreeSHA: f.tree, Result: strings.ToUpper(f.result),
		Agent: upload.Agent{Name: f.agent, InstanceType: f.agentInstanceType, Lifecycle: strings.ToUpper(f.agentLifecycle)},
	}

	n, err := strconv.ParseInt(f.buildNumber, 10, 32)
	if err != nil {
		return m, fmt.Errorf("--build-number %q is not a number", f.buildNumber)
	}
	m.BuildNumber = int32(n)

	if f.pr != "" {
		pr, err := strconv.ParseInt(f.pr, 10, 32)
		if err != nil {
			return m, fmt.Errorf("--pr %q is not a number", f.pr)
		}
		p := int32(pr)
		m.PRNumber = &p
	}

	startMs, err := strconv.ParseInt(f.startedAtMs, 10, 64)
	if err != nil {
		return m, fmt.Errorf("--started-at %q is not epoch milliseconds", f.startedAtMs)
	}
	m.StartedAt = time.UnixMilli(startMs).UTC()

	var dur int64
	if f.durationMs != "" {
		if dur, err = strconv.ParseInt(f.durationMs, 10, 64); err != nil {
			return m, fmt.Errorf("--duration-ms %q is not a number", f.durationMs)
		}
	} else {
		dur = max(now.Sub(m.StartedAt).Milliseconds(), 0)
	}
	end := m.StartedAt.Add(time.Duration(dur) * time.Millisecond)
	m.DurationMs, m.FinishedAt = &dur, &end

	if f.stagesFile != "" {
		if m.Stages, err = readStages(f.stagesFile); err != nil {
			return m, err
		}
	}
	if err := m.Validate(); err != nil {
		return m, fmt.Errorf("invalid build metadata: %w", err)
	}
	return m, nil
}

func readStages(path string) ([]upload.Stage, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is the pipeline's own stages file
	if err != nil {
		return nil, fmt.Errorf("read --stages: %w", err)
	}
	var recs []stageRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil, fmt.Errorf("--stages %s is not a JSON array of stages: %w", path, err)
	}
	stages := make([]upload.Stage, 0, len(recs))
	for _, r := range recs {
		stages = append(stages, upload.Stage{
			Name: r.Name, StartedAt: time.UnixMilli(r.StartedAt).UTC(), DurationMs: r.DurationMs, Result: r.Result,
		})
	}
	return stages, nil
}

// run is "buildlens report". It returns the process exit code.
func (r reporter) run(ctx context.Context, args []string, getenv func(string) string) int {
	f, err := parseReportFlags(args, getenv, r.stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintln(r.stderr, "buildlens report:", err)
		return exitUsage
	}

	meta, err := buildMetadata(f, time.Now())
	if err != nil {
		fmt.Fprintln(r.stderr, "buildlens report:", err)
		return exitUsage
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		fmt.Fprintln(r.stderr, "buildlens report: encode metadata:", err)
		return exitUsage
	}

	reports, err := findReports(f.dir, strings.Split(f.reports, ","))
	if err != nil {
		fmt.Fprintln(r.stderr, "buildlens report:", err)
		return exitUsage
	}
	if len(reports) == 0 {
		fmt.Fprintf(r.stderr, "buildlens report: no test reports matched %q under %s; uploading build metadata only\n", f.reports, f.dir)
	}

	body := uploadBody{meta: metaJSON, dir: f.dir, reports: reports, logTail: f.logTailFile}
	answer, err := r.send(ctx, f, body)
	if err != nil {
		fmt.Fprintln(r.stderr, "buildlens report: WARNING: build not uploaded to BuildLens:", err)
		if f.strict {
			return exitFailed
		}
		return exitOK
	}
	fmt.Fprintf(r.stdout, "buildlens report: uploaded %s #%d with %d report file(s): %s\n",
		meta.Job, meta.BuildNumber, len(reports), strings.TrimSpace(answer))
	return exitOK
}

// uploadBody is what one request sends. It is rebuilt for every attempt because a streamed body can be read once.
type uploadBody struct {
	meta    []byte
	dir     string
	reports []string // relative, slash-separated
	logTail string
}

// statusError is an HTTP answer that will not change if the same request is sent again (4xx except 429),
// so send does not retry it.
type statusError struct {
	status int
	body   string
}

func (e *statusError) Error() string { return fmt.Sprintf("server answered %d: %s", e.status, e.body) }

// send posts the upload, retrying network errors, 429 and 5xx with exponential backoff and jitter.
// It returns the server's answer on success.
func (r reporter) send(ctx context.Context, f reportFlags, body uploadBody) (string, error) {
	endpoint := strings.TrimRight(f.server, "/") + "/api/v1/builds"
	var lastErr error
	for attempt := 0; attempt <= f.retries; attempt++ {
		if attempt > 0 {
			wait := backoff(attempt)
			fmt.Fprintf(r.stderr, "buildlens report: attempt %d failed (%v); retrying in %s\n", attempt, lastErr, wait.Round(time.Millisecond))
			if err := r.sleep(ctx, wait); err != nil {
				return "", fmt.Errorf("cancelled while waiting to retry: %w (last error: %w)", err, lastErr)
			}
		}
		answer, err := r.post(ctx, endpoint, f.key, f.timeout, body)
		if err == nil {
			return answer, nil
		}
		var se *statusError
		if errors.As(err, &se) {
			return "", err // retrying would get the same answer
		}
		lastErr = err
	}
	return "", fmt.Errorf("gave up after %d attempts: %w", f.retries+1, lastErr)
}

// backoff is 1s, 2s, 4s, ... plus up to 25% random jitter, so agents do not retry in lockstep.
func backoff(attempt int) time.Duration {
	base := time.Second << (attempt - 1)
	return base + time.Duration(rand.Int64N(int64(base/4)+1)) //nolint:gosec // jitter, not security
}

// post sends one attempt. The multipart body is streamed through a pipe: a goroutine writes the parts while
// the HTTP client reads them, so report files are never loaded into memory all at once.
func (r reporter) post(ctx context.Context, endpoint, key string, timeout time.Duration, body uploadBody) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// CloseWithError(nil) is a normal close; any error makes the request fail with it instead of hanging.
		pw.CloseWithError(writeParts(mw, body))
	}()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, pr)
	if err != nil { // the URL was validated with the flags, so this is not expected
		_ = pr.Close()
		<-done
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := r.client.Do(req)
	// The client closes the request body when Do returns, which unblocks the writer goroutine.
	_ = pr.Close()
	<-done
	if err != nil {
		return "", fmt.Errorf("post %s: %w", endpoint, err)
	}
	defer resp.Body.Close() //nolint:errcheck // nothing useful to do if close fails

	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return string(answer), nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", fmt.Errorf("server answered %d: %s", resp.StatusCode, strings.TrimSpace(string(answer)))
	default:
		return "", &statusError{status: resp.StatusCode, body: strings.TrimSpace(string(answer))}
	}
}

// writeParts writes metadata first, then each report, then the log tail.
func writeParts(mw *multipart.Writer, body uploadBody) error {
	w, err := mw.CreateFormField(upload.PartMetadata)
	if err != nil {
		return err
	}
	if _, err := w.Write(body.meta); err != nil {
		return err
	}
	for _, rel := range body.reports {
		if err := copyFilePart(mw, upload.PartReport, rel, filepath.Join(body.dir, filepath.FromSlash(rel))); err != nil {
			return err
		}
	}
	if body.logTail != "" {
		if err := copyFilePart(mw, upload.PartLog, filepath.Base(body.logTail), body.logTail); err != nil {
			return err
		}
	}
	return mw.Close()
}

func copyFilePart(mw *multipart.Writer, field, name, path string) error {
	f, err := os.Open(path) //nolint:gosec // paths come from the workspace search or the pipeline's own flags
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close() //nolint:errcheck // read-only

	w, err := mw.CreateFormFile(field, name)
	if err != nil {
		return err
	}
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

// sleepCtx waits d, or returns early with ctx's error when ctx ends.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
