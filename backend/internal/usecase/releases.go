package usecase

import (
	"context"
	"time"

	"github.com/marcoscorrea/ndovu/backend/internal/domain"
)

// ReleaseService expõe a visão de releases (versões do app) e a comparação
// entre duas — base para detectar regressões após deploys.
type ReleaseService struct {
	reader domain.EventReader
}

// NewReleaseService cria o serviço de releases.
func NewReleaseService(reader domain.EventReader) *ReleaseService {
	return &ReleaseService{reader: reader}
}

// List devolve as releases ativas na janela ordenadas por atividade recente.
func (s *ReleaseService) List(ctx context.Context, f domain.ReleaseFilter) ([]domain.Release, error) {
	return s.reader.FindReleases(ctx, f)
}

// Compare calcula deltas entre duas releases + issues novas em B (que não
// existiam em A) — indicador direto de regressão introduzida no deploy B.
func (s *ReleaseService) Compare(ctx context.Context, app, releaseA, releaseB string, from, to time.Time) (domain.ReleaseComparison, error) {
	if releaseA == "" || releaseB == "" {
		return domain.ReleaseComparison{}, domain.NewValidationError("releaseA e releaseB são obrigatórios")
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}

	relA, err := s.reader.GetRelease(ctx, app, releaseA, from, to)
	if err != nil {
		return domain.ReleaseComparison{}, err
	}
	relB, err := s.reader.GetRelease(ctx, app, releaseB, from, to)
	if err != nil {
		return domain.ReleaseComparison{}, err
	}

	// Issues novas: pegue as issues da release B e subtraia as fingerprints
	// que já apareciam em A. A ordenação por impacto vem do reader.
	issuesA, err := s.reader.FindIssues(ctx, domain.IssueFilter{
		From: &from, To: &to, App: app, Release: releaseA, Limit: 500,
	})
	if err != nil {
		return domain.ReleaseComparison{}, err
	}
	knownA := map[string]struct{}{}
	for _, i := range issuesA {
		knownA[i.Fingerprint] = struct{}{}
	}
	issuesB, err := s.reader.FindIssues(ctx, domain.IssueFilter{
		From: &from, To: &to, App: app, Release: releaseB, Limit: 100,
	})
	if err != nil {
		return domain.ReleaseComparison{}, err
	}
	newIssues := []domain.Issue{}
	for _, i := range issuesB {
		if _, seen := knownA[i.Fingerprint]; !seen {
			newIssues = append(newIssues, i)
		}
	}

	return domain.ReleaseComparison{
		ReleaseA:       relA,
		ReleaseB:       relB,
		DeltaErrorRate: relB.ErrorRate - relA.ErrorRate,
		DeltaAvgMs:     relB.AvgDurationMs - relA.AvgDurationMs,
		NewIssues:      newIssues,
	}, nil
}
