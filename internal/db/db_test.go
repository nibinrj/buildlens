package db

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/nibinrj/buildlens/internal/dbtest"
	"github.com/nibinrj/buildlens/internal/store"
)

// wantTables is every table in the plan's data model (docs/plan.md, "Tables").
var wantTables = []string{
	"alert", "build", "infra_event", "price", "quarantine",
	"repository", "stage_run", "test_case", "test_run",
}

// startPostgres runs a throwaway Postgres in Docker (internal/dbtest) and returns a pool to it.
func startPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := dbtest.URL(t)

	openCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := Open(openCtx, url)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func quietLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

func publicTables(ctx context.Context, t *testing.T, pool *pgxpool.Pool) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = 'public' AND table_type = 'BASE TABLE' AND table_name <> 'goose_db_version'
		ORDER BY table_name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	tables, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scan tables: %v", err)
	}
	return tables
}

// TestMigrations proves the migrations apply to a real Postgres, create every table in the plan,
// can be applied twice, and can be rolled back and applied again.
func TestMigrations(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()

	if err := Migrate(ctx, pool, quietLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	got := publicTables(ctx, t, pool)
	if !slices.Equal(got, wantTables) {
		t.Fatalf("tables after migrate = %v, want %v", got, wantTables)
	}

	t.Run("00002 adds build.tested_tree_sha", func(t *testing.T) {
		var nullable string
		err := pool.QueryRow(ctx, `
			SELECT is_nullable FROM information_schema.columns
			WHERE table_name = 'build' AND column_name = 'tested_tree_sha'`).Scan(&nullable)
		if err != nil {
			t.Fatalf("column lookup: %v", err)
		}
		if nullable != "YES" {
			t.Errorf("tested_tree_sha is_nullable = %s, want YES", nullable)
		}
	})

	t.Run("second run is a no-op", func(t *testing.T) {
		if err := Migrate(ctx, pool, quietLogger()); err != nil {
			t.Fatalf("second Migrate: %v", err)
		}
	})

	t.Run("down to zero then up again", func(t *testing.T) {
		sqlDB := stdlib.OpenDBFromPool(pool)
		defer sqlDB.Close() //nolint:errcheck // test cleanup

		provider, err := newProvider(sqlDB)
		if err != nil {
			t.Fatalf("newProvider: %v", err)
		}
		if _, err := provider.DownTo(ctx, 0); err != nil {
			t.Fatalf("DownTo(0): %v", err)
		}
		if left := publicTables(ctx, t, pool); len(left) != 0 {
			t.Fatalf("tables after down = %v, want none", left)
		}
		if err := Migrate(ctx, pool, quietLogger()); err != nil {
			t.Fatalf("Migrate after down: %v", err)
		}
		if got := publicTables(ctx, t, pool); !slices.Equal(got, wantTables) {
			t.Fatalf("tables after re-up = %v, want %v", got, wantTables)
		}
	})
}

// TestSchemaConstraints proves the data-model rules the database itself enforces, and that the
// sqlc-generated queries run against the migrated schema.
func TestSchemaConstraints(t *testing.T) {
	pool := startPostgres(t)
	ctx := context.Background()
	if err := Migrate(ctx, pool, quietLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	q := store.New(pool)

	repo, err := q.UpsertRepository(ctx, store.UpsertRepositoryParams{Name: "nibinrj/buildlens-lab", DefaultBranch: "main"})
	if err != nil {
		t.Fatalf("UpsertRepository: %v", err)
	}

	t.Run("upsert repository updates instead of duplicating", func(t *testing.T) {
		again, err := q.UpsertRepository(ctx, store.UpsertRepositoryParams{Name: "nibinrj/buildlens-lab", DefaultBranch: "trunk"})
		if err != nil {
			t.Fatalf("second UpsertRepository: %v", err)
		}
		if again.ID != repo.ID || again.DefaultBranch != "trunk" {
			t.Fatalf("upsert = %+v, want id %d with default branch trunk", again, repo.ID)
		}
		all, err := q.ListRepositories(ctx)
		if err != nil {
			t.Fatalf("ListRepositories: %v", err)
		}
		if len(all) != 1 {
			t.Fatalf("ListRepositories returned %d rows, want 1", len(all))
		}
	})

	t.Run("unknown repository is ErrNoRows", func(t *testing.T) {
		_, err := q.GetRepositoryByName(ctx, "nobody/nothing")
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("GetRepositoryByName error = %v, want pgx.ErrNoRows", err)
		}
	})

	// Fixed rows the constraint cases build on.
	var buildID, testCaseID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO build (repository_id, job_name, build_number, branch, commit_sha, result, started_at)
		VALUES ($1, 'lab/main', 1, 'main', 'abc123', 'SUCCESS', now()) RETURNING id`, repo.ID).Scan(&buildID)
	if err != nil {
		t.Fatalf("insert build: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO test_case (repository_id, class_name, method_name)
		VALUES ($1, 'lab.CoreTest', 'adds') RETURNING id`, repo.ID).Scan(&testCaseID)
	if err != nil {
		t.Fatalf("insert test_case: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO quarantine (test_case_id, state, reason_rule) VALUES ($1, 'QUARANTINED', 'R1')`, testCaseID)
	if err != nil {
		t.Fatalf("insert quarantine: %v", err)
	}

	t.Run("generated queries read the rows back", func(t *testing.T) {
		b, err := q.GetBuildByJobAndNumber(ctx, store.GetBuildByJobAndNumberParams{JobName: "lab/main", BuildNumber: 1})
		if err != nil {
			t.Fatalf("GetBuildByJobAndNumber: %v", err)
		}
		if b.ID != buildID || b.InfraFailure || b.PrNumber != nil {
			t.Fatalf("build = %+v, want id %d, infra_failure false, no PR", b, buildID)
		}
		active, err := q.ListActiveQuarantine(ctx, repo.ID)
		if err != nil {
			t.Fatalf("ListActiveQuarantine: %v", err)
		}
		if len(active) != 1 || active[0].ClassName != "lab.CoreTest" || active[0].MethodName != "adds" {
			t.Fatalf("active quarantine = %+v, want lab.CoreTest#adds", active)
		}
	})

	const (
		uniqueViolation = "23505"
		checkViolation  = "23514"
		fkViolation     = "23503"
	)
	tests := []struct {
		name     string
		sql      string
		args     []any
		wantCode string
	}{
		{
			name: "same job and build number twice",
			sql: `INSERT INTO build (repository_id, job_name, build_number, branch, commit_sha, result, started_at)
			      VALUES ($1, 'lab/main', 1, 'main', 'def456', 'FAILURE', now())`,
			args: []any{repo.ID}, wantCode: uniqueViolation,
		},
		{
			name: "unknown build result",
			sql: `INSERT INTO build (repository_id, job_name, build_number, branch, commit_sha, result, started_at)
			      VALUES ($1, 'lab/main', 2, 'main', 'def456', 'GREEN', now())`,
			args: []any{repo.ID}, wantCode: checkViolation,
		},
		{
			name: "unknown agent lifecycle",
			sql: `INSERT INTO build (repository_id, job_name, build_number, branch, commit_sha, result, started_at, agent_lifecycle)
			      VALUES ($1, 'lab/main', 3, 'main', 'def456', 'SUCCESS', now(), 'RESERVED')`,
			args: []any{repo.ID}, wantCode: checkViolation,
		},
		{
			name:     "same test class and method twice in a repository",
			sql:      `INSERT INTO test_case (repository_id, class_name, method_name) VALUES ($1, 'lab.CoreTest', 'adds')`,
			args:     []any{repo.ID},
			wantCode: uniqueViolation,
		},
		{
			name:     "two runs of one test in one build",
			sql:      `INSERT INTO test_run (build_id, test_case_id, outcome) VALUES ($1, $2, 'PASSED'), ($1, $2, 'FAILED')`,
			args:     []any{buildID, testCaseID},
			wantCode: uniqueViolation,
		},
		{
			name:     "unknown test outcome",
			sql:      `INSERT INTO test_run (build_id, test_case_id, outcome) VALUES ($1, $2, 'BROKEN')`,
			args:     []any{buildID, testCaseID},
			wantCode: checkViolation,
		},
		{
			name:     "unknown test stage",
			sql:      `INSERT INTO test_run (build_id, test_case_id, outcome, stage) VALUES ($1, $2, 'PASSED', 'NIGHTLY')`,
			args:     []any{buildID, testCaseID},
			wantCode: checkViolation,
		},
		{
			name:     "second active quarantine for one test",
			sql:      `INSERT INTO quarantine (test_case_id, state, reason_rule) VALUES ($1, 'QUARANTINED', 'R2')`,
			args:     []any{testCaseID},
			wantCode: uniqueViolation,
		},
		{
			name:     "released quarantine without released_at",
			sql:      `INSERT INTO quarantine (test_case_id, state, reason_rule) VALUES ($1, 'RELEASED', 'R2')`,
			args:     []any{testCaseID},
			wantCode: checkViolation,
		},
		{
			name:     "unknown alert type",
			sql:      `INSERT INTO alert (type, repository_id) VALUES ('SPAM', $1)`,
			args:     []any{repo.ID},
			wantCode: checkViolation,
		},
		{
			name:     "test run for a missing build",
			sql:      `INSERT INTO test_run (build_id, test_case_id, outcome) VALUES (999999, $1, 'PASSED')`,
			args:     []any{testCaseID},
			wantCode: fkViolation,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tc.sql, tc.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("Exec error = %v, want a Postgres error with code %s", err, tc.wantCode)
			}
			if pgErr.Code != tc.wantCode {
				t.Fatalf("SQLSTATE = %s (%s), want %s", pgErr.Code, pgErr.Message, tc.wantCode)
			}
		})
	}

	t.Run("released history plus one new active quarantine is allowed", func(t *testing.T) {
		_, err := pool.Exec(ctx, `
			UPDATE quarantine SET state = 'RELEASED', released_at = now() WHERE test_case_id = $1;
			`, testCaseID)
		if err != nil {
			t.Fatalf("release: %v", err)
		}
		_, err = pool.Exec(ctx, `
			INSERT INTO quarantine (test_case_id, state, reason_rule) VALUES ($1, 'QUARANTINED', 'R3')`, testCaseID)
		if err != nil {
			t.Fatalf("re-quarantine after release: %v", err)
		}
	})
}
