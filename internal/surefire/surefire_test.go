package surefire

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// hashOf computes the expected hash independently of FailureHash: type, newline, frame.
func hashOf(typ, frame string) string {
	sum := sha256.Sum256([]byte(typ + "\n" + frame))
	return hex.EncodeToString(sum[:8])
}

const (
	assertionErr = "org.opentest4j.AssertionFailedError"
	arithmetic   = "java.lang.ArithmeticException"
)

var (
	// The failures in the fixtures and the first frame in the subject's own code for each.
	wrongSumHash     = hashOf(assertionErr, "lab.core.CalculatorTest.wrongSum")
	flakyAssertHash  = hashOf(assertionErr, "lab.core.CalculatorTest.flakyAssertion")
	divideByZeroHash = hashOf(arithmetic, "lab.core.Calculator.divide") // first frame is in main code, not the test
	failsInITHash    = hashOf(assertionErr, "lab.core.CalculatorIT.failsInIntegration")
)

// rerunWant is the content of testdata/surefire/rerun, which the wrong-totals fixture must also produce.
var rerunWant = []Result{
	{Module: "core", ClassName: "lab.core.CalculatorTest", MethodName: "wrongSum", Outcome: Failed, DurationMs: 70, RerunFailures: 2, FailureType: assertionErr, FailureHash: wrongSumHash},
	{Module: "core", ClassName: "lab.core.CalculatorTest", MethodName: "flakyAssertion", Outcome: Flaky, DurationMs: 1, RerunFailures: 1, FailureType: assertionErr, FailureHash: flakyAssertHash},
	{Module: "core", ClassName: "lab.core.CalculatorTest", MethodName: "flakyException", Outcome: Flaky, DurationMs: 1, RerunFailures: 1, FailureType: arithmetic, FailureHash: divideByZeroHash},
	{Module: "core", ClassName: "lab.core.CalculatorTest", MethodName: "skippedTest", Outcome: Skipped, DurationMs: 0},
	{Module: "core", ClassName: "lab.core.CalculatorTest", MethodName: "addsNumbers", Outcome: Passed, DurationMs: 2},
	{Module: "core", ClassName: "lab.core.CalculatorTest", MethodName: "divideByZero", Outcome: Error, DurationMs: 3, RerunFailures: 2, FailureType: arithmetic, FailureHash: divideByZeroHash},
}

func TestParseFixtures(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string // under testdata/surefire
		reportPath string // path as the CLI uploads it; gives the module
		want       []Result
	}{
		{
			// passed, failure + rerunFailure, error + rerunError, skipped, flakyFailure, flakyError
			name:       "real report with reruns",
			fixture:    "rerun/TEST-lab.core.CalculatorTest.xml",
			reportPath: "core/target/surefire-reports/TEST-lab.core.CalculatorTest.xml",
			want:       rerunWant,
		},
		{
			// The bug in SUREFIRE-1627: totals say 14 tests; there are 6 testcase elements.
			name:       "wrong suite totals are ignored",
			fixture:    "wrong-totals/TEST-lab.core.CalculatorTest.xml",
			reportPath: "core/target/surefire-reports/TEST-lab.core.CalculatorTest.xml",
			want:       rerunWant,
		},
		{
			name:       "real report without reruns",
			fixture:    "no-rerun/TEST-lab.core.CalculatorTest.xml",
			reportPath: "target/surefire-reports/TEST-lab.core.CalculatorTest.xml",
			want: []Result{
				{ClassName: "lab.core.CalculatorTest", MethodName: "wrongSum", Outcome: Failed, DurationMs: 64, FailureType: assertionErr, FailureHash: wrongSumHash},
				{ClassName: "lab.core.CalculatorTest", MethodName: "flakyAssertion", Outcome: Failed, DurationMs: 8, FailureType: assertionErr, FailureHash: flakyAssertHash},
				{ClassName: "lab.core.CalculatorTest", MethodName: "flakyException", Outcome: Error, DurationMs: 2, FailureType: arithmetic, FailureHash: divideByZeroHash},
				{ClassName: "lab.core.CalculatorTest", MethodName: "skippedTest", Outcome: Skipped},
				{ClassName: "lab.core.CalculatorTest", MethodName: "addsNumbers", Outcome: Passed, DurationMs: 2},
				{ClassName: "lab.core.CalculatorTest", MethodName: "divideByZero", Outcome: Error, DurationMs: 2, FailureType: arithmetic, FailureHash: divideByZeroHash},
			},
		},
		{
			name:       "real failsafe report",
			fixture:    "failsafe/TEST-lab.core.CalculatorIT.xml",
			reportPath: "api/target/failsafe-reports/TEST-lab.core.CalculatorIT.xml",
			want: []Result{
				{Module: "api", ClassName: "lab.core.CalculatorIT", MethodName: "failsInIntegration", Outcome: Failed, DurationMs: 62, RerunFailures: 2, FailureType: assertionErr, FailureHash: failsInITHash},
				{Module: "api", ClassName: "lab.core.CalculatorIT", MethodName: "addsInIntegration", Outcome: Passed, DurationMs: 6},
			},
		},
		{
			name:       "testcase without classname uses the suite name",
			fixture:    "no-classname/TEST-lab.core.NoClass.xml",
			reportPath: "target/surefire-reports/TEST-lab.core.NoClass.xml",
			want:       []Result{{ClassName: "lab.core.NoClass", MethodName: "stillCounted", Outcome: Passed, DurationMs: 3}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("..", "..", "testdata", "surefire", tc.fixture))
			if err != nil {
				t.Fatalf("open fixture: %v", err)
			}
			defer f.Close() //nolint:errcheck // read-only test file

			got, err := Parse(f, tc.reportPath)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %d results, want %d: %+v", len(got), len(tc.want), got)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("result %d:\n got  %+v\n want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestParseRejectsBadInput(t *testing.T) {
	malformed, err := os.ReadFile(filepath.Join("..", "..", "testdata", "surefire", "malformed", "TEST-lab.core.Broken.xml"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	tests := []struct {
		name  string
		input string
	}{
		{"truncated report (real-looking, hand-made fixture)", string(malformed)},
		{"not XML at all", "this is a log file, not a report"},
		{"unclosed testcase", `<testsuite name="x"><testcase name="a" classname="b" time="1">`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tc.input), "target/surefire-reports/TEST-x.xml")
			if err == nil {
				t.Fatalf("Parse() = %+v, nil; want an error", got)
			}
			if got != nil {
				t.Errorf("Parse() returned %d results with the error; want none", len(got))
			}
			if !strings.Contains(err.Error(), "TEST-x.xml") {
				t.Errorf("error %q does not name the report", err)
			}
		})
	}
}

func TestParseEmptySuite(t *testing.T) {
	got, err := Parse(strings.NewReader(`<?xml version="1.0"?><testsuite name="x" tests="0"/>`), "TEST-x.xml")
	if err != nil || len(got) != 0 {
		t.Fatalf("Parse(empty suite) = %v, %v; want no results and no error", got, err)
	}
}

func TestFailureHash(t *testing.T) {
	const traceLine12 = "java.lang.IllegalStateException: boom\n\tat java.base/java.util.Objects.requireNonNull(Objects.java:259)\n\tat org.junit.jupiter.api.Assertions.fail(Assertions.java:1)\n\tat com.acme.Order.total(Order.java:12)\n\tat com.acme.OrderTest.totals(OrderTest.java:30)\n"
	traceLine99 := strings.ReplaceAll(traceLine12, "Order.java:12", "Order.java:99")

	tests := []struct {
		name      string
		typ       string
		trace     string
		want      string
		wantEqual string // if set, the hash must equal FailureHash(typ, wantEqual)
	}{
		{
			name: "first frame outside JDK and test frameworks",
			typ:  "java.lang.IllegalStateException", trace: traceLine12,
			want: hashOf("java.lang.IllegalStateException", "com.acme.Order.total"),
		},
		{
			name: "line number does not matter",
			typ:  "java.lang.IllegalStateException", trace: traceLine99,
			want: hashOf("java.lang.IllegalStateException", "com.acme.Order.total"),
		},
		{
			name: "different exception type, same frame, different hash",
			typ:  "java.lang.NullPointerException", trace: traceLine12,
			want: hashOf("java.lang.NullPointerException", "com.acme.Order.total"),
		},
		{
			name: "module prefix is removed",
			typ:  "x.E", trace: "x.E\n\tat app//com.acme.Thing.run(Thing.java:3)\n",
			want: hashOf("x.E", "com.acme.Thing.run"),
		},
		{
			name: "only framework frames: type alone",
			typ:  "x.E", trace: "x.E\n\tat java.base/java.lang.Thread.run(Thread.java:1)\n\tat org.junit.Foo.bar(Foo.java:2)\n",
			want: hashOf("x.E", ""),
		},
		{
			name: "no stack trace at all",
			typ:  "x.E", trace: "",
			want: hashOf("x.E", ""),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FailureHash(tc.typ, tc.trace)
			if got != tc.want {
				t.Errorf("FailureHash() = %s, want %s", got, tc.want)
			}
			if len(got) != 16 {
				t.Errorf("hash length = %d, want 16 hex characters", len(got))
			}
		})
	}
}

func TestFailureTypeFromTraceWhenAttributeMissing(t *testing.T) {
	report := `<testsuite name="s"><testcase name="m" classname="c" time="0.5">
<failure message="m"><![CDATA[com.acme.BadThing: went wrong
	at com.acme.Code.run(Code.java:1)
]]></failure></testcase></testsuite>`
	got, err := Parse(strings.NewReader(report), "TEST-c.xml")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := Result{ClassName: "c", MethodName: "m", Outcome: Failed, DurationMs: 500,
		FailureType: "com.acme.BadThing", FailureHash: hashOf("com.acme.BadThing", "com.acme.Code.run")}
	if len(got) != 1 || got[0] != want {
		t.Fatalf("Parse() = %+v, want %+v", got, want)
	}
}

func TestModuleFromPath(t *testing.T) {
	tests := []struct{ path, want string }{
		{"target/surefire-reports/TEST-a.xml", ""},
		{"core/target/surefire-reports/TEST-a.xml", "core"},
		{"services/api/target/failsafe-reports/TEST-a.xml", "services/api"},
		{`core\target\surefire-reports\TEST-a.xml`, "core"},
		{"./core/target/surefire-reports/TEST-a.xml", "core"},
		{"TEST-a.xml", ""},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			if got := ModuleFromPath(tc.path); got != tc.want {
				t.Errorf("ModuleFromPath(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestSecondsToMs(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0.07", 70}, {"0.0005", 1}, {"0.0004", 0}, {"12", 12000}, {"1,234.5", 1234500},
		{"", 0}, {"abc", 0}, {"-1", 0}, {"NaN", 0},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := secondsToMs(tc.in); got != tc.want {
				t.Errorf("secondsToMs(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	pass := Result{Module: "core", ClassName: "C", MethodName: "m", Outcome: Passed, DurationMs: 10}
	fail := Result{ClassName: "C", MethodName: "m", Outcome: Failed, DurationMs: 5, RerunFailures: 2, FailureType: "T", FailureHash: "h1"}
	errored := Result{ClassName: "C", MethodName: "m", Outcome: Error, DurationMs: 1, FailureType: "E", FailureHash: "h2"}
	other := Result{ClassName: "C", MethodName: "other", Outcome: Passed, DurationMs: 3}

	tests := []struct {
		name string
		in   []Result
		want []Result
	}{
		{"no duplicates unchanged", []Result{pass, other}, []Result{pass, other}},
		{
			"worse outcome wins, durations and reruns add up",
			[]Result{pass, other, fail},
			[]Result{{Module: "core", ClassName: "C", MethodName: "m", Outcome: Failed, DurationMs: 15, RerunFailures: 2, FailureType: "T", FailureHash: "h1"}, other},
		},
		{
			"error beats failure",
			[]Result{fail, errored},
			[]Result{{ClassName: "C", MethodName: "m", Outcome: Error, DurationMs: 6, RerunFailures: 2, FailureType: "E", FailureHash: "h2"}},
		},
		{
			"a later pass does not hide a failure",
			[]Result{fail, pass},
			[]Result{{Module: "core", ClassName: "C", MethodName: "m", Outcome: Failed, DurationMs: 15, RerunFailures: 2, FailureType: "T", FailureHash: "h1"}},
		},
		{"empty", nil, []Result{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Merge(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Merge() =\n %+v\nwant\n %+v", got, tc.want)
			}
		})
	}
}
