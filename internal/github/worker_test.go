package github

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/nibinrj/buildlens/internal/ingest"
	"github.com/nibinrj/buildlens/internal/regression"
	"github.com/nibinrj/buildlens/internal/store"
)

type fakeWork struct {
	claimErr, completeErr, failErr error
	completed, failed              int
	work                           store.PrComment
	retry                          time.Time
	reason                         string
}

func (f *fakeWork) Claim(ctx context.Context, token string) (store.PrComment, error) {
	f.work.LeaseToken = &token
	return f.work, f.claimErr
}
func (f *fakeWork) Complete(ctx context.Context, w store.PrComment, id *int64) error {
	f.completed++
	return f.completeErr
}
func (f *fakeWork) Fail(ctx context.Context, w store.PrComment, retry time.Time, reason string) error {
	f.failed++
	f.retry = retry
	f.reason = reason
	return f.failErr
}

type fakeSource struct{ err error }

func (f fakeSource) Load(ctx context.Context, w store.PrComment) (Summary, error) {
	return Summary{Repository: "owner/lab", CommitSHA: "abc", Body: Marker + "\nbody"}, f.err
}

type fakePublisher struct {
	err   error
	calls int
}

func (f *fakePublisher) Sync(ctx context.Context, repo string, pr int, sha, body string) (*int64, error) {
	f.calls++
	id := int64(91)
	return &id, f.err
}

func TestWorker(t *testing.T) {
	for _, tc := range []struct {
		name                                   string
		claim, source, publish, complete, fail error
		did, wantErr                           bool
		completed, failed, calls               int
	}{
		{name: "success", did: true, completed: 1, calls: 1},
		{name: "empty", claim: pgx.ErrNoRows},
		{name: "claim failure", claim: errors.New("db"), wantErr: true},
		{name: "source failure", source: errors.New("db"), did: true, failed: 1},
		{name: "403", publish: &HTTPError{Status: 403}, did: true, failed: 1, calls: 1},
		{name: "5xx", publish: &HTTPError{Status: 500}, did: true, failed: 1, calls: 1},
		{name: "ack failure", complete: errors.New("db"), did: true, wantErr: true, completed: 1, calls: 1},
		{name: "retry write failure", publish: errors.New("network"), fail: errors.New("db"), did: true, wantErr: true, failed: 1, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &fakeWork{claimErr: tc.claim, completeErr: tc.complete, failErr: tc.fail, work: store.PrComment{PrNumber: 7, DesiredVersion: 1}}
			p := &fakePublisher{err: tc.publish}
			w := NewWorker(s, fakeSource{tc.source}, p, slog.New(slog.NewTextHandler(io.Discard, nil)))
			did, err := w.ProcessOne(context.Background())
			if did != tc.did || (err != nil) != tc.wantErr || s.completed != tc.completed || s.failed != tc.failed || p.calls != tc.calls {
				t.Fatalf("did=%v err=%v complete=%d fail=%d calls=%d", did, err, s.completed, s.failed, p.calls)
			}
			if s.failed > 0 && s.retry.Before(time.Now()) {
				t.Fatal("retry is not deferred")
			}
		})
	}
	t.Run("bounded wake and shutdown", func(t *testing.T) {
		w := NewWorker(&fakeWork{claimErr: pgx.ErrNoRows}, fakeSource{}, &fakePublisher{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		for range 1000 {
			w.Notify()
		}
		if len(w.wake) != 1 {
			t.Fatal("wake not coalesced")
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { w.Run(ctx); close(done) }()
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("worker did not stop")
		}
	})
	t.Run("rate limit retry time", func(t *testing.T) {
		s := &fakeWork{}
		retry := time.Now().Add(time.Hour)
		w := NewWorker(s, fakeSource{}, &fakePublisher{err: &HTTPError{Status: 429, RetryAt: retry}}, slog.New(slog.NewTextHandler(io.Discard, nil)))
		if _, err := w.ProcessOne(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !s.retry.Equal(retry) {
			t.Fatalf("%v != %v", s.retry, retry)
		}
	})
}

func TestRender(t *testing.T) {
	amount := "0.020900"
	b := store.GetBuildWithRepositoryRow{Build: store.Build{BuildNumber: 2, Result: "SUCCESS", CommitSha: "abc"}}
	a := ingest.Analysis{Cost: ingest.CostView{USD: &amount, Label: "equivalent cost at reference price"}, RegressionState: "EVALUATED", Regressions: []regression.Evidence{{Kind: "STAGE", Name: "<script>|@all`\n", Status: "REGRESSION", Samples: 10}}}
	body := Render(b, a, []store.ListActiveQuarantineRow{{ClassName: "<img>@all", MethodName: "x"}})
	for _, want := range []string{Marker, "### Quarantined tests", "### Regressions", "### Cost", "REGRESSION", "equivalent cost", "&#64;", "&#124;"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "@all") {
		t.Fatal("uploaded markup/mention not escaped")
	}
	for _, state := range []string{"INELIGIBLE_BUILD", "EVALUATED"} {
		body := Render(b, ingest.Analysis{RegressionState: state}, nil)
		if !strings.Contains(body, "Unknown:") || !strings.Contains(body, "No active quarantines") {
			t.Fatal(body)
		}
	}
	t.Run("large Unicode summary is bounded", func(t *testing.T) {
		a.Cost.Source = strings.Repeat("<&界", 400)
		a.Cost.InstanceType = a.Cost.Source
		a.Cost.Region = a.Cost.Source
		a.Regressions = nil
		for range 1000 {
			a.Regressions = append(a.Regressions, regression.Evidence{Kind: "TEST", Name: strings.Repeat("<&界", 300), Status: "INSUFFICIENT_DATA"})
		}
		body := Render(b, a, nil)
		if len(body) > 60_000 || !utf8.ValidString(body) || !strings.HasPrefix(body, Marker+"\n") {
			t.Fatalf("invalid bounded summary: %d bytes", len(body))
		}
	})
}
