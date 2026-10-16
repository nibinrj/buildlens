// Package infra attributes bounded Jenkins log tails before flaky detection.
package infra

import (
	"regexp"
	"strings"
)

type Match struct {
	Reason string
	Line   string
}

func (m Match) Failure() bool { return m.Reason != "" }

var stamp = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}(?:\.\d+)?\s+`)
var agent = regexp.MustCompile(`(?i)(hudson\.remoting\.(ChannelClosedException|RequestAbortedException)|^Agent .+ was deleted; cancelling node body|^.+ has been removed or offline .+terminating shell step|^Cannot contact .+:.*(ChannelClosedException|channel is closing)|^ERROR:.*agent.*disconnected)`)
var oom = regexp.MustCompile(`(?i)(\bOOMKilled(?:\s*[:=]\s*true|\s*$)|\b(exit code|exit status|exitCode)\s*[:=]?\s*137\b)`)
var disk = regexp.MustCompile(`(?i)^(ERROR:|\[ERROR\]|java\.io\.(IOException|FileNotFoundException):|(?:mkdir|cp|mv|tar|sh|write):).*[Nn]o space left on device`)
var docker = regexp.MustCompile(`(?i)^(ERROR:|\[ERROR\]|docker:|org\.testcontainers\.|com\.github\.dockerjava\.|java\.lang\.IllegalStateException:).*(cannot connect to the docker daemon|could not find a valid docker environment|is the docker daemon running)`)

// Classify gives a stable first-match reason. Assertion/example lines are not host diagnostics.
func Classify(tail string) Match {
	for _, raw := range strings.Split(tail, "\n") {
		line := stamp.ReplaceAllString(strings.TrimSpace(raw), "")
		lower := strings.ToLower(line)
		if strings.Contains(lower, "assertion") || strings.Contains(lower, "expected:") || strings.Contains(lower, "expected message") || strings.Contains(lower, "example:") || strings.Contains(lower, "test stdout:") {
			continue
		}
		reason := ""
		switch {
		case agent.MatchString(line):
			reason = "AGENT_LOST"
		case oom.MatchString(line) && !strings.Contains(lower, "oomkilled=false") && !strings.Contains(lower, "oomkilled: false"):
			reason = "OUT_OF_MEMORY"
		case disk.MatchString(line):
			reason = "DISK_FULL"
		case docker.MatchString(line):
			reason = "DOCKER_UNAVAILABLE"
		case strings.HasPrefix(line, "[ERROR]") &&
			(strings.Contains(lower, "could not transfer artifact") || strings.Contains(lower, "could not resolve dependencies") || strings.Contains(lower, "failed to read artifact descriptor")) &&
			(strings.Contains(lower, "timed out") || strings.Contains(lower, "connect timeout") || strings.Contains(lower, "read timeout")):
			reason = "DEPENDENCY_TIMEOUT"
		}
		if reason != "" {
			return Match{reason, line}
		}
	}
	return Match{}
}
