package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/quarantine"
	"github.com/nibinrj/buildlens/internal/surefire"
)

func TestP3Lifecycle(t *testing.T) {
	_, pool := newService(t)
	cfg := quarantine.Defaults()
	cfg.ReleasePasses = 3
	svc := NewServiceWithPolicy(pool, cfg)
	qs := quarantine.NewService(pool, cfg)
	ctx := t.Context()
	upload := func(t *testing.T, repo string, n int32, stage string, outcome surefire.Outcome, log string) Result {
		t.Helper()
		m := meta(repo+"/main", n, "SUCCESS")
		m.Repo = repo
		m.StartedAt = t0.Add(time.Duration(n) * time.Minute)
		m.TestedTreeSHA = fmt.Sprintf("%040x", n)
		u := Upload{Meta: m, LogTail: log}
		r := test("lab.FlakyTest", "victim", outcome)
		r.FailureHash = "abc123"
		if stage == "QUARANTINE" {
			u.QuarantineTests = []surefire.Result{r}
		} else {
			u.Tests = []surefire.Result{r}
		}
		got, err := svc.Ingest(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	list := func(t *testing.T, repo string) []quarantine.Entry {
		t.Helper()
		rows, err := qs.List(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	for _, tc := range []struct {
		name, log string
		want      int
	}{
		{"ordinary", "", 1}, {"infra only", "ERROR: process exited with exit code 137", 0},
		{"diagnostic near miss", "Expected: process exit code 137", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := "p3/" + fmt.Sprint(tc.want) + fmt.Sprint(len(tc.log))
			upload(t, repo, 1, "BLOCKING", surefire.Flaky, tc.log)
			last := upload(t, repo, 2, "BLOCKING", surefire.Flaky, tc.log)
			entries := list(t, repo)
			if len(entries) != tc.want {
				t.Fatalf("entries=%+v", entries)
			}
			b, err := svc.GetBuild(ctx, last.BuildID)
			if err != nil {
				t.Fatal(err)
			}
			if b.InfraFailure != (tc.want == 0) {
				t.Fatalf("infra=%v", b.InfraFailure)
			}
			if tc.want == 1 {
				var evidence struct {
					Findings []struct {
						Rule string
						Runs []struct {
							FailureHash string `json:"failure_hash"`
						}
					}
				}
				if err := json.Unmarshal(entries[0].Evidence, &evidence); err != nil {
					t.Fatal(err)
				}
				if len(evidence.Findings) == 0 || evidence.Findings[0].Rule != "R1" || evidence.Findings[0].Runs[0].FailureHash != "abc123" {
					t.Fatalf("evidence=%s", entries[0].Evidence)
				}
			}
		})
	}
	t.Run("release recomputes streak with retry infra skip failure and out of order", func(t *testing.T) {
		repo := "p3/release"
		upload(t, repo, 1, "BLOCKING", surefire.Flaky, "")
		upload(t, repo, 2, "BLOCKING", surefire.Flaky, "")
		if len(list(t, repo)) != 1 {
			t.Fatal("not quarantined")
		}
		upload(t, repo, 3, "QUARANTINE", surefire.Passed, "")
		upload(t, repo, 3, "QUARANTINE", surefire.Passed, "") // same stored build, one pass
		upload(t, repo, 4, "QUARANTINE", surefire.Skipped, "")
		upload(t, repo, 5, "QUARANTINE", surefire.Passed, "ERROR: exit code 137")
		if got := list(t, repo); len(got) != 1 || got[0].ConsecutivePasses != 1 {
			t.Fatalf("retry/neutral advanced: %+v", got)
		}
		upload(t, repo, 7, "QUARANTINE", surefire.Passed, "")
		upload(t, repo, 6, "QUARANTINE", surefire.Failed, "") // late failure before build7
		if got := list(t, repo); len(got) != 1 || got[0].ConsecutivePasses != 1 {
			t.Fatalf("late failure not reordered: %+v", got)
		}
		upload(t, repo, 8, "QUARANTINE", surefire.Flaky, "")
		if got := list(t, repo); len(got) != 1 || got[0].ConsecutivePasses != 0 {
			t.Fatalf("flake did not reset: %+v", got)
		}
		for n := int32(9); n <= 11; n++ {
			upload(t, repo, n, "QUARANTINE", surefire.Passed, "")
		}
		if len(list(t, repo)) != 0 {
			t.Fatal("K passes did not release")
		}
		upload(t, repo, 2, "BLOCKING", surefire.Flaky, "") // old replay must not use old evidence
		if len(list(t, repo)) != 0 {
			t.Fatal("stale evidence requarantined")
		}
		upload(t, repo, 12, "BLOCKING", surefire.Flaky, "")
		if len(list(t, repo)) != 0 {
			t.Fatal("one new flake triggered R1")
		}
		upload(t, repo, 13, "BLOCKING", surefire.Flaky, "")
		if len(list(t, repo)) != 1 {
			t.Fatal("fresh evidence did not quarantine")
		}
		if n := count(ctx, t, pool, "SELECT count(*) FROM quarantine q JOIN test_case tc ON tc.id=q.test_case_id JOIN repository r ON r.id=tc.repository_id WHERE r.name=$1", repo); n != 2 {
			t.Fatalf("history=%d", n)
		}
	})
	t.Run("manual create release audit conflict and missing", func(t *testing.T) {
		repo := "p3/manual"
		upload(t, repo, 1, "BLOCKING", surefire.Passed, "")
		req := quarantine.Request{Repo: repo, ClassName: "lab.FlakyTest", MethodName: "victim", Reason: "owner investigation"}
		id, err := qs.CreateManual(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := qs.CreateManual(ctx, req); !errors.Is(err, quarantine.ErrConflict) {
			t.Fatalf("duplicate=%v", err)
		}
		rows := list(t, repo)
		if len(rows) != 1 || !rows[0].Manual || rows[0].ReasonRule != "MANUAL" {
			t.Fatalf("manual=%+v", rows)
		}
		if err := qs.ReleaseManual(ctx, id, "fixed fixture"); err != nil {
			t.Fatal(err)
		}
		if err := qs.ReleaseManual(ctx, id, "again"); !errors.Is(err, quarantine.ErrNotFound) {
			t.Fatalf("second release=%v", err)
		}
		if len(list(t, repo)) != 0 {
			t.Fatal("manual release still active")
		}
		var audit []byte
		if err := pool.QueryRow(ctx, "SELECT evidence FROM quarantine WHERE id=$1", id).Scan(&audit); err != nil {
			t.Fatal(err)
		}
		var evidence map[string]any
		if err := json.Unmarshal(audit, &evidence); err != nil {
			t.Fatal(err)
		}
		if evidence["release_reason"] != "fixed fixture" || evidence["manual_reason"] != req.Reason {
			t.Fatalf("audit=%s", audit)
		}
		req.MethodName = "missing"
		if _, err := qs.CreateManual(ctx, req); !errors.Is(err, quarantine.ErrNotFound) {
			t.Fatalf("missing test=%v", err)
		}
		req.Repo = "p3/unknown"
		if _, err := qs.CreateManual(ctx, req); !errors.Is(err, quarantine.ErrNotFound) {
			t.Fatalf("missing repo=%v", err)
		}
		if got := list(t, req.Repo); got == nil || len(got) != 0 {
			t.Fatalf("unknown list=%v", got)
		}
	})
	t.Run("cap counts manual quarantines and deduplicates alert retries", func(t *testing.T) {
		repo := "p3/cap"
		m := meta(repo+"/main", 1, "SUCCESS")
		m.Repo = repo
		tests := []surefire.Result{test("lab.CapTest", "manual", surefire.Passed), test("lab.CapTest", "auto", surefire.Flaky)}
		if _, err := svc.Ingest(ctx, Upload{Meta: m, Tests: tests}); err != nil {
			t.Fatal(err)
		}
		req := quarantine.Request{Repo: repo, ClassName: "lab.CapTest", MethodName: "manual", Reason: "investigate"}
		if _, err := qs.CreateManual(ctx, req); err != nil {
			t.Fatal(err)
		}
		req.MethodName = "auto"
		if _, err := qs.CreateManual(ctx, req); !errors.Is(err, quarantine.ErrCap) {
			t.Fatalf("manual cap=%v", err)
		}
		m.BuildNumber = 2
		m.StartedAt = t0.Add(time.Minute)
		for range 2 {
			if _, err := svc.Ingest(ctx, Upload{Meta: m, Tests: tests}); err != nil {
				t.Fatal(err)
			}
		}
		if len(list(t, repo)) != 1 {
			t.Fatal("automatic cap exceeded")
		}
		n := count(ctx, t, pool, "SELECT count(*) FROM alert a JOIN repository r ON r.id=a.repository_id WHERE r.name=$1 AND a.type='FLAKY'", repo)
		if n != 1 {
			t.Fatalf("cap alerts=%d want1", n)
		}
	})
	t.Run("concurrent manual requests serialize at cap", func(t *testing.T) {
		repo := "p3/concurrent"
		m := meta(repo+"/main", 1, "SUCCESS")
		m.Repo = repo
		var tests []surefire.Result
		for n := range 8 {
			tests = append(tests, test("lab.ConcurrentTest", fmt.Sprint("m", n), surefire.Passed))
		}
		if _, err := svc.Ingest(ctx, Upload{Meta: m, Tests: tests}); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make(chan error, len(tests))
		for _, r := range tests {
			wg.Go(func() {
				_, err := qs.CreateManual(ctx, quarantine.Request{Repo: repo, ClassName: r.ClassName, MethodName: r.MethodName, Reason: "parallel request"})
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		success := 0
		for err := range errs {
			if err == nil {
				success++
			} else if !errors.Is(err, quarantine.ErrCap) {
				t.Fatal(err)
			}
		}
		if success != 1 || len(list(t, repo)) != 1 {
			t.Fatalf("cap admitted=%d", success)
		}
	})
	t.Run("cross-stage duplicate rejects before storage", func(t *testing.T) {
		m := meta("p3/duplicate", 1, "SUCCESS")
		m.Repo = "p3/duplicate"
		r := test("lab.DuplicateTest", "a", surefire.Passed)
		if _, err := svc.Ingest(ctx, Upload{Meta: m, Tests: []surefire.Result{r}, QuarantineTests: []surefire.Result{r}}); err == nil {
			t.Fatal("duplicate accepted")
		}
		if count(ctx, t, pool, "SELECT count(*) FROM repository WHERE name=$1", m.Repo) != 0 {
			t.Fatal("invalid upload persisted")
		}
	})
}
