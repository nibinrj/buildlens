// Package regression detects sustained duration changes using robust baselines.
package regression

import (
	"errors"
	"math"
	"slices"
)

type Config struct {
	Window        int     `yaml:"window"`
	MinSamples    int     `yaml:"min_samples"`
	MADMultiplier float64 `yaml:"mad_multiplier"`
	MinIncrease   float64 `yaml:"min_increase"`
	PRJump        float64 `yaml:"pr_jump"`
}

func Defaults() Config { return Config{20, 10, 3, 0.2, 0.5} }

func (c Config) Validate() error {
	if c.Window < 1 || c.Window > 100 || c.MinSamples < 2 || c.MinSamples > c.Window ||
		!finitePositive(c.MADMultiplier) || !finitePositive(c.MinIncrease) || !finitePositive(c.PRJump) {
		return errors.New("regression: require window <= 100, 2 <= min_samples <= window, and finite positive multipliers")
	}
	return nil
}

func finitePositive(v float64) bool { return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v) }

// Evidence retains enough information to explain normal and insufficient results too.
type Evidence struct {
	Kind             string  `json:"kind"`
	Name             string  `json:"name"`
	Status           string  `json:"status"`
	Samples          int     `json:"samples"`
	MedianMs         float64 `json:"median_ms"`
	MADMs            float64 `json:"mad_ms"`
	ThresholdMs      float64 `json:"threshold_ms"`
	CurrentMs        int64   `json:"current_ms"`
	RecentMs         []int64 `json:"recent_ms"`
	BaselineMs       []int64 `json:"baseline_ms"`
	BaselineBuildIDs []int64 `json:"baseline_build_ids"`
	RecentBuildIDs   []int64 `json:"recent_build_ids"`
}

// Evaluate expects disjoint baseline and newest-first recent observations, with
// the current observation first. The caller selects comparable successful runs.
func Evaluate(baseline, recent []int64, pr bool, cfg Config) (Evidence, error) {
	if err := cfg.Validate(); err != nil {
		return Evidence{}, err
	}
	for _, values := range [][]int64{baseline, recent} {
		for _, v := range values {
			if v < 0 {
				return Evidence{}, errors.New("regression: negative duration")
			}
		}
	}
	n := 3
	if pr {
		n = 2
	}
	baseline = baseline[:min(len(baseline), cfg.Window)]
	recent = recent[:min(len(recent), n)]
	e := Evidence{Status: "INSUFFICIENT_DATA", Samples: len(baseline), RecentMs: append([]int64{}, recent...), BaselineMs: append([]int64{}, baseline...)}
	if len(recent) == 0 {
		return e, nil
	}
	e.CurrentMs = recent[0]
	if len(baseline) < cfg.MinSamples {
		return e, nil
	}
	values := make([]float64, len(baseline))
	for i, v := range baseline {
		values[i] = float64(v)
	}
	e.MedianMs = median(values)
	for i, v := range baseline {
		values[i] = math.Abs(float64(v) - e.MedianMs)
	}
	e.MADMs = median(values)
	e.ThresholdMs = e.MedianMs + math.Max(cfg.MADMultiplier*1.4826*e.MADMs, cfg.MinIncrease*e.MedianMs)
	e.Status = "NORMAL"
	exceeded := 0
	for _, v := range recent {
		if float64(v) > e.ThresholdMs {
			exceeded++
		}
	}
	// A default-branch alert describes the sustained window even if its last run recovered.
	alert := exceeded >= 2
	if pr {
		alert = float64(recent[0]) > e.ThresholdMs && (float64(recent[0]) >= (1+cfg.PRJump)*e.ThresholdMs || exceeded >= 2)
	}
	if alert {
		e.Status = "REGRESSION"
	}
	return e, nil
}

func median(values []float64) float64 {
	slices.Sort(values)
	n := len(values)
	if n%2 != 0 {
		return values[n/2]
	}
	return values[n/2-1]/2 + values[n/2]/2
}
