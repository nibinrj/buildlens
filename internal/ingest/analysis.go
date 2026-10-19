package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nibinrj/buildlens/internal/cost"
	"github.com/nibinrj/buildlens/internal/regression"
	"github.com/nibinrj/buildlens/internal/store"
	"github.com/nibinrj/buildlens/internal/upload"
)

// Analysis is the immutable explanation saved with a build. A retry replaces it.
type Analysis struct {
	Cost            CostView              `json:"cost"`
	RegressionState string                `json:"regression_state"`
	Regressions     []regression.Evidence `json:"regressions"`
}

type CostView struct {
	Status        string     `json:"status"`
	Label         string     `json:"label"`
	USD           *string    `json:"usd"`
	PriceID       *int64     `json:"price_id,omitempty"`
	HourlyRate    string     `json:"hourly_rate,omitempty"`
	InstanceType  string     `json:"instance_type,omitempty"`
	Region        string     `json:"region,omitempty"`
	Source        string     `json:"source,omitempty"`
	EffectiveFrom *time.Time `json:"effective_from,omitempty"`
}

func (s *Service) analyze(ctx context.Context, q *store.Queries, repo store.Repository, id int64, m upload.Metadata, infra bool) error {
	c, err := s.buildCost(ctx, q, m)
	if err != nil {
		return err
	}
	a := Analysis{Cost: c, RegressionState: "EVALUATED", Regressions: []regression.Evidence{}}
	if infra || m.Result != "SUCCESS" {
		a.RegressionState = "INELIGIBLE_BUILD"
	} else if m.PRNumber == nil && m.Branch != repo.DefaultBranch {
		a.RegressionState = "NOT_DEFAULT_BRANCH"
	} else {
		a.Regressions, err = s.buildRegressions(ctx, q, repo, id, m)
		if err != nil {
			return err
		}
	}
	if err := q.DeleteBuildAnalysisAlerts(ctx, &id); err != nil {
		return fmt.Errorf("delete prior analysis alerts: %w", err)
	}
	for _, e := range a.Regressions {
		if e.Status != "REGRESSION" {
			continue
		}
		payload, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("encode regression: %w", err)
		}
		if err := q.InsertAnalysisAlert(ctx, store.InsertAnalysisAlertParams{Type: "REGRESSION", BuildID: &id, RepositoryID: repo.ID, Payload: payload}); err != nil {
			return fmt.Errorf("write regression alert: %w", err)
		}
	}
	if c.USD != nil {
		payload, err := json.Marshal(c)
		if err != nil {
			return fmt.Errorf("encode cost: %w", err)
		}
		if err := q.InsertAnalysisAlert(ctx, store.InsertAnalysisAlertParams{Type: "COST", BuildID: &id, RepositoryID: repo.ID, Payload: payload}); err != nil {
			return fmt.Errorf("write cost record: %w", err)
		}
	}
	payload, err := json.Marshal(a)
	if err != nil {
		return fmt.Errorf("encode analysis: %w", err)
	}
	var amount pgtype.Numeric
	if c.USD != nil {
		if err := amount.Scan(*c.USD); err != nil {
			return fmt.Errorf("encode cost amount: %w", err)
		}
	}
	if err := q.SetBuildAnalysis(ctx, store.SetBuildAnalysisParams{ID: id, CostUsd: amount, Analysis: payload}); err != nil {
		return fmt.Errorf("save analysis: %w", err)
	}
	return nil
}

func (s *Service) buildCost(ctx context.Context, q *store.Queries, m upload.Metadata) (CostView, error) {
	c := CostView{Status: "UNKNOWN", Label: "cost unavailable"}
	if m.Agent.BusyMs == nil {
		return c, nil
	}
	instance := m.Agent.InstanceType
	if m.Agent.Lifecycle == "LOCAL" {
		instance = s.referenceInstance
		c.Label = "equivalent cost at reference price"
	}
	if instance == "" || m.Agent.Lifecycle == "" {
		return c, nil
	}
	p, err := q.FindBuildPrice(ctx, store.FindBuildPriceParams{InstanceType: instance, Lifecycle: m.Agent.Lifecycle, Region: s.priceRegion, AtTime: m.StartedAt})
	if errors.Is(err, pgx.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, fmt.Errorf("find build price: %w", err)
	}
	amount, err := cost.Calculate(*m.Agent.BusyMs, p.HourlyRate)
	if err != nil {
		c.Status = "OUT_OF_RANGE"
		return c, nil //nolint:nilerr // unrepresentable monetary values remain unknown without losing valid build results
	}
	c.Status = "KNOWN"
	c.USD = &amount
	c.PriceID = &p.ID
	c.HourlyRate = p.HourlyRate
	c.InstanceType = p.InstanceType
	c.Region = p.Region
	c.Source = p.Source
	c.EffectiveFrom = &p.EffectiveFrom
	if m.Agent.Lifecycle != "LOCAL" {
		c.Label = "compute cost at recorded price"
	}
	return c, nil
}

func (s *Service) buildRegressions(ctx context.Context, q *store.Queries, repo store.Repository, id int64, m upload.Metadata) ([]regression.Evidence, error) {
	if err := s.regression.Validate(); err != nil {
		return nil, err
	}
	// Hold out up to three default-branch observations as a single evaluation
	// cohort. The remaining baseline cannot contain any member of that cohort.
	history, err := q.ListPerformanceBuilds(ctx, store.ListPerformanceBuildsParams{RepositoryID: repo.ID, Branch: repo.DefaultBranch, BeforeTime: m.StartedAt, BeforeID: id, SampleLimit: int32(s.regression.Window + 3)}) //nolint:gosec // validated window <= 100, so the limit is at most 103
	if err != nil {
		return nil, fmt.Errorf("list baseline builds: %w", err)
	}
	baseline := make([]int64, 0, len(history))
	recent := []int64{}
	for _, b := range history {
		baseline = append(baseline, b.ID)
	}
	if m.PRNumber != nil {
		previous, err := q.ListPRPerformanceBuilds(ctx, store.ListPRPerformanceBuildsParams{RepositoryID: repo.ID, PrNumber: m.PRNumber, BeforeTime: m.StartedAt, BeforeID: id})
		if err != nil {
			return nil, fmt.Errorf("list PR builds: %w", err)
		}
		for _, b := range previous {
			recent = append(recent, b.ID)
		}
	} else {
		n := min(3, len(baseline))
		recent = append(recent, baseline[:n]...)
		baseline = baseline[n:]
	}
	baseline = baseline[:min(len(baseline), s.regression.Window)]
	ids := append(append([]int64{}, baseline...), recent...)
	rows, err := q.ListPerformanceMetrics(ctx, store.ListPerformanceMetricsParams{CurrentBuildID: id, BuildIds: ids})
	if err != nil {
		return nil, fmt.Errorf("list performance metrics: %w", err)
	}
	type key struct{ kind, id string }
	type observation struct {
		name   string
		values map[int64]int64
	}
	metrics := map[key]*observation{}
	for _, r := range rows {
		k := key{r.Kind, r.MetricKey}
		if metrics[k] == nil {
			metrics[k] = &observation{name: r.Name, values: map[int64]int64{}}
		}
		metrics[k].values[r.BuildID] = r.DurationMs
	}
	result := []regression.Evidence{}
	for k, metric := range metrics {
		if _, ok := metric.values[id]; !ok {
			continue
		}
		base, latest := []int64{}, []int64{}
		baselineIDs, recentIDs := []int64{}, []int64{}
		for _, bid := range baseline {
			if v, ok := metric.values[bid]; ok {
				base = append(base, v)
				baselineIDs = append(baselineIDs, bid)
			}
		}
		for _, bid := range recent {
			if v, ok := metric.values[bid]; ok {
				latest = append(latest, v)
				recentIDs = append(recentIDs, bid)
			} else {
				break
			}
		} // a missing observation breaks sustained evidence
		e, err := regression.Evaluate(base, latest, m.PRNumber != nil, s.regression)
		if err != nil {
			return nil, err
		}
		e.Kind = k.kind
		e.Name = metric.name
		e.BaselineBuildIDs = baselineIDs
		e.RecentBuildIDs = recentIDs
		result = append(result, e)
	}
	slices.SortFunc(result, func(a, b regression.Evidence) int {
		if a.Kind != b.Kind {
			if a.Kind < b.Kind {
				return -1
			}
			return 1
		}
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
	return result, nil
}
