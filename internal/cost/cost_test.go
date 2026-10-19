package cost

import "testing"

func TestCalculate(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ms         int64
		rate, want string
	}{
		{"hour", 3_600_000, "0.020900", "0.020900"},
		{"minute", 60_000, "0.020900", "0.000348"},
		{"half micro rounds up", 1, "1.8", "0.000001"},
		{"zero duration", 0, "0.02", "0.000000"},
		{"free explicit rate", 123, "0", "0.000000"},
		{"negative", -1, "1", ""},
		{"bad rate", 1, "NaN", ""},
		{"negative rate", 1, "-1", ""},
		{"excess precision", 1, "0.0000001", ""},
		{"overflow", 1<<63 - 1, "9999.999999", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Calculate(tc.ms, tc.rate)
			if got != tc.want || (err != nil) != (tc.want == "") {
				t.Fatalf("got %q,%v want %q", got, err, tc.want)
			}
		})
	}
}
