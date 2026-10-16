package quarantine

import "testing"

func TestCap(t *testing.T) {
	for _, tt := range []struct{ seen, want int }{{0, 1}, {19, 1}, {20, 1}, {39, 1}, {40, 2}, {199, 9}, {200, 10}, {1000, 10}} {
		if got := Cap(tt.seen, Defaults()); got != tt.want {
			t.Errorf("Cap(%d)=%d want %d", tt.seen, got, tt.want)
		}
	}
	cfg := Defaults()
	cfg.CapPercent = 10
	cfg.CapMaxTests = 3
	if got := Cap(100, cfg); got != 3 {
		t.Errorf("config cap=%d", got)
	}
}
func TestConsecutivePasses(t *testing.T) {
	for _, tt := range []struct {
		name     string
		outcomes []string
		want     int
	}{
		{"empty", nil, 0},
		{"passes", []string{"PASSED", "PASSED"}, 2},
		{"failed reset", []string{"PASSED", "FAILED", "PASSED"}, 1},
		{"error reset", []string{"ERROR", "PASSED"}, 0},
		{"flake reset", []string{"FLAKY", "PASSED"}, 0},
		{"skipped neutral", []string{"PASSED", "SKIPPED", "PASSED"}, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConsecutivePasses(tt.outcomes); got != tt.want {
				t.Errorf("got %d want %d", got, tt.want)
			}
		})
	}
}
func TestPolicyValidation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		mutate func(*Config)
	}{
		{"zero K", func(c *Config) { c.ReleasePasses = 0 }},
		{"impossible R1", func(c *Config) { c.Rules.R1Window = 1 }},
		{"impossible R3", func(c *Config) { c.Rules.R3Window = 3 }},
		{"zero cap", func(c *Config) { c.CapMaxTests = 0 }},
		{"bad percent", func(c *Config) { c.CapPercent = 101 }},
		{"negative history", func(c *Config) { c.CapBuildWindow = -1 }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Defaults()
			tt.mutate(&cfg)
			if cfg.Validate() == nil {
				t.Error("invalid policy accepted")
			}
		})
	}
	if err := Defaults().Validate(); err != nil {
		t.Fatal(err)
	}
}
