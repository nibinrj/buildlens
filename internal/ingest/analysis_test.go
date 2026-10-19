package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/quarantine"
	"github.com/nibinrj/buildlens/internal/regression"
	"github.com/nibinrj/buildlens/internal/surefire"
)

func TestBuildAnalysis(t *testing.T) {
	_, pool := newService(t)
	svc := NewServiceWithAnalysis(pool, quarantine.Defaults(), regression.Defaults(), "t3.small", "us-east-1", []string{"nibinrj/buildlens-lab"})
	ctx := context.Background()
	start := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	makeUpload := func(n int32, branch string, pr *int32, ms int64) Upload {
		m := meta("analysis/"+branch, n, "SUCCESS", stage("Test", ms))
		m.Branch = branch
		m.PRNumber = pr
		m.StartedAt = start.Add(time.Duration(n) * time.Minute)
		busy, duration := int64(3_600_000), int64(7_200_000)
		m.Agent.BusyMs = &busy
		m.DurationMs = &duration
		r := test("lab.SlowTest", "work", surefire.Passed)
		r.DurationMs = ms
		return Upload{Meta: m, Tests: []surefire.Result{r}}
	}
	storeUpload := func(u Upload) BuildView {
		t.Helper()
		result, err := svc.Ingest(ctx, u)
		if err != nil {
			t.Fatal(err)
		}
		b, err := svc.GetBuild(ctx, result.BuildID)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	analysis := func(b BuildView) Analysis {
		t.Helper()
		var a Analysis
		if err := json.Unmarshal(b.Analysis, &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	for n := int32(1); n <= 15; n++ {
		storeUpload(makeUpload(n, "main", nil, 100))
	}
	t.Run("spikes and sustained shift", func(t *testing.T) {
		for _, tc := range []struct {
			n      int32
			ms     int64
			alerts int
		}{{16, 1000, 0}, {17, 100, 0}, {18, 1000, 2}} {
			b := storeUpload(makeUpload(tc.n, "main", nil, tc.ms))
			if got := count(ctx, t, pool, "SELECT count(*) FROM alert WHERE build_id=$1 AND type='REGRESSION'", b.ID); got != tc.alerts {
				t.Fatalf("build %d alerts=%d want %d", tc.n, got, tc.alerts)
			}
			a := analysis(b)
			if a.Cost.USD == nil || *a.Cost.USD != "0.020900" || a.Cost.Label != "equivalent cost at reference price" {
				t.Fatalf("cost %+v", a.Cost)
			}
			if len(a.Regressions) != 2 || a.Regressions[0].MedianMs != 100 {
				t.Fatalf("baseline includes held-out slowdown: %+v", a.Regressions)
			}
			for _, e := range a.Regressions {
				if len(e.BaselineBuildIDs) != e.Samples || len(e.BaselineMs) != e.Samples || len(e.RecentBuildIDs) != len(e.RecentMs) {
					t.Fatal("incomplete baseline provenance")
				}
				for _, baselineID := range e.BaselineBuildIDs {
					for _, recentID := range e.RecentBuildIDs {
						if baselineID == recentID {
							t.Fatal("evaluation build leaked into baseline")
						}
					}
				}
			}
		}
	})
	t.Run("PR consecutive threshold and corrected upload", func(t *testing.T) {
		pr := int32(7)
		first := storeUpload(makeUpload(30, "PR-7", &pr, 121))
		a := analysis(first)
		if len(a.Regressions) != 2 || a.Regressions[0].Status != "NORMAL" {
			t.Fatalf("first PR %+v", a)
		}
		second := storeUpload(makeUpload(31, "PR-7", &pr, 121))
		a = analysis(second)
		if a.Regressions[0].Status != "REGRESSION" {
			t.Fatalf("second PR %+v", a)
		}
		corrected := storeUpload(makeUpload(31, "PR-7", &pr, 100))
		if corrected.ID != second.ID {
			t.Fatal("retry duplicated build")
		}
		if n := count(ctx, t, pool, "SELECT count(*) FROM alert WHERE build_id=$1 AND type='REGRESSION'", second.ID); n != 0 {
			t.Fatal("stale alerts retained")
		}
		if n := count(ctx, t, pool, "SELECT count(*) FROM alert WHERE build_id=$1 AND type='COST'", second.ID); n != 1 {
			t.Fatal("cost duplicated on retry")
		}
		var queued int64
		var version int
		if err := pool.QueryRow(ctx, "SELECT build_id,desired_version FROM pr_comment WHERE pr_number=7").Scan(&queued, &version); err != nil {
			t.Fatal(err)
		}
		if queued != second.ID || version != 3 {
			t.Fatalf("queue %d version%d", queued, version)
		}
		storeUpload(makeUpload(29, "PR-7", &pr, 900))
		if err := pool.QueryRow(ctx, "SELECT build_id,desired_version FROM pr_comment WHERE pr_number=7").Scan(&queued, &version); err != nil {
			t.Fatal(err)
		}
		if queued != second.ID || version != 3 {
			t.Fatal("late upload replaced newest PR")
		}
	})
	t.Run("large PR jump and top twenty tests", func(t *testing.T) {
		pr := int32(8)
		u := makeUpload(40, "PR-8", &pr, 1000)
		for i := 0; i < 25; i++ {
			r := test("lab.ManyTest", fmt.Sprint("test", i), surefire.Passed)
			r.DurationMs = int64(i)
			u.Tests = append(u.Tests, r)
		}
		a := analysis(storeUpload(u))
		tests := 0
		for _, e := range a.Regressions {
			if e.Kind == "TEST" {
				tests++
			}
			if e.Kind == "STAGE" && e.Status != "REGRESSION" {
				t.Fatalf("large jump %+v", e)
			}
		}
		if tests != 20 {
			t.Fatalf("tested %d metrics, want20", tests)
		}
	})
	t.Run("missing duration or rate stays unknown", func(t *testing.T) {
		for _, kind := range []string{"duration", "price", "historical"} {
			u := makeUpload(50, "cost-"+kind, nil, 100)
			switch kind {
			case "duration":
				u.Meta.Agent.BusyMs = nil
			case "price":
				u.Meta.Agent.Lifecycle = "SPOT"
			case "historical":
				u.Meta.StartedAt = time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
			}
			b := storeUpload(u)
			a := analysis(b)
			if a.Cost.USD != nil || a.Cost.Status != "UNKNOWN" {
				t.Fatalf("%s %+v", kind, a.Cost)
			}
			if n := count(ctx, t, pool, "SELECT count(*) FROM build WHERE id=$1 AND cost_usd IS NULL", b.ID); n != 1 {
				t.Fatal("unknown stored as zero")
			}
		}
	})
	t.Run("infrastructure excluded", func(t *testing.T) {
		u := makeUpload(60, "main", nil, 9000)
		u.LogTail = "OOMKilled=true"
		b := storeUpload(u)
		a := analysis(b)
		if !b.InfraFailure || a.RegressionState != "INELIGIBLE_BUILD" || len(a.Regressions) != 0 {
			t.Fatalf("%+v infra=%v", a, b.InfraFailure)
		}
	})
	t.Run("cost price snapshot survives later rate", func(t *testing.T) {
		before := storeUpload(makeUpload(70, "price-before", nil, 100))
		_, err := pool.Exec(ctx, `INSERT INTO price(instance_type,lifecycle,region,usd_per_hour,effective_from,source) VALUES('t3.small','LOCAL','us-east-1',0.04,$1,'reference: synthetic integration test')`, start.Add(71*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		after := analysis(storeUpload(makeUpload(72, "price-after", nil, 100)))
		old, err := svc.GetBuild(ctx, before.ID)
		if err != nil {
			t.Fatal(err)
		}
		if *analysis(old).Cost.USD != "0.020900" || after.Cost.USD == nil || *after.Cost.USD != "0.040000" {
			t.Fatal("incorrect price effective time or mutated snapshot")
		}
	})
}
