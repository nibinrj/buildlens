package regression

import (
	"math"
	"testing"
)

func TestEvaluate(t *testing.T) {
	flat := []int64{100, 100, 100, 100, 100, 100, 100, 100, 100, 100}
	for _, tc := range []struct {
		name             string
		baseline, recent []int64
		pr               bool
		status           string
	}{
		{"noise", flat, []int64{110, 95, 102}, false, "NORMAL"},
		{"one spike", flat, []int64{900, 100, 100}, false, "NORMAL"},
		{"sustained", flat, []int64{130, 100, 131}, false, "REGRESSION"},
		{"recovered latest", flat, []int64{100, 130, 131}, false, "REGRESSION"},
		{"equal threshold", flat, []int64{120, 120, 120}, false, "NORMAL"},
		{"pr big jump boundary", flat, []int64{180}, true, "REGRESSION"},
		{"pr below jump", flat, []int64{179}, true, "NORMAL"},
		{"pr two exceedances", flat, []int64{121, 122}, true, "REGRESSION"},
		{"pr recovered", flat, []int64{100, 200}, true, "NORMAL"},
		{"few samples", flat[:9], []int64{900, 900, 900}, false, "INSUFFICIENT_DATA"},
		{"no recent", flat, nil, false, "INSUFFICIENT_DATA"},
		{"zeros", []int64{0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, []int64{1, 1}, false, "REGRESSION"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, err := Evaluate(tc.baseline, tc.recent, tc.pr, Defaults())
			if err != nil || e.Status != tc.status {
				t.Fatalf("got %+v, %v; want %s", e, err, tc.status)
			}
		})
	}
	t.Run("median MAD resists outlier without mutating inputs", func(t *testing.T) {
		b := []int64{90, 92, 94, 96, 98, 102, 104, 106, 108, 10000}
		e, err := Evaluate(b, []int64{101}, false, Defaults())
		if err != nil || e.MedianMs != 100 || e.MADMs != 6 || math.Abs(e.ThresholdMs-126.6868) > 1e-8 || b[9] != 10000 {
			t.Fatalf("%+v, %v", e, err)
		}
	})
	t.Run("invalid durations", func(t *testing.T) {
		for _, parts := range [][2][]int64{{{-1}, {1}}, {{1}, {-1}}} {
			if _, err := Evaluate(parts[0], parts[1], false, Defaults()); err == nil {
				t.Fatal("negative accepted")
			}
		}
	})
}

func TestConfig(t *testing.T) {
	for _, change := range []func(*Config){func(c *Config) { c.Window = 101 }, func(c *Config) { c.MinSamples = 1 }, func(c *Config) { c.MinSamples = 21 }, func(c *Config) { c.MADMultiplier = 0 }, func(c *Config) { c.MinIncrease = math.NaN() }, func(c *Config) { c.PRJump = math.Inf(1) }} {
		c := Defaults()
		change(&c)
		if c.Validate() == nil {
			t.Fatalf("accepted %+v", c)
		}
	}
}
