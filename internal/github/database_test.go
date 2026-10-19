package github

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nibinrj/buildlens/internal/db"
	"github.com/nibinrj/buildlens/internal/dbtest"
	"github.com/nibinrj/buildlens/internal/ingest"
	"github.com/nibinrj/buildlens/internal/quarantine"
	"github.com/nibinrj/buildlens/internal/regression"
	"github.com/nibinrj/buildlens/internal/store"
	"github.com/nibinrj/buildlens/internal/surefire"
	"github.com/nibinrj/buildlens/internal/upload"
)

func commentDB(t *testing.T) (*pgxpool.Pool, *ingest.Service) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := db.Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatal(err)
	}
	return pool, ingest.NewServiceWithAnalysis(pool, quarantine.Defaults(), regression.Defaults(), "t3.small", "us-east-1", []string{"owner/lab"})
}

func prUpload(n int32) ingest.Upload {
	pr := int32(7)
	ms := int64(1000)
	return ingest.Upload{Meta: upload.Metadata{Repo: "owner/lab", Job: "lab/PR-7", BuildNumber: n, Branch: "PR-7", PRNumber: &pr, CommitSHA: strings.Repeat("a", 40), Result: "SUCCESS", StartedAt: time.Date(2026, 10, 16, 10, int(n), 0, 0, time.UTC), DurationMs: &ms, Agent: upload.Agent{Lifecycle: "LOCAL", BusyMs: &ms}}, Tests: []surefire.Result{{ClassName: "lab.Test", MethodName: "x", Outcome: surefire.Passed}}}
}

func TestDurableCommentOutbox(t *testing.T) {
	pool, svc := commentDB(t)
	ctx := context.Background()
	s := DatabaseStore{pool}
	if _, err := svc.Ingest(ctx, prUpload(1)); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	claims := make(chan store.PrComment, 2)
	errs := make(chan error, 2)
	for _, token := range []string{"one", "two"} {
		workers.Go(func() {
			job, err := s.Claim(ctx, token)
			if err != nil {
				errs <- err
			} else {
				claims <- job
			}
		})
	}
	workers.Wait()
	close(claims)
	close(errs)
	if len(claims) != 1 || len(errs) != 1 {
		t.Fatalf("claims=%d errors=%d", len(claims), len(errs))
	}
	if err := <-errs; !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	first := <-claims
	if _, err := svc.Ingest(ctx, prUpload(2)); err != nil {
		t.Fatal(err)
	}
	id := int64(91)
	if err := s.Complete(ctx, first, &id); err != nil {
		t.Fatal(err)
	}
	second, err := s.Claim(ctx, "new-token")
	if err != nil {
		t.Fatal(err)
	}
	if second.BuildID == first.BuildID || second.DesiredVersion != 2 || second.SentVersion != 1 {
		t.Fatalf("%+v", second)
	}
	if err := s.Complete(ctx, first, &id); err == nil {
		t.Fatal("stale lease completion accepted")
	}
	// New work arriving during an ordinary 5xx failure need not inherit its retry delay.
	if _, err := svc.Ingest(ctx, prUpload(3)); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(ctx, second, time.Now().Add(time.Hour), "GitHub HTTP 500"); err != nil {
		t.Fatal(err)
	}
	third, err := s.Claim(ctx, "third-token")
	if err != nil {
		t.Fatal(err)
	}
	if third.DesiredVersion != 3 || third.Attempts != 0 {
		t.Fatalf("%+v", third)
	}
	// Simulate a crashed process: unexpired leases exclude other workers; expired
	// leases are reclaimed and the old process cannot acknowledge the new lease.
	if _, err := s.Claim(ctx, "competing"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("%v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE pr_comment SET lease_until=now()-interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Claim(ctx, "after-restart")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Complete(ctx, third, &id); err == nil {
		t.Fatal("expired lease accepted")
	}
	if err := s.Complete(ctx, recovered, &id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(ctx, "done"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("sent row remained pending: %v", err)
	}
	// Repeated uploads must not bypass Retry-After / permission cooldowns.
	if _, err := svc.Ingest(ctx, prUpload(4)); err != nil {
		t.Fatal(err)
	}
	rateWork, err := s.Claim(ctx, "rate-limited")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ingest(ctx, prUpload(5)); err != nil {
		t.Fatal(err)
	}
	if err := s.Fail(ctx, rateWork, time.Now().Add(time.Hour), "GitHub HTTP 429"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int32{6, 7} {
		if _, err := svc.Ingest(ctx, prUpload(n)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Claim(ctx, "must-cool-down"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("new uploads bypassed cooldown: %v", err)
	}
	var linked int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM alert WHERE build_id=$1 AND pr_comment_id=91", recovered.BuildID).Scan(&linked); err != nil || linked != 1 {
		t.Fatalf("alert linkage=%d %v", linked, err)
	}
}

func TestWorkerDatabaseFailureIsolation(t *testing.T) {
	pool, svc := commentDB(t)
	ctx := context.Background()
	result, err := svc.Ingest(ctx, prUpload(1))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	publisher := &fakePublisher{err: &HTTPError{Status: 403}}
	w := NewWorker(DatabaseStore{pool}, DatabaseSource{pool}, publisher, logger)
	if did, err := w.ProcessOne(ctx); err != nil || !did {
		t.Fatalf("%v %v", did, err)
	}
	if _, err := svc.GetBuild(ctx, result.BuildID); err != nil {
		t.Fatalf("remote failure lost ingest: %v", err)
	}
	var attempts int
	var reason string
	if err := pool.QueryRow(ctx, "SELECT attempts,last_error FROM pr_comment").Scan(&attempts, &reason); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 || reason != "GitHub HTTP 403" {
		t.Fatalf("attempts=%d reason=%s", attempts, reason)
	}
	// A later successful retry uses durable state and renders all three sections.
	if _, err := pool.Exec(ctx, "UPDATE pr_comment SET next_attempt_at=now()"); err != nil {
		t.Fatal(err)
	}
	publisher.err = nil
	if did, err := w.ProcessOne(ctx); err != nil || !did {
		t.Fatalf("%v %v", did, err)
	}
	if publisher.calls != 2 {
		t.Fatal("no retry")
	}
}
