package quarantine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nibinrj/buildlens/internal/rules"
	"github.com/nibinrj/buildlens/internal/store"
)

var (
	ErrNotFound = errors.New("test or active quarantine not found")
	ErrConflict = errors.New("test already quarantined")
	ErrCap      = errors.New("quarantine safety cap reached")
)

// Entry is the authenticated API's active row, including audit evidence.
type Entry struct {
	ID                int64           `json:"id"`
	TestCaseID        int64           `json:"test_case_id"`
	Module            string          `json:"module"`
	ClassName         string          `json:"class_name"`
	MethodName        string          `json:"method_name"`
	ReasonRule        string          `json:"reason_rule"`
	Evidence          json.RawMessage `json:"evidence"`
	QuarantinedAt     time.Time       `json:"quarantined_at"`
	ConsecutivePasses int32           `json:"consecutive_passes"`
	Manual            bool            `json:"manual"`
}
type Request struct {
	Repo       string `json:"repo"`
	ClassName  string `json:"class_name"`
	MethodName string `json:"method_name"`
	Reason     string `json:"reason"`
}
type Service struct {
	pool *pgxpool.Pool
	cfg  Config
}

func NewService(pool *pgxpool.Pool, cfg Config) *Service { return &Service{pool, cfg} }

func (s *Service) List(ctx context.Context, repo string) ([]Entry, error) {
	q := store.New(s.pool)
	r, err := q.GetRepositoryByName(ctx, repo)
	if errors.Is(err, pgx.ErrNoRows) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find repository: %w", err)
	}
	rows, err := q.ListActiveQuarantine(ctx, r.ID)
	if err != nil {
		return nil, fmt.Errorf("list quarantines: %w", err)
	}
	entries := make([]Entry, 0, len(rows))
	for _, row := range rows {
		entries = append(entries, Entry{
			ID: row.ID, TestCaseID: row.TestCaseID, Module: row.Module, ClassName: row.ClassName,
			MethodName: row.MethodName, ReasonRule: row.ReasonRule, Evidence: row.Evidence,
			QuarantinedAt: row.QuarantinedAt, ConsecutivePasses: row.ConsecutivePasses, Manual: row.Manual,
		})
	}
	return entries, nil
}

// CreateManual serializes with ingest by locking the same repository row.
func (s *Service) CreateManual(ctx context.Context, request Request) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin manual quarantine: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	q := store.New(tx)
	repo, err := q.LockRepository(ctx, request.Repo)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("lock repository: %w", err)
	}
	tc, err := q.FindCaseByName(ctx, store.FindCaseByNameParams{RepositoryID: repo.ID, ClassName: request.ClassName, MethodName: request.MethodName})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find test: %w", err)
	}
	_, err = q.GetActiveForTest(ctx, tc.ID)
	if err == nil {
		return 0, ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("find active quarantine: %w", err)
	}
	full, _, err := atCap(ctx, q, repo.ID, s.cfg)
	if err != nil {
		return 0, err
	}
	if full {
		return 0, ErrCap
	}
	after, err := q.RepositoryBuildWatermark(ctx, repo.ID)
	if err != nil {
		return 0, fmt.Errorf("read activation watermark: %w", err)
	}
	evidence, err := json.Marshal(map[string]any{"manual_reason": request.Reason, "after_build_id": after})
	if err != nil {
		return 0, fmt.Errorf("encode manual evidence: %w", err)
	}
	row, err := q.InsertQuarantine(ctx, store.InsertQuarantineParams{TestCaseID: tc.ID, ReasonRule: "MANUAL", Evidence: evidence, Manual: true})
	if err != nil {
		return 0, fmt.Errorf("create manual quarantine: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit manual quarantine: %w", err)
	}
	return row.ID, nil
}

func (s *Service) ReleaseManual(ctx context.Context, id int64, reason string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin manual release: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	q := store.New(tx)
	repo, err := q.GetQuarantineRepository(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("find quarantine repository: %w", err)
	}
	if _, err := q.LockRepository(ctx, repo); err != nil {
		return fmt.Errorf("lock release repository: %w", err)
	}
	count, err := q.ManualRelease(ctx, store.ManualReleaseParams{ID: id, Reason: reason})
	if err != nil {
		return fmt.Errorf("release manually: %w", err)
	}
	if count == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit manual release: %w", err)
	}
	return nil
}

// Apply processes the already persisted upload in its existing, repository-locked transaction.
func Apply(ctx context.Context, q *store.Queries, repo store.Repository, buildID int64, cfg Config) error {
	active, err := q.ListActiveQuarantine(ctx, repo.ID)
	if err != nil {
		return fmt.Errorf("list active release candidates: %w", err)
	}
	for _, row := range active {
		var audit struct {
			AfterBuildID int64 `json:"after_build_id"`
		}
		if err := json.Unmarshal(row.Evidence, &audit); err != nil {
			return fmt.Errorf("decode quarantine audit: %w", err)
		}
		outcomes, err := q.ListReleaseOutcomes(ctx, store.ListReleaseOutcomesParams{TestCaseID: row.TestCaseID, AfterBuildID: audit.AfterBuildID})
		if err != nil {
			return fmt.Errorf("list quarantine history: %w", err)
		}
		// Capping keeps conversion to SQL integer bounded without losing the release decision.
		passes := min(ConsecutivePasses(outcomes), cfg.ReleasePasses)
		if err := q.SetReleaseProgress(ctx, store.SetReleaseProgressParams{ID: row.ID, Passes: int32(passes), Release: passes >= cfg.ReleasePasses}); err != nil { //nolint:gosec // config validated to int32
			return fmt.Errorf("set release progress: %w", err)
		}
	}
	b, err := q.GetBuild(ctx, buildID)
	if err != nil {
		return fmt.Errorf("get classified build: %w", err)
	}
	if b.InfraFailure {
		return nil
	}
	cases, err := q.ListBuildCases(ctx, buildID)
	if err != nil {
		return fmt.Errorf("list detection candidates: %w", err)
	}
	for _, tc := range cases {
		if tc.Stage != "BLOCKING" {
			continue
		}
		_, err := q.GetActiveForTest(ctx, tc.TestCaseID)
		if err == nil {
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check active test: %w", err)
		}
		rows, err := q.ListDetectionHistory(ctx, store.ListDetectionHistoryParams{TestCaseID: tc.TestCaseID,
			RunWindow: int32(max(cfg.Rules.R1Window, cfg.Rules.R2Window)), MainWindow: int32(cfg.Rules.R3Window), DefaultBranch: repo.DefaultBranch}) //nolint:gosec // validated int32 windows
		if err != nil {
			return fmt.Errorf("list detection history: %w", err)
		}
		history := make([]rules.Run, 0, len(rows))
		for _, row := range rows {
			history = append(history, rules.Run{BuildID: row.BuildID, Outcome: row.Outcome,
				Branch: row.Branch, TestedTreeSHA: row.TestedTreeSha, InfraFailure: row.InfraFailure,
				StartedAt: row.StartedAt, FailureHash: row.FailureHash})
		}
		findings := rules.Evaluate(history, repo.DefaultBranch, cfg.Rules)
		if len(findings) == 0 {
			continue
		}
		after, err := q.RepositoryBuildWatermark(ctx, repo.ID)
		if err != nil {
			return fmt.Errorf("read activation watermark: %w", err)
		}
		full, capLimit, err := atCap(ctx, q, repo.ID, cfg)
		if err != nil {
			return err
		}
		evidence, err := json.Marshal(struct {
			TestCaseID   int64           `json:"test_case_id"`
			Findings     []rules.Finding `json:"findings"`
			Cap          int             `json:"cap"`
			CapReached   bool            `json:"cap_reached"`
			AfterBuildID int64           `json:"after_build_id"`
		}{tc.TestCaseID, findings, capLimit, full, after})
		if err != nil {
			return fmt.Errorf("encode detection evidence: %w", err)
		}
		if full {
			err = q.InsertFlakyAlert(ctx, store.InsertFlakyAlertParams{RepositoryID: repo.ID, BuildID: &buildID, Payload: evidence, TestCaseID: strconv.FormatInt(tc.TestCaseID, 10)})
			if err != nil {
				return fmt.Errorf("write cap alert: %w", err)
			}
			continue
		}
		if _, err := q.InsertQuarantine(ctx, store.InsertQuarantineParams{TestCaseID: tc.TestCaseID, ReasonRule: findings[0].Rule, Evidence: evidence}); err != nil {
			return fmt.Errorf("create automatic quarantine: %w", err)
		}
	}
	return nil
}

func atCap(ctx context.Context, q *store.Queries, repoID int64, cfg Config) (bool, int, error) {
	seen, err := q.CountRecentTests(ctx, store.CountRecentTestsParams{RepositoryID: repoID, BuildWindow: int32(cfg.CapBuildWindow)}) //nolint:gosec // validated positive int32
	if err != nil {
		return false, 0, fmt.Errorf("count recently seen tests: %w", err)
	}
	count, err := q.CountActiveQuarantines(ctx, repoID)
	if err != nil {
		return false, 0, fmt.Errorf("count active quarantines: %w", err)
	}
	limit := Cap(int(seen), cfg)
	return count >= int64(limit), limit, nil
}
