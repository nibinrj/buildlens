package ingest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nibinrj/buildlens/internal/db"
	"github.com/nibinrj/buildlens/internal/dbtest"
	"github.com/nibinrj/buildlens/internal/surefire"
	"github.com/nibinrj/buildlens/internal/upload"
)

// newService starts Postgres, applies the migrations and returns a Service and the pool behind it.
func newService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := db.Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool, slog.New(slog.NewJSONHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return NewService(pool), pool
}

var t0 = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

func meta(job string, number int32, result string, stages ...upload.Stage) upload.Metadata {
	return upload.Metadata{
		Repo: "nibinrj/buildlens-lab", Job: job, BuildNumber: number, Branch: "main",
		CommitSHA: "0123456789abcdef0123456789abcdef01234567", Result: result, StartedAt: t0,
		Agent: upload.Agent{Name: "agent-1", Lifecycle: "LOCAL"}, Stages: stages,
	}
}

func stage(name string, ms int64) upload.Stage {
	return upload.Stage{Name: name, StartedAt: t0, DurationMs: ms, Result: "SUCCESS"}
}

func test(class, method string, o surefire.Outcome) surefire.Result {
	r := surefire.Result{Module: "core", ClassName: class, MethodName: method, Outcome: o, DurationMs: 5}
	if o == surefire.Failed {
		r.FailureType, r.FailureHash = "org.opentest4j.AssertionFailedError", "00112233aabbccdd"
	}
	return r
}

func count(ctx context.Context, t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", sql, err)
	}
	return n
}

func TestIngest(t *testing.T) {
	svc, pool := newService(t)
	ctx := context.Background()

	t.Run("same job and build number twice updates, never duplicates", func(t *testing.T) {
		first, err := svc.Ingest(ctx, Upload{
			Meta: meta("lab/main", 1, "UNSTABLE", stage("Build", 1000), stage("Test", 2000)),
			Tests: []surefire.Result{
				test("lab.ATest", "passes", surefire.Passed),
				test("lab.ATest", "fails", surefire.Failed),
				test("lab.BTest", "flakes", surefire.Flaky),
			},
		})
		if err != nil {
			t.Fatalf("first Ingest: %v", err)
		}
		if !first.Created || first.Tests != 3 || first.Stages != 2 {
			t.Fatalf("first = %+v, want created with 3 tests and 2 stages", first)
		}

		// Same job + number, different content: a re-run of the report step after a fix, for example.
		second, err := svc.Ingest(ctx, Upload{
			Meta:  meta("lab/main", 1, "SUCCESS", stage("Build", 900)),
			Tests: []surefire.Result{test("lab.ATest", "passes", surefire.Passed), test("lab.CTest", "isNew", surefire.Passed)},
		})
		if err != nil {
			t.Fatalf("second Ingest: %v", err)
		}
		if second.Created || second.BuildID != first.BuildID {
			t.Fatalf("second = %+v, want an update of build %d", second, first.BuildID)
		}

		if n := count(ctx, t, pool, `SELECT count(*) FROM build WHERE job_name = 'lab/main' AND build_number = 1`); n != 1 {
			t.Errorf("build rows = %d, want 1", n)
		}
		if n := count(ctx, t, pool, `SELECT count(*) FROM stage_run WHERE build_id = $1`, first.BuildID); n != 1 {
			t.Errorf("stage_run rows = %d, want 1 (old stages replaced)", n)
		}
		if n := count(ctx, t, pool, `SELECT count(*) FROM test_run WHERE build_id = $1`, first.BuildID); n != 2 {
			t.Errorf("test_run rows = %d, want 2 (old runs replaced)", n)
		}

		got, err := svc.GetBuild(ctx, first.BuildID)
		if err != nil {
			t.Fatalf("GetBuild: %v", err)
		}
		if got.Result != "SUCCESS" || len(got.Stages) != 1 || got.Stages[0].DurationMs != 900 {
			t.Errorf("GetBuild = result %s, stages %+v; want the second upload's content", got.Result, got.Stages)
		}
		if got.TestSummary["PASSED"] != 2 || len(got.TestSummary) != 1 {
			t.Errorf("TestSummary = %v, want {PASSED: 2}", got.TestSummary)
		}
		if !got.UpdatedAt.After(got.CreatedAt) && !got.UpdatedAt.Equal(got.CreatedAt) {
			t.Errorf("updated_at %v before created_at %v", got.UpdatedAt, got.CreatedAt)
		}
	})

	t.Run("test cases are shared across builds of one repository", func(t *testing.T) {
		before := count(ctx, t, pool, `SELECT count(*) FROM test_case`)
		for n := int32(1); n <= 2; n++ {
			if _, err := svc.Ingest(ctx, Upload{
				Meta:  meta("lab/shared", n, "SUCCESS"),
				Tests: []surefire.Result{test("lab.SharedTest", "a", surefire.Passed), test("lab.SharedTest", "b", surefire.Passed)},
			}); err != nil {
				t.Fatalf("Ingest build %d: %v", n, err)
			}
		}
		if added := count(ctx, t, pool, `SELECT count(*) FROM test_case`) - before; added != 2 {
			t.Errorf("test_case rows added = %d, want 2 (reused by the second build)", added)
		}
		if n := count(ctx, t, pool, `SELECT count(*) FROM test_run tr JOIN build b ON b.id = tr.build_id WHERE b.job_name = 'lab/shared'`); n != 4 {
			t.Errorf("test_run rows = %d, want 4", n)
		}
	})

	t.Run("duplicate test in one upload is merged, worse outcome wins", func(t *testing.T) {
		res, err := svc.Ingest(ctx, Upload{
			Meta: meta("lab/dup", 1, "UNSTABLE"),
			Tests: []surefire.Result{
				test("lab.DupTest", "m", surefire.Passed),
				test("lab.DupTest", "m", surefire.Failed), // e.g. the same class in a Failsafe report
			},
		})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		got, err := svc.GetBuild(ctx, res.BuildID)
		if err != nil {
			t.Fatalf("GetBuild: %v", err)
		}
		if res.Tests != 1 || len(got.Tests) != 1 || got.Tests[0].Outcome != "FAILED" || got.Tests[0].DurationMs != 10 {
			t.Fatalf("tests = %+v (result %+v), want one FAILED run of 10 ms", got.Tests, res)
		}
	})

	t.Run("a failure halfway stores nothing", func(t *testing.T) {
		bad := meta("lab/rollback", 1, "SUCCESS", upload.Stage{Name: "Build", StartedAt: t0, DurationMs: 1, Result: "BOGUS"})
		_, err := svc.Ingest(ctx, Upload{Meta: bad, Tests: []surefire.Result{test("lab.RollbackTest", "m", surefire.Passed)}})
		if err == nil {
			t.Fatal("Ingest with an invalid stage result succeeded; want the CHECK constraint to fail it")
		}
		if n := count(ctx, t, pool, `SELECT count(*) FROM build WHERE job_name = 'lab/rollback'`); n != 0 {
			t.Errorf("build rows after failed ingest = %d, want 0 (rolled back)", n)
		}
		if n := count(ctx, t, pool, `SELECT count(*) FROM test_case WHERE class_name = 'lab.RollbackTest'`); n != 0 {
			t.Errorf("test_case rows after failed ingest = %d, want 0 (rolled back)", n)
		}
	})

	t.Run("existing default branch is kept", func(t *testing.T) {
		m := meta("other/main", 1, "SUCCESS")
		m.Repo, m.DefaultBranch = "nibinrj/other", "trunk"
		if _, err := svc.Ingest(ctx, Upload{Meta: m}); err != nil {
			t.Fatalf("first Ingest: %v", err)
		}
		m.BuildNumber, m.DefaultBranch = 2, "main"
		if _, err := svc.Ingest(ctx, Upload{Meta: m}); err != nil {
			t.Fatalf("second Ingest: %v", err)
		}
		var branch string
		if err := pool.QueryRow(ctx, `SELECT default_branch FROM repository WHERE name = 'nibinrj/other'`).Scan(&branch); err != nil {
			t.Fatalf("query: %v", err)
		}
		if branch != "trunk" {
			t.Errorf("default_branch = %q, want trunk (set once, not overwritten by uploads)", branch)
		}
	})

	t.Run("optional fields round-trip", func(t *testing.T) {
		m := meta("lab/pr", 4, "FAILURE")
		pr, dur := int32(12), int64(61_000)
		end := t0.Add(61 * time.Second)
		m.Branch, m.PRNumber, m.TestedTreeSHA, m.FinishedAt, m.DurationMs = "PR-12", &pr, "89abcdef0123456789abcdef0123456789abcdef", &end, &dur
		res, err := svc.Ingest(ctx, Upload{Meta: m})
		if err != nil {
			t.Fatalf("Ingest: %v", err)
		}
		got, err := svc.GetBuild(ctx, res.BuildID)
		if err != nil {
			t.Fatalf("GetBuild: %v", err)
		}
		if got.PRNumber == nil || *got.PRNumber != 12 || got.TestedTreeSHA == nil || *got.TestedTreeSHA != m.TestedTreeSHA ||
			got.FinishedAt == nil || !got.FinishedAt.Equal(end) || got.DurationMs == nil || *got.DurationMs != dur ||
			got.Agent.Lifecycle == nil || *got.Agent.Lifecycle != "LOCAL" || got.Repository != "nibinrj/buildlens-lab" {
			t.Errorf("GetBuild = %+v; optional fields did not round-trip", got)
		}
	})

	t.Run("unknown build is ErrNotFound", func(t *testing.T) {
		if _, err := svc.GetBuild(ctx, 987654); !errors.Is(err, ErrNotFound) {
			t.Fatalf("GetBuild(unknown) error = %v, want ErrNotFound", err)
		}
	})
}
