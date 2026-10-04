// Package surefire parses Maven Surefire and Failsafe XML test reports.
//
// The rules follow docs/plan.md ("Parsing Surefire and Failsafe XML"): every testcase element is read and the
// suite's top-level totals are ignored, because with reruns they can count every attempt as a test (SUREFIRE-1627).
package surefire

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"
)

// Outcome is a test's result in one build. The values match the test_run.outcome CHECK constraint.
type Outcome string

// The possible outcomes, in the plan's terms.
const (
	Passed  Outcome = "PASSED"
	Failed  Outcome = "FAILED"
	Error   Outcome = "ERROR"
	Skipped Outcome = "SKIPPED"
	Flaky   Outcome = "FLAKY"
)

// Result is one test method's result, as stored in test_case and test_run.
type Result struct {
	Module        string // Maven module, from the report path; "" for the root module
	ClassName     string
	MethodName    string
	Outcome       Outcome
	DurationMs    int64
	RerunFailures int    // failed reruns (FAILED/ERROR) or failed attempts before the pass (FLAKY)
	FailureType   string // exception class, e.g. org.opentest4j.AssertionFailedError; "" when it passed
	FailureHash   string // see FailureHash; "" when it passed
}

// xmlTestCase mirrors the testcase element of surefire-test-report.xsd. Struct tags tell encoding/xml which
// attribute or child element fills each field, much like JAXB annotations.
type xmlTestCase struct {
	Name          string       `xml:"name,attr"`
	ClassName     string       `xml:"classname,attr"`
	Time          string       `xml:"time,attr"`
	Failures      []xmlProblem `xml:"failure"`
	Errors        []xmlProblem `xml:"error"`
	Skipped       *struct{}    `xml:"skipped"`
	FlakyFailures []xmlRerun   `xml:"flakyFailure"`
	FlakyErrors   []xmlRerun   `xml:"flakyError"`
	RerunFailures []xmlRerun   `xml:"rerunFailure"`
	RerunErrors   []xmlRerun   `xml:"rerunError"`
}

// xmlProblem is a failure or error: the stack trace is the element's text.
type xmlProblem struct {
	Type  string `xml:"type,attr"`
	Trace string `xml:",chardata"`
}

// xmlRerun is a flaky*/rerun* element: the stack trace is a child element.
type xmlRerun struct {
	Type  string `xml:"type,attr"`
	Trace string `xml:"stackTrace"`
}

// Parse reads one report and returns a Result per testcase element. reportPath is the report's path relative to
// the workspace (for example "core/target/surefire-reports/TEST-x.xml"); it gives the module name.
// A malformed report returns an error and no results, never a partial list.
func Parse(r io.Reader, reportPath string) ([]Result, error) {
	module := ModuleFromPath(reportPath)
	dec := xml.NewDecoder(r)

	var (
		results   []Result
		suiteName string
		sawSuite  bool
	)
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", reportPath, err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "testsuite":
			sawSuite = true
			suiteName = attr(start, "name")
		case "testcase":
			var tc xmlTestCase
			// DecodeElement reads just this testcase (and its children) from the stream.
			if err := dec.DecodeElement(&tc, &start); err != nil {
				return nil, fmt.Errorf("parse %s: testcase: %w", reportPath, err)
			}
			res := toResult(tc, suiteName)
			res.Module = module
			results = append(results, res)
		}
	}
	if !sawSuite {
		// Plain text has no XML tokens to fail on, so "no testsuite" is the only sign it is not a report.
		return nil, fmt.Errorf("parse %s: no testsuite element, not a Surefire/Failsafe report", reportPath)
	}
	return results, nil
}

func toResult(tc xmlTestCase, suiteName string) Result {
	class := tc.ClassName
	if class == "" {
		class = suiteName // classname is optional in the xsd
	}
	res := Result{ClassName: class, MethodName: tc.Name, DurationMs: secondsToMs(tc.Time)}

	// The plan's order: failure, error, flaky, skipped, passed.
	switch {
	case len(tc.Failures) > 0:
		res.Outcome = Failed
		res.RerunFailures = len(tc.RerunFailures)
		res.FailureType, res.FailureHash = describe(tc.Failures[0].Type, tc.Failures[0].Trace)
	case len(tc.Errors) > 0:
		res.Outcome = Error
		res.RerunFailures = len(tc.RerunErrors)
		res.FailureType, res.FailureHash = describe(tc.Errors[0].Type, tc.Errors[0].Trace)
	case len(tc.FlakyFailures)+len(tc.FlakyErrors) > 0:
		res.Outcome = Flaky
		res.RerunFailures = len(tc.FlakyFailures) + len(tc.FlakyErrors)
		first := firstFlaky(tc)
		res.FailureType, res.FailureHash = describe(first.Type, first.Trace)
	case tc.Skipped != nil:
		res.Outcome = Skipped
	default:
		res.Outcome = Passed
	}
	return res
}

// firstFlaky returns the first failed attempt of a flaky test. The xsd orders flakyFailure before flakyError.
func firstFlaky(tc xmlTestCase) xmlRerun {
	if len(tc.FlakyFailures) > 0 {
		return tc.FlakyFailures[0]
	}
	return tc.FlakyErrors[0]
}

func describe(typ, trace string) (string, string) {
	if typ == "" {
		typ = typeFromTrace(trace)
	}
	return typ, FailureHash(typ, trace)
}

// typeFromTrace takes "pkg.SomeException: message" from the first line of a stack trace.
func typeFromTrace(trace string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(trace), "\n")
	typ, _, _ := strings.Cut(first, ":")
	return strings.TrimSpace(typ)
}

// frameworkPrefixes are packages that are never "the subject's own code" when looking for the frame to hash.
var frameworkPrefixes = []string{
	"java.", "javax.", "jdk.", "sun.", "com.sun.",
	"org.junit.", "junit.", "org.opentest4j.", "org.apache.maven.", "org.testng.",
	"org.assertj.", "org.hamcrest.", "org.mockito.", "kotlin.", "groovy.", "org.codehaus.groovy.",
}

// FailureHash identifies "the same failure" across builds: the exception type plus the first stack frame in the
// subject's own code (not the JDK or a test framework), without its line number, so that editing unrelated lines
// above the failure does not change the hash. It returns the first 16 hex characters of a SHA-256.
func FailureHash(typ, trace string) string {
	sum := sha256.Sum256([]byte(typ + "\n" + firstOwnFrame(trace)))
	return hex.EncodeToString(sum[:8])
}

// firstOwnFrame returns "pkg.Class.method" for the first "at ..." line outside frameworkPrefixes, or "".
func firstOwnFrame(trace string) string {
	for _, line := range strings.Split(trace, "\n") {
		frame, ok := strings.CutPrefix(strings.TrimSpace(line), "at ")
		if !ok {
			continue
		}
		frame, _, _ = strings.Cut(frame, "(") // drop "(File.java:12)"
		if i := strings.LastIndex(frame, "/"); i >= 0 {
			frame = frame[i+1:] // drop a module prefix such as "java.base/"
		}
		frame = strings.TrimSpace(frame)
		if frame != "" && !isFramework(frame) {
			return frame
		}
	}
	return ""
}

func isFramework(frame string) bool {
	for _, p := range frameworkPrefixes {
		if strings.HasPrefix(frame, p) {
			return true
		}
	}
	return false
}

// secondsToMs converts a time attribute ("0.07", or "1,234.5" from old Surefire versions) to milliseconds.
// A missing or unreadable value gives 0: a bad duration must not reject the whole report.
func secondsToMs(s string) int64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", "")
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int64(math.Round(f * 1000))
}

// ModuleFromPath returns the Maven module for a report path: the directory before "target/".
// "core/target/surefire-reports/TEST-a.xml" gives "core"; "target/surefire-reports/TEST-a.xml" gives "".
func ModuleFromPath(p string) string {
	p = path.Clean(strings.ReplaceAll(p, `\`, "/"))
	parts := strings.Split(p, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "target" {
			return strings.Join(parts[:i], "/")
		}
	}
	return ""
}

func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// severity orders outcomes for Merge: a higher number is worse.
var severity = map[Outcome]int{Skipped: 0, Passed: 1, Flaky: 2, Failed: 3, Error: 4}

// Merge combines results for the same class and method (for example a Surefire and a Failsafe report of one
// class) into one, because the database keeps one run per test per build. The worse outcome wins, with its
// failure type and hash; durations and rerun counts add up. Order of first appearance is kept.
func Merge(results []Result) []Result {
	type key struct{ class, method string }
	index := make(map[key]int, len(results))
	merged := make([]Result, 0, len(results))

	for _, r := range results {
		k := key{r.ClassName, r.MethodName}
		i, seen := index[k]
		if !seen {
			index[k] = len(merged)
			merged = append(merged, r)
			continue
		}
		m := &merged[i]
		m.DurationMs += r.DurationMs
		m.RerunFailures += r.RerunFailures
		if m.Module == "" {
			m.Module = r.Module
		}
		if severity[r.Outcome] > severity[m.Outcome] {
			m.Outcome, m.FailureType, m.FailureHash = r.Outcome, r.FailureType, r.FailureHash
		}
	}
	return merged
}
