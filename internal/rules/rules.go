// Package rules evaluates explainable flaky-test evidence.
package rules

import (
	"sort"
	"time"
)

// Config windows count eligible runs of one test.
type Config struct {
	R1FlakyBuilds int `yaml:"r1_flaky_builds"`
	R1Window      int `yaml:"r1_window"`
	R2Window      int `yaml:"r2_window"`
	R3Flips       int `yaml:"r3_flips"`
	R3Window      int `yaml:"r3_window"`
}

func Defaults() Config { return Config{2, 30, 30, 3, 30} }

// Run retains observations used by a finding, including failure hashes for later review.
type Run struct {
	BuildID       int64     `json:"build_id"`
	Outcome       string    `json:"outcome"`
	Branch        string    `json:"branch"`
	TestedTreeSHA string    `json:"tested_tree_sha,omitempty"`
	InfraFailure  bool      `json:"infra_failure"`
	StartedAt     time.Time `json:"started_at"`
	FailureHash   string    `json:"failure_hash,omitempty"`
}

type Finding struct {
	Rule string `json:"rule"`
	Runs []Run  `json:"runs"`
}

// Evaluate removes infra observations before windowing and accepts any input order.
func Evaluate(history []Run, defaultBranch string, cfg Config) []Finding {
	runs := make([]Run, 0, len(history))
	for _, r := range history {
		if !r.InfraFailure {
			runs = append(runs, r)
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt.Equal(runs[j].StartedAt) {
			return runs[i].BuildID > runs[j].BuildID
		}
		return runs[i].StartedAt.After(runs[j].StartedAt)
	})
	findings := make([]Finding, 0, 3)
	flakes := make([]Run, 0)
	for _, r := range window(runs, cfg.R1Window) {
		if r.Outcome == "FLAKY" {
			flakes = append(flakes, r)
		}
	}
	if len(flakes) >= cfg.R1FlakyBuilds {
		findings = append(findings, Finding{"R1", flakes})
	}
	type pair struct{ pass, fail *Run }
	trees := map[string]pair{}
	for _, r := range window(runs, cfg.R2Window) {
		if r.TestedTreeSHA == "" {
			continue
		}
		p := trees[r.TestedTreeSHA]
		switch r.Outcome {
		case "PASSED":
			p.pass = &r
		case "FAILED", "ERROR":
			p.fail = &r
		}
		if p.pass != nil && p.fail != nil {
			findings = append(findings, Finding{"R2", []Run{*p.pass, *p.fail}})
			break
		}
		trees[r.TestedTreeSHA] = p
	}
	mainRuns := make([]Run, 0)
	for _, r := range runs {
		if r.Branch == defaultBranch && (r.Outcome == "PASSED" || r.Outcome == "FAILED" || r.Outcome == "ERROR") {
			mainRuns = append(mainRuns, r)
		}
	}
	mainRuns = window(mainRuns, cfg.R3Window)
	flips := 0
	for i := 1; i < len(mainRuns); i++ {
		if (mainRuns[i-1].Outcome == "PASSED") != (mainRuns[i].Outcome == "PASSED") {
			flips++
		}
	}
	if flips >= cfg.R3Flips {
		findings = append(findings, Finding{"R3", mainRuns})
	}
	return findings
}
func window(runs []Run, n int) []Run { return runs[:min(len(runs), max(0, n))] }
