// Command buildlens is the agent-side CLI that Jenkins pipelines run.
//
// Usage:
//
//	buildlens report [flags]      upload one build's test reports, stage timings and log tail
//	buildlens quarantine [flags]  write Surefire exclusions or run-only patterns
//	buildlens version
//
// "buildlens report" never fails a build because BuildLens is down: it prints a warning and exits 0,
// unless --strict is given.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

// version is replaced at build time with -ldflags "-X main.version=...".
var version = "dev"

// Exit codes.
const (
	exitOK     = 0 // done, or failed without --strict (a warning was printed)
	exitFailed = 1 // report failed with --strict, or quarantine file could not be written
	exitUsage  = 2 // bad flags or invalid input: a pipeline bug, not an outage
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	if len(args) == 0 {
		usage(stderr)
		return exitUsage
	}
	switch args[0] {
	case "report":
		r := reporter{client: &http.Client{}, sleep: sleepCtx, stdout: stdout, stderr: stderr}
		return r.run(ctx, args[1:], getenv)
	case "quarantine":
		q := quarantineFetcher{client: &http.Client{}, stdout: stdout, stderr: stderr}
		return q.run(ctx, args[1:], getenv)
	case "version":
		fmt.Fprintln(stdout, "buildlens", version)
		return exitOK
	case "help", "-h", "--help":
		usage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "buildlens: unknown command %q\n", args[0])
		usage(stderr)
		return exitUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `usage: buildlens <command> [flags]

commands:
  report      upload test reports, stage timings and the log tail of one build
  quarantine  write quarantined tests as Surefire exclusion/include patterns
  version     print the version

Run "buildlens <command> -h" for the flags of a command.
`)
}
