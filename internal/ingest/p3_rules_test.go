package ingest

import (
	"fmt"
	"testing"
	"time"

	"github.com/nibinrj/buildlens/internal/quarantine"
	"github.com/nibinrj/buildlens/internal/surefire"
)

func TestP3RulesWithSQLWindows(t *testing.T) {
	_, pool := newService(t)
	cfg := quarantine.Defaults()
	cfg.Rules.R1Window = 3
	cfg.Rules.R2Window = 3
	cfg.Rules.R3Window = 4
	svc := NewServiceWithPolicy(pool, cfg)
	qs := quarantine.NewService(pool, cfg)
	type observation struct {
		branch    string
		outcome   surefire.Outcome
		tree, log string
	}
	obs := func(outcome surefire.Outcome, n int) observation {
		return observation{"trunk", outcome, fmt.Sprintf("%040x", n), ""}
	}
	table := []struct {
		name    string
		history []observation
		rule    string
	}{
		{"R2", []observation{obs(surefire.Failed, 1), obs(surefire.Passed, 1)}, "R2"},
		{"genuine-break-fix", []observation{obs(surefire.Passed, 1), obs(surefire.Failed, 2), obs(surefire.Failed, 3), obs(surefire.Passed, 4)}, ""},
		{"R3-despite-PR-flood", []observation{
			obs(surefire.Passed, 1), obs(surefire.Failed, 2), obs(surefire.Passed, 3),
			{"feature", surefire.Passed, fmt.Sprintf("%040x", 4), ""},
			{"feature", surefire.Passed, fmt.Sprintf("%040x", 5), ""},
			{"feature", surefire.Passed, fmt.Sprintf("%040x", 6), ""},
			{"feature", surefire.Passed, fmt.Sprintf("%040x", 7), ""},
			obs(surefire.Error, 8)}, "R3"},
		{"infra-only-failures", []observation{
			obs(surefire.Passed, 1), {"trunk", surefire.Failed, fmt.Sprintf("%040x", 1), "ERROR: exit code 137"},
			{"trunk", surefire.Flaky, fmt.Sprintf("%040x", 1), "ERROR: exit code 137"},
			{"trunk", surefire.Flaky, fmt.Sprintf("%040x", 1), "ERROR: exit code 137"}, obs(surefire.Passed, 1)}, ""},
		{"infra-before-window", []observation{
			obs(surefire.Flaky, 1), {"trunk", surefire.Flaky, fmt.Sprintf("%040x", 2), "ERROR: exit code 137"},
			{"trunk", surefire.Flaky, fmt.Sprintf("%040x", 3), "ERROR: exit code 137"}, obs(surefire.Flaky, 4)}, "R1"},
	}
	for _, tc := range table {
		t.Run(tc.name, func(t *testing.T) {
			repo := "sql/" + tc.name
			for i, o := range tc.history {
				m := meta(repo, int32(i+1), "SUCCESS")
				m.Repo = repo
				m.DefaultBranch = "trunk"
				m.Branch = o.branch
				m.TestedTreeSHA = o.tree
				m.StartedAt = t0.Add(time.Duration(i) * time.Minute)
				if _, err := svc.Ingest(t.Context(), Upload{Meta: m, Tests: []surefire.Result{test("lab.WindowTest", "a", o.outcome)}, LogTail: o.log}); err != nil {
					t.Fatal(err)
				}
			}
			rows, err := qs.List(t.Context(), repo)
			if err != nil {
				t.Fatal(err)
			}
			if tc.rule == "" {
				if len(rows) != 0 {
					t.Fatalf("false positive=%+v", rows)
				}
				return
			}
			if len(rows) != 1 || rows[0].ReasonRule != tc.rule {
				t.Fatalf("rows=%+v want%s", rows, tc.rule)
			}
		})
	}
}
