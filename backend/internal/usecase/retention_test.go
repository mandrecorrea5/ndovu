package usecase

import (
	"context"
	"testing"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// fakeRetentionReader captura o filter recebido e devolve resultado planejado.
type fakeRetentionReader struct {
	nopEventReader
	lastFilter domain.RetentionFilter
	toReturn   domain.RetentionResult
	err        error
}

func (r *fakeRetentionReader) Retention(_ context.Context, f domain.RetentionFilter) (domain.RetentionResult, error) {
	r.lastFilter = f
	return r.toReturn, r.err
}

func TestRetention_QueryPassaFilterProReader(t *testing.T) {
	reader := &fakeRetentionReader{
		toReturn: domain.RetentionResult{
			CohortBy:    "week",
			OffsetsDays: []int{1, 7, 14, 30},
			Cohorts: []domain.RetentionCohort{
				{
					Cohort:      time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
					NewUsers:    100,
					Retained:    map[string]int{"D1": 60, "D7": 30},
					RetainedPct: map[string]float64{"D1": 0.6, "D7": 0.3},
				},
			},
		},
	}
	svc := NewRetentionService(reader)

	got, err := svc.Query(context.Background(), domain.RetentionFilter{
		App: "portal-x", CohortBy: "week",
	})
	if err != nil {
		t.Fatalf("query falhou: %v", err)
	}
	if reader.lastFilter.App != "portal-x" || reader.lastFilter.CohortBy != "week" {
		t.Errorf("filter não repassado: %+v", reader.lastFilter)
	}
	if got.CohortBy != "week" || len(got.Cohorts) != 1 {
		t.Errorf("result não veio do reader: %+v", got)
	}
}

func TestRetention_QueryCohortsVaziosNaoQuebra(t *testing.T) {
	// Regressão do bug do frontend: reader devolve array vazio, não nil.
	reader := &fakeRetentionReader{
		toReturn: domain.RetentionResult{
			CohortBy:    "week",
			OffsetsDays: []int{1, 7},
			Cohorts:     []domain.RetentionCohort{},
		},
	}
	svc := NewRetentionService(reader)
	got, err := svc.Query(context.Background(), domain.RetentionFilter{})
	if err != nil {
		t.Fatalf("erro: %v", err)
	}
	if got.Cohorts == nil {
		t.Error("cohorts NÃO deveria ser nil quando vazio (regressão de bug)")
	}
	if len(got.Cohorts) != 0 {
		t.Errorf("esperava 0 cohorts, veio %d", len(got.Cohorts))
	}
}
