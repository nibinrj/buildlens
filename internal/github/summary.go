package github

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nibinrj/buildlens/internal/ingest"
	"github.com/nibinrj/buildlens/internal/store"
)

type Summary struct {
	Repository, CommitSHA, Body string
	Skip                        bool
}
type Source interface {
	Load(context.Context, store.PrComment) (Summary, error)
}

type DatabaseSource struct{ Pool *pgxpool.Pool }

func (s DatabaseSource) Load(ctx context.Context, work store.PrComment) (Summary, error) {
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Summary{}, fmt.Errorf("begin summary read: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck // read-only transaction
	q := store.New(tx)
	b, err := q.GetBuildWithRepository(ctx, work.BuildID)
	if err != nil {
		return Summary{}, fmt.Errorf("read summary build: %w", err)
	}
	if b.Build.RepositoryID != work.RepositoryID || b.Build.PrNumber == nil || *b.Build.PrNumber != work.PrNumber {
		return Summary{Skip: true}, nil
	}
	entries, err := q.ListActiveQuarantine(ctx, work.RepositoryID)
	if err != nil {
		return Summary{}, fmt.Errorf("read summary quarantines: %w", err)
	}
	var analysis ingest.Analysis
	if err := json.Unmarshal(b.Build.Analysis, &analysis); err != nil {
		return Summary{}, fmt.Errorf("decode summary analysis: %w", err)
	}
	return Summary{Repository: b.RepositoryName, CommitSHA: b.Build.CommitSha, Body: Render(b, analysis, entries)}, nil
}

// Render treats uploaded names as text: HTML, mentions and table syntax are escaped.
// The marker and headings are controlled by BuildLens, never uploaded report content.
func Render(b store.GetBuildWithRepositoryRow, a ingest.Analysis, entries []store.ListActiveQuarantineRow) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%s\n## BuildLens\n\nBuild **%d** · %s · commit `%s`\n\n", Marker, b.Build.BuildNumber, text(b.Build.Result), text(b.Build.CommitSha))
	out.WriteString("### Quarantined tests\n\n")
	if len(entries) == 0 {
		out.WriteString("No active quarantines.\n")
	} else {
		for _, e := range entries {
			fmt.Fprintf(&out, "- %s#%s — %s, %d consecutive quarantine passes\n", text(e.ClassName), text(e.MethodName), text(e.ReasonRule), e.ConsecutivePasses)
			if out.Len() > 48_000 {
				out.WriteString("\nAdditional quarantine entries omitted.\n")
				break
			}
		}
	}
	out.WriteString("\n### Regressions\n\n")
	if a.RegressionState != "EVALUATED" {
		fmt.Fprintf(&out, "Duration checks: %s.\n", text(a.RegressionState))
	} else if len(a.Regressions) == 0 {
		out.WriteString("Insufficient data: no comparable stages or tests.\n")
	} else {
		out.WriteString("| Kind | Name | Status | Latest ms | Baseline samples | Median ms | Threshold ms |\n|---|---|---|---:|---:|---:|---:|\n")
		for _, e := range a.Regressions {
			fmt.Fprintf(&out, "| %s | %s | %s | %d | %d | %.2f | %.2f |\n", text(e.Kind), text(e.Name), text(e.Status), e.CurrentMs, e.Samples, e.MedianMs, e.ThresholdMs)
			if out.Len() > 56_000 {
				out.WriteString("\nAdditional metrics omitted; see the build analysis API.\n")
				break
			}
		}
	}
	out.WriteString("\n### Cost\n\n")
	if a.Cost.USD == nil {
		out.WriteString("Unknown: no applicable price or complete agent duration.\n")
	} else {
		fmt.Fprintf(&out, "**$%s USD — %s** (%s, %s; $%s/hour).\n\nSource: %s\n", text(*a.Cost.USD), text(a.Cost.Label), text(a.Cost.InstanceType), text(a.Cost.Region), text(a.Cost.HourlyRate), text(a.Cost.Source))
	}
	out.WriteString("\nLocal reference cost excludes storage, networking, CPU credit charges and idle capacity.\n")
	body := out.String()
	if len(body) > 59_000 {
		end := 59_000
		for !utf8.RuneStart(body[end]) {
			end--
		}
		body = body[:end] + "\n\nAdditional text omitted; see the authenticated build analysis API.\n"
	}
	return body
}

func text(s string) string {
	runes := []rune(s)
	if len(runes) > 300 {
		runes = runes[:300]
	}
	s = html.EscapeString(string(runes))
	return strings.NewReplacer("@", "&#64;", "|", "&#124;", "`", "&#96;", "\n", " ", "\r", " ").Replace(s)
}
