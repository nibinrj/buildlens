// Package upload defines what the buildlens CLI sends to POST /api/v1/builds and how the server validates it.
// Both sides import it, so the format is defined once. It has no database code, so the CLI stays small.
package upload

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Multipart part names of an upload.
const (
	PartMetadata = "metadata" // JSON Metadata, required, sent first
	PartReport   = "report"   // one Surefire/Failsafe XML file; the filename is its path relative to the workspace
	PartLog      = "log"      // the build's log tail, plain text, optional
)

// Limits the server enforces on single parts (the whole body has its own limit).
const (
	MaxMetadataBytes = 1 << 20 // 1 MiB
	MaxLogBytes      = 1 << 20 // 1 MiB: 500 lines is far less
)

// Results a build or a stage can have; they match the CHECK constraints in the schema.
var results = map[string]bool{"SUCCESS": true, "UNSTABLE": true, "FAILURE": true, "ABORTED": true, "NOT_BUILT": true}

// Lifecycles an agent can have; empty means unknown.
var lifecycles = map[string]bool{"": true, "LOCAL": true, "SPOT": true, "ON_DEMAND": true}

// Metadata describes one build.
type Metadata struct {
	Repo          string     `json:"repo"`                     // "owner/name"
	DefaultBranch string     `json:"default_branch,omitempty"` // used only when the repository is first seen; default "main"
	Job           string     `json:"job"`                      // Jenkins JOB_NAME, e.g. "buildlens-lab/main"
	BuildNumber   int32      `json:"build_number"`
	Branch        string     `json:"branch"`
	PRNumber      *int32     `json:"pr_number,omitempty"`
	CommitSHA     string     `json:"commit_sha"`
	TestedTreeSHA string     `json:"tested_tree_sha,omitempty"` // git tree hash of the checkout (D-078)
	Result        string     `json:"result"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
	DurationMs    *int64     `json:"duration_ms,omitempty"`
	Agent         Agent      `json:"agent"`
	Stages        []Stage    `json:"stages"`
}

// Agent is where the build ran.
type Agent struct {
	Name         string `json:"name,omitempty"`
	InstanceType string `json:"instance_type,omitempty"`
	Lifecycle    string `json:"lifecycle,omitempty"` // LOCAL, SPOT, ON_DEMAND
}

// Stage is one stage timed by the shared library's timedStage.
type Stage struct {
	Name       string    `json:"name"`
	StartedAt  time.Time `json:"started_at"`
	DurationMs int64     `json:"duration_ms"`
	Result     string    `json:"result"`
}

// Validate reports every problem at once, so a broken pipeline is fixed in one go.
func (m Metadata) Validate() error {
	var errs []error
	check := func(ok bool, format string, args ...any) {
		if !ok {
			errs = append(errs, fmt.Errorf(format, args...))
		}
	}

	owner, name, found := strings.Cut(m.Repo, "/")
	check(found && owner != "" && name != "" && !strings.Contains(name, "/") && len(m.Repo) <= 200,
		"repo must look like owner/name, got %q", m.Repo)
	check(strings.TrimSpace(m.Job) != "", "job is required")
	check(m.BuildNumber > 0, "build_number must be positive, got %d", m.BuildNumber)
	check(strings.TrimSpace(m.Branch) != "", "branch is required")
	check(m.PRNumber == nil || *m.PRNumber > 0, "pr_number must be positive when set")
	check(isHex(m.CommitSHA, 7, 64), "commit_sha must be 7-64 hex characters, got %q", m.CommitSHA)
	check(m.TestedTreeSHA == "" || isHex(m.TestedTreeSHA, 40, 64), "tested_tree_sha must be 40-64 hex characters, got %q", m.TestedTreeSHA)
	check(results[m.Result], "result must be one of SUCCESS, UNSTABLE, FAILURE, ABORTED, NOT_BUILT, got %q", m.Result)
	check(!m.StartedAt.IsZero(), "started_at is required")
	check(m.FinishedAt == nil || !m.FinishedAt.Before(m.StartedAt), "finished_at is before started_at")
	check(m.DurationMs == nil || *m.DurationMs >= 0, "duration_ms must not be negative")
	check(lifecycles[m.Agent.Lifecycle], "agent.lifecycle must be LOCAL, SPOT or ON_DEMAND, got %q", m.Agent.Lifecycle)

	for i, s := range m.Stages {
		check(strings.TrimSpace(s.Name) != "", "stages[%d].name is required", i)
		check(!s.StartedAt.IsZero(), "stages[%d].started_at is required", i)
		check(s.DurationMs >= 0, "stages[%d].duration_ms must not be negative", i)
		check(results[s.Result], "stages[%d].result %q is not a known result", i, s.Result)
	}
	return errors.Join(errs...) // nil when errs is empty
}

func isHex(s string, minLen, maxLen int) bool {
	if len(s) < minLen || len(s) > maxLen {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
