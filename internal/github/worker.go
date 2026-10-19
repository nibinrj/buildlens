package github

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nibinrj/buildlens/internal/store"
)

// WorkStore is the durable outbox boundary; tests can simulate storage failures.
type WorkStore interface {
	Claim(context.Context, string) (store.PrComment, error)
	Complete(context.Context, store.PrComment, *int64) error
	Fail(context.Context, store.PrComment, time.Time, string) error
}
type Publisher interface {
	Sync(context.Context, string, int, string, string) (*int64, error)
}

// Worker has one publisher goroutine and a one-slot wake channel. Notifications
// coalesce; pending rows in Postgres survive a full channel and process restart.
type Worker struct {
	store     WorkStore
	source    Source
	publisher Publisher
	logger    *slog.Logger
	wake      chan struct{}
}

func NewWorker(s WorkStore, source Source, publisher Publisher, logger *slog.Logger) *Worker {
	return &Worker{store: s, source: source, publisher: publisher, logger: logger, wake: make(chan struct{}, 1)}
}

func (w *Worker) Notify() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		for range 16 {
			if ctx.Err() != nil {
				return
			}
			did, err := w.ProcessOne(ctx)
			if err != nil {
				w.logger.ErrorContext(ctx, "comment worker storage failure", "err", err)
				break
			}
			if !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
		case <-ticker.C:
		}
	}
}

// ProcessOne is also used for integration verification. Remote failures are
// recorded for retry; they never propagate into the separate ingest transaction.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return false, fmt.Errorf("lease token: %w", err)
	}
	workCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	job, err := w.store.Claim(workCtx, hex.EncodeToString(tokenBytes))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("claim comment: %w", err)
	}
	summary, err := w.source.Load(workCtx, job)
	var commentID *int64
	if err == nil && !summary.Skip {
		commentID, err = w.publisher.Sync(workCtx, summary.Repository, int(job.PrNumber), summary.CommitSHA, summary.Body)
	}
	saveCtx, saveCancel := context.WithTimeout(ctx, 5*time.Second)
	defer saveCancel()
	if err != nil {
		reason := "comment preparation or transport failed"
		retry := time.Now().Add(time.Duration(1<<min(job.Attempts, 7)) * 30 * time.Second)
		var remote *HTTPError
		if errors.As(err, &remote) {
			reason = remote.Error()
			if remote.RetryAt.After(retry) {
				retry = remote.RetryAt
			}
		}
		w.logger.WarnContext(ctx, "PR comment deferred", "repository_id", job.RepositoryID, "pr", job.PrNumber, "reason", reason)
		if err := w.store.Fail(saveCtx, job, retry, reason); err != nil {
			return true, fmt.Errorf("record comment failure: %w", err)
		}
		return true, nil
	}
	if commentID == nil {
		commentID = job.CommentID
	}
	if err := w.store.Complete(saveCtx, job, commentID); err != nil {
		return true, fmt.Errorf("complete comment: %w", err)
	}
	w.logger.InfoContext(ctx, "PR comment work completed", "repository_id", job.RepositoryID, "pr", job.PrNumber, "version", job.DesiredVersion, "comment_id", commentID)
	return true, nil
}

type DatabaseStore struct{ Pool *pgxpool.Pool }

func (s DatabaseStore) Claim(ctx context.Context, token string) (store.PrComment, error) {
	return store.New(s.Pool).ClaimPRComment(ctx, &token)
}
func (s DatabaseStore) Fail(ctx context.Context, work store.PrComment, retry time.Time, reason string) error {
	n, err := store.New(s.Pool).FailPRComment(ctx, store.FailPRCommentParams{Version: work.DesiredVersion, LastError: &reason, RetryAt: retry, RepositoryID: work.RepositoryID, PrNumber: work.PrNumber, LeaseToken: work.LeaseToken})
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("comment lease was lost")
	}
	return nil
}
func (s DatabaseStore) Complete(ctx context.Context, work store.PrComment, id *int64) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	q := store.New(tx)
	n, err := q.CompletePRComment(ctx, store.CompletePRCommentParams{CommentID: id, SentVersion: work.DesiredVersion, RepositoryID: work.RepositoryID, PrNumber: work.PrNumber, LeaseToken: work.LeaseToken})
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("comment lease was lost")
	}
	if id != nil {
		if err := q.SetAlertCommentID(ctx, store.SetAlertCommentIDParams{BuildID: &work.BuildID, PrCommentID: id}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
