package upload

import (
	"strings"
	"testing"
	"time"
)

func validMetadata() Metadata {
	start := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Second)
	dur := int64(90_000)
	pr := int32(7)
	return Metadata{
		Repo: "nibinrj/buildlens-lab", Job: "buildlens-lab/main", BuildNumber: 3, Branch: "main", PRNumber: &pr,
		CommitSHA: "0123456789abcdef0123456789abcdef01234567", TestedTreeSHA: "89abcdef0123456789abcdef0123456789abcdef",
		Result: "SUCCESS", StartedAt: start, FinishedAt: &end, DurationMs: &dur,
		Agent:  Agent{Name: "agent-1", Lifecycle: "LOCAL"},
		Stages: []Stage{{Name: "Build", StartedAt: start, DurationMs: 1000, Result: "SUCCESS"}},
	}
}

func TestValidate(t *testing.T) {
	neg := int64(-1)
	zeroPR := int32(0)
	early := time.Date(2026, 10, 4, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		mutate  func(*Metadata)
		wantErr []string // substrings; empty means valid
	}{
		{name: "valid", mutate: func(*Metadata) {}},
		{name: "minimal valid: no PR, tree, end, agent or stages", mutate: func(m *Metadata) {
			m.PRNumber, m.TestedTreeSHA, m.FinishedAt, m.DurationMs, m.Agent, m.Stages = nil, "", nil, nil, Agent{}, nil
		}},
		{name: "short commit sha is fine", mutate: func(m *Metadata) { m.CommitSHA = "abc1234" }},
		{name: "repo without owner", mutate: func(m *Metadata) { m.Repo = "buildlens-lab" }, wantErr: []string{"repo must look like owner/name"}},
		{name: "repo with extra slash", mutate: func(m *Metadata) { m.Repo = "a/b/c" }, wantErr: []string{"repo must look like owner/name"}},
		{name: "missing job", mutate: func(m *Metadata) { m.Job = " " }, wantErr: []string{"job is required"}},
		{name: "zero build number", mutate: func(m *Metadata) { m.BuildNumber = 0 }, wantErr: []string{"build_number must be positive"}},
		{name: "missing branch", mutate: func(m *Metadata) { m.Branch = "" }, wantErr: []string{"branch is required"}},
		{name: "zero PR number", mutate: func(m *Metadata) { m.PRNumber = &zeroPR }, wantErr: []string{"pr_number must be positive"}},
		{name: "commit not hex", mutate: func(m *Metadata) { m.CommitSHA = "main" }, wantErr: []string{"commit_sha"}},
		{name: "tree too short", mutate: func(m *Metadata) { m.TestedTreeSHA = "abc1234" }, wantErr: []string{"tested_tree_sha"}},
		{name: "unknown result", mutate: func(m *Metadata) { m.Result = "GREEN" }, wantErr: []string{"result must be one of"}},
		{name: "missing start", mutate: func(m *Metadata) { m.StartedAt = time.Time{} }, wantErr: []string{"started_at is required"}},
		{name: "end before start", mutate: func(m *Metadata) { m.FinishedAt = &early }, wantErr: []string{"finished_at is before started_at"}},
		{name: "negative duration", mutate: func(m *Metadata) { m.DurationMs = &neg }, wantErr: []string{"duration_ms must not be negative"}},
		{name: "unknown lifecycle", mutate: func(m *Metadata) { m.Agent.Lifecycle = "RESERVED" }, wantErr: []string{"agent.lifecycle"}},
		{name: "bad stage", mutate: func(m *Metadata) {
			m.Stages = append(m.Stages, Stage{Name: "", DurationMs: -5, Result: "OK"})
		}, wantErr: []string{"stages[1].name is required", "stages[1].started_at is required", "stages[1].duration_ms", "stages[1].result"}},
		{name: "all errors reported together", mutate: func(m *Metadata) { m.Job, m.Branch = "", "" },
			wantErr: []string{"job is required", "branch is required"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := validMetadata()
			tc.mutate(&m)
			err := m.Validate()
			if len(tc.wantErr) == 0 {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() = nil, want errors containing %q", tc.wantErr)
			}
			for _, w := range tc.wantErr {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q does not contain %q", err, w)
				}
			}
		})
	}
}
