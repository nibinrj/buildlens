package rules

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func history(outcomes ...string) []Run {
	runs := make([]Run, len(outcomes))
	for i, o := range outcomes {
		runs[i] = Run{BuildID: int64(i + 1), Outcome: o, Branch: "main",
			TestedTreeSHA: fmt.Sprint("tree-", i), StartedAt: time.Unix(int64(i), 0), FailureHash: "hash"}
	}
	return runs
}
func TestEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		outcomes []string
		change   func([]Run)
		cfg      Config
		want     []string
	}{
		{"R1 two reruns", []string{"FLAKY", "PASSED", "FLAKY"}, nil, Defaults(), []string{"R1"}},
		{"R1 one is insufficient", []string{"PASSED", "FLAKY"}, nil, Defaults(), []string{}},
		{"R1 thresholds configurable", []string{"FLAKY", "FLAKY", "PASSED"}, nil, Config{2, 2, 2, 3, 4}, []string{}},
		{"R2 identical tree", []string{"FAILED", "PASSED"}, func(r []Run) { r[1].TestedTreeSHA = r[0].TestedTreeSHA }, Defaults(), []string{"R2"}},
		{"R2 error is failure", []string{"ERROR", "PASSED"}, func(r []Run) { r[1].TestedTreeSHA = r[0].TestedTreeSHA }, Defaults(), []string{"R2"}},
		{"R2 PR target moved", []string{"FAILED", "PASSED"}, func(r []Run) {
			for i := range r {
				r[i].Branch = "PR-1"
			}
		}, Defaults(), []string{}},
		{"R2 missing trees", []string{"FAILED", "PASSED"}, func(r []Run) {
			for i := range r {
				r[i].TestedTreeSHA = ""
			}
		}, Defaults(), []string{}},
		{"R2 window", []string{"FAILED", "PASSED", "PASSED"}, func(r []Run) {
			for i := range r {
				r[i].TestedTreeSHA = "a"
			}
		}, Config{2, 30, 2, 3, 30}, []string{}},
		{"R3 three flips", []string{"PASSED", "FAILED", "PASSED", "ERROR"}, nil, Defaults(), []string{"R3"}},
		{"genuine break then fix", []string{"PASSED", "FAILED", "FAILED", "PASSED"}, nil, Defaults(), []string{}},
		{"R3 skips flakes and skipped", []string{"PASSED", "FLAKY", "FAILED", "SKIPPED", "PASSED", "ERROR"}, nil, Defaults(), []string{"R3"}},
		{"R3 default branch only", []string{"PASSED", "FAILED", "PASSED", "ERROR"}, func(r []Run) {
			for i := range r {
				r[i].Branch = "feature"
			}
		}, Defaults(), []string{}},
		{"R3 failure and error same", []string{"FAILED", "ERROR", "FAILED", "ERROR"}, nil, Defaults(), []string{}},
		{"all failures only infra", []string{"PASSED", "FAILED", "FLAKY", "FLAKY", "PASSED", "ERROR"}, func(r []Run) {
			for i := range r {
				r[i].TestedTreeSHA = "a"
				r[i].InfraFailure = r[i].Outcome != "PASSED"
			}
		}, Defaults(), []string{}},
		{"infra filtered before R1 window", []string{"FLAKY", "FLAKY", "PASSED", "PASSED"}, func(r []Run) { r[2].InfraFailure = true; r[3].InfraFailure = true }, Config{2, 2, 2, 3, 4}, []string{"R1"}},
		{"all rules store independent evidence", []string{"FLAKY", "FLAKY", "PASSED", "FAILED", "PASSED", "ERROR"}, func(r []Run) {
			for i := range r {
				r[i].TestedTreeSHA = "a"
			}
		}, Defaults(), []string{"R1", "R2", "R3"}},
		{"no history", nil, nil, Defaults(), []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runs := history(tt.outcomes...)
			if tt.change != nil {
				tt.change(runs)
			}
			before := append([]Run(nil), runs...)
			got := Evaluate(runs, "main", tt.cfg)
			ids := []string{}
			for _, f := range got {
				ids = append(ids, f.Rule)
				if len(f.Runs) == 0 {
					t.Error("finding without evidence")
				}
				for _, r := range f.Runs {
					if r.InfraFailure {
						t.Error("infra leaked into evidence")
					}
				}
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("rules=%v want %v", ids, tt.want)
			}
			if len(runs) > 0 && !reflect.DeepEqual(runs, before) {
				t.Error("mutated input")
			}
		})
	}
}
func TestHistoryOrdering(t *testing.T) {
	r := history("FLAKY", "PASSED", "FLAKY")
	r[0], r[2] = r[2], r[0]
	got := Evaluate(r, "main", Defaults())
	if got[0].Runs[0].BuildID != 3 || got[0].Runs[1].BuildID != 1 {
		t.Fatalf("wrong order: %+v", got)
	}
}
