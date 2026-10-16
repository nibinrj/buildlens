// Package quarantine manages auditable test exclusions and their release policy.
package quarantine

import (
	"errors"
	"math"

	"github.com/nibinrj/buildlens/internal/rules"
)

// Config is the complete P3 policy loaded from optional YAML.
type Config struct {
	Rules          rules.Config `yaml:"rules"`
	ReleasePasses  int          `yaml:"release_passes"`
	CapPercent     float64      `yaml:"cap_percent"`
	CapMaxTests    int          `yaml:"cap_max_tests"`
	CapBuildWindow int          `yaml:"cap_build_window"`
}

func Defaults() Config { return Config{rules.Defaults(), 10, 5, 10, 30} }

// Validate rejects policy mistakes at startup, before any tests can be excluded.
func (c Config) Validate() error {
	r := c.Rules
	if r.R1FlakyBuilds < 1 || r.R1Window < r.R1FlakyBuilds || r.R2Window < 2 ||
		r.R3Flips < 1 || r.R3Window <= r.R3Flips || c.ReleasePasses < 1 ||
		math.IsNaN(c.CapPercent) || math.IsInf(c.CapPercent, 0) || c.CapPercent <= 0 || c.CapPercent > 100 ||
		c.CapMaxTests < 1 || c.CapBuildWindow < 1 || c.CapMaxTests > 1<<30 || c.CapBuildWindow > 1<<30 ||
		c.ReleasePasses > 1<<30 || r.R1Window > 1<<30 || r.R2Window > 1<<30 || r.R3Window > 1<<30 {
		return errors.New("invalid P3 thresholds: positive counts, possible rule windows and cap percent in (0,100] required")
	}
	return nil
}

// Cap implements D-077 and includes manual entries in the caller's active count.
func Cap(testsSeen int, cfg Config) int {
	return min(cfg.CapMaxTests, max(1, int(math.Floor(cfg.CapPercent/100*float64(testsSeen)))))
}

// ConsecutivePasses scans newest-first eligible quarantine outcomes; skipped runs are neutral.
func ConsecutivePasses(outcomes []string) int {
	passes := 0
	for _, outcome := range outcomes {
		switch outcome {
		case "PASSED":
			passes++
		case "FAILED", "ERROR", "FLAKY":
			return passes
		}
	}
	return passes
}
