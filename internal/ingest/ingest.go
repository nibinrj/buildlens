// Package ingest stores an uploaded build. Uploading the same job + build number again replaces it.
package ingest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nibinrj/buildlens/internal/store"
	"github.com/nibinrj/buildlens/internal/surefire"
	"github.com/nibinrj/buildlens/internal/upload"
)

// ErrNotFound is returned by GetBuild for an unknown build id.
var ErrNotFound = errors.New("build not found")

// Upload is one build as received: its metadata and every parsed test result.
type Upload struct {
	Meta  upload.Metadata
	Tests []surefire.Result
}

// Result tells the caller what was stored.
type Result struct {
	BuildID int64 `json:"id"`
	Created bool  `json:"created"` // false when an existing build was replaced
	Tests   int   `json:"tests"`   // test runs stored, after merging duplicates
	Stages  int   `json:"stages"`
}

// Service writes and reads builds.
type Service struct {
	pool *pgxpool.Pool
}

// NewService returns a Service using pool.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// Ingest stores u in one transaction: the repository and build rows are upserted, and the build's stage runs
// and test runs are replaced. Nothing is stored if any step fails.
func (s *Service) Ingest(ctx context.Context, u Upload) (Result, error) {
	tests := surefire.Merge(u.Tests) // one row per test per build

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("begin: %w", err)
	}
	// Rollback after Commit is a no-op, so this undoes everything on any early return, like an exception
	// leaving a @Transactional method in Spring.
	defer tx.Rollback(ctx) //nolint:errcheck // the error after a successful commit is expected and meaningless

	q := store.New(tx)

	defaultBranch := u.Meta.DefaultBranch
	if defaultBranch == "" {
		defaultBranch = "main"
	}
	repoID, err := q.EnsureRepository(ctx, store.EnsureRepositoryParams{Name: u.Meta.Repo, DefaultBranch: defaultBranch})
	if err != nil {
		return Result{}, fmt.Errorf("ensure repository: %w", err)
	}

	b, err := q.UpsertBuild(ctx, buildParams(repoID, u.Meta))
	if err != nil {
		return Result{}, fmt.Errorf("upsert build: %w", err)
	}

	if err := q.DeleteStageRuns(ctx, b.ID); err != nil {
		return Result{}, fmt.Errorf("delete old stage runs: %w", err)
	}
	if err := q.DeleteTestRuns(ctx, b.ID); err != nil {
		return Result{}, fmt.Errorf("delete old test runs: %w", err)
	}

	if len(u.Meta.Stages) > 0 {
		rows := make([]store.InsertStageRunsParams, 0, len(u.Meta.Stages))
		for _, st := range u.Meta.Stages {
			rows = append(rows, store.InsertStageRunsParams{
				BuildID: b.ID, Name: st.Name, StartedAt: st.StartedAt.UTC(), DurationMs: st.DurationMs, Result: st.Result,
			})
		}
		if _, err := q.InsertStageRuns(ctx, rows); err != nil {
			return Result{}, fmt.Errorf("insert stage runs: %w", err)
		}
	}

	if len(tests) > 0 {
		if err := insertTests(ctx, q, repoID, b.ID, tests); err != nil {
			return Result{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit: %w", err)
	}
	return Result{BuildID: b.ID, Created: b.Inserted, Tests: len(tests), Stages: len(u.Meta.Stages)}, nil
}

func buildParams(repoID int64, m upload.Metadata) store.UpsertBuildParams {
	p := store.UpsertBuildParams{
		RepositoryID:      repoID,
		JobName:           m.Job,
		BuildNumber:       m.BuildNumber,
		Branch:            m.Branch,
		PrNumber:          m.PRNumber,
		CommitSha:         m.CommitSHA,
		TestedTreeSha:     nonEmpty(m.TestedTreeSHA),
		Result:            m.Result,
		StartedAt:         m.StartedAt.UTC(),
		DurationMs:        m.DurationMs,
		AgentName:         nonEmpty(m.Agent.Name),
		AgentInstanceType: nonEmpty(m.Agent.InstanceType),
		AgentLifecycle:    nonEmpty(m.Agent.Lifecycle),
	}
	if m.FinishedAt != nil {
		t := m.FinishedAt.UTC()
		p.FinishedAt = &t
	}
	return p
}

// insertTests upserts every test case in one statement, then copies the runs in.
func insertTests(ctx context.Context, q *store.Queries, repoID, buildID int64, tests []surefire.Result) error {
	params := store.UpsertTestCasesParams{
		RepositoryID: repoID,
		Modules:      make([]string, len(tests)),
		ClassNames:   make([]string, len(tests)),
		MethodNames:  make([]string, len(tests)),
	}
	for i, t := range tests {
		params.Modules[i], params.ClassNames[i], params.MethodNames[i] = t.Module, t.ClassName, t.MethodName
	}
	cases, err := q.UpsertTestCases(ctx, params)
	if err != nil {
		return fmt.Errorf("upsert test cases: %w", err)
	}

	type key struct{ class, method string }
	ids := make(map[key]int64, len(cases))
	for _, c := range cases {
		ids[key{c.ClassName, c.MethodName}] = c.ID
	}

	runs := make([]store.InsertTestRunsParams, 0, len(tests))
	for _, t := range tests {
		id, ok := ids[key{t.ClassName, t.MethodName}]
		if !ok {
			return fmt.Errorf("test case %s#%s missing after upsert", t.ClassName, t.MethodName)
		}
		runs = append(runs, store.InsertTestRunsParams{
			BuildID:       buildID,
			TestCaseID:    id,
			Outcome:       string(t.Outcome),
			DurationMs:    t.DurationMs,
			RerunFailures: int32(min(t.RerunFailures, 1<<30)), //nolint:gosec // capped above, cannot overflow
			FailureType:   nonEmpty(t.FailureType),
			FailureHash:   nonEmpty(t.FailureHash),
			Stage:         "BLOCKING", // the quarantine stage arrives in P3
		})
	}
	if _, err := q.InsertTestRuns(ctx, runs); err != nil {
		return fmt.Errorf("insert test runs: %w", err)
	}
	return nil
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// BuildView is GET /api/v1/builds/{id}: a build with its stages and tests, for debugging.
type BuildView struct {
	ID            int64          `json:"id"`
	Repository    string         `json:"repository"`
	Job           string         `json:"job"`
	BuildNumber   int32          `json:"build_number"`
	Branch        string         `json:"branch"`
	PRNumber      *int32         `json:"pr_number"`
	CommitSHA     string         `json:"commit_sha"`
	TestedTreeSHA *string        `json:"tested_tree_sha"`
	Result        string         `json:"result"`
	StartedAt     time.Time      `json:"started_at"`
	FinishedAt    *time.Time     `json:"finished_at"`
	DurationMs    *int64         `json:"duration_ms"`
	Agent         AgentView      `json:"agent"`
	InfraFailure  bool           `json:"infra_failure"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Stages        []StageView    `json:"stages"`
	TestSummary   map[string]int `json:"test_summary"` // count per outcome
	Tests         []TestView     `json:"tests"`
}

// AgentView is where the build ran.
type AgentView struct {
	Name         *string `json:"name"`
	InstanceType *string `json:"instance_type"`
	Lifecycle    *string `json:"lifecycle"`
}

// StageView is one timed stage.
type StageView struct {
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Result     string    `json:"result"`
}

// TestView is one test run.
type TestView struct {
	Module        string  `json:"module"`
	ClassName     string  `json:"class_name"`
	MethodName    string  `json:"method_name"`
	Outcome       string  `json:"outcome"`
	DurationMs    int64   `json:"duration_ms"`
	RerunFailures int32   `json:"rerun_failures"`
	FailureType   *string `json:"failure_type"`
	FailureHash   *string `json:"failure_hash"`
	Stage         string  `json:"stage"`
}

// GetBuild returns one build with its stages and tests, or ErrNotFound.
func (s *Service) GetBuild(ctx context.Context, id int64) (BuildView, error) {
	q := store.New(s.pool)

	row, err := q.GetBuildWithRepository(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return BuildView{}, ErrNotFound
	}
	if err != nil {
		return BuildView{}, fmt.Errorf("get build %d: %w", id, err)
	}
	b := row.Build

	stages, err := q.ListStageRuns(ctx, id)
	if err != nil {
		return BuildView{}, fmt.Errorf("list stage runs: %w", err)
	}
	tests, err := q.ListTestRuns(ctx, id)
	if err != nil {
		return BuildView{}, fmt.Errorf("list test runs: %w", err)
	}

	v := BuildView{
		ID: b.ID, Repository: row.RepositoryName, Job: b.JobName, BuildNumber: b.BuildNumber, Branch: b.Branch,
		PRNumber: b.PrNumber, CommitSHA: b.CommitSha, TestedTreeSHA: b.TestedTreeSha, Result: b.Result,
		StartedAt: b.StartedAt, FinishedAt: b.FinishedAt, DurationMs: b.DurationMs,
		Agent:        AgentView{Name: b.AgentName, InstanceType: b.AgentInstanceType, Lifecycle: b.AgentLifecycle},
		InfraFailure: b.InfraFailure, CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt,
		Stages:      make([]StageView, 0, len(stages)),
		TestSummary: map[string]int{},
		Tests:       make([]TestView, 0, len(tests)),
	}
	for _, st := range stages {
		v.Stages = append(v.Stages, StageView{Name: st.Name, StartedAt: st.StartedAt, DurationMs: st.DurationMs, Result: st.Result})
	}
	for _, t := range tests {
		v.TestSummary[t.Outcome]++
		v.Tests = append(v.Tests, TestView{
			Module: t.Module, ClassName: t.ClassName, MethodName: t.MethodName, Outcome: t.Outcome,
			DurationMs: t.DurationMs, RerunFailures: t.RerunFailures, FailureType: t.FailureType,
			FailureHash: t.FailureHash, Stage: t.Stage,
		})
	}
	return v, nil
}
